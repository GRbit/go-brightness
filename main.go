package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
	"golang.org/x/xerrors"
)

const (
	idPath         = ".local/.go-brightness-id-123458"
	brightnessIcon = "/usr/share/icons/Papirus/64x64/apps/display-brightness.svg"

	pciBrightness     = "/sys/devices/pci*/*/drm/card*/*/*/brightness"
	pciBrightnessReg  = "/sys/devices/pci[^/]*/[^/]*/drm/card[^/]*/[^/]*/[^/]*/brightness"
	backlight         = "/sys/devices/pci*/*/*/backlight/*/brightness"
	backlightReg      = "/sys/devices/pci[^/]*/[^/]*/[^/]*/backlight/[^/]*/brightness"
	classBacklight    = "/sys/class/backlight/*/brightness"
	classBacklightReg = "/sys/class/backlight/(.*)/brightness"

	defaultSteps = 20
)

var cfg config

type config struct {
	debug      bool
	increase   bool
	decrease   bool
	list       bool
	device     string
	devicePath string
	steps      int
}

type brightnessDevicePath struct {
	glob   string
	regexp string
}

func devicePaths() []brightnessDevicePath {
	return []brightnessDevicePath{
		{glob: pciBrightness, regexp: pciBrightnessReg},
		{glob: backlight, regexp: backlightReg},
		{glob: classBacklight, regexp: classBacklightReg},
	}
}

func homeID() string {
	return path.Join(os.Getenv("HOME"), idPath)
}

func main() {
	if err := readArgs(); err != nil {
		log.Fatalf("failed to read args: %+v\n", err)
	}

	b, err := readBrightness()
	if err != nil {
		log.Println("failed to read brightness:", err)
		debugPf("verbose error: %+v", err)
		os.Exit(2)
	}

	if !cfg.decrease && !cfg.increase {
		return
	}

	switch {
	case cfg.decrease:
		b.set = getRawValue(b, false)
	case cfg.increase:
		b.set = getRawValue(b, true)
	}

	debugPf("setting brightness to %v, which in percents will be %v", b.set, b.willBeInPercents())
	if err = b.SetBrightness(); err != nil {
		log.Printf("failed to set brightness: %+v\n", err)
		os.Exit(3)
	}

	nID, err := sentNotification(b)
	if err != nil {
		log.Printf("failed to create notification: %+v\n", err)
		os.Exit(4)
	}

	if err = saveID(nID); err != nil {
		log.Printf("failed to save notification id to %q: %+v\n", homeID(), err)
		os.Exit(5)
	}
}

// Get raw hardware value for a specific level logarithmically
func getRawValue(b brightness, increase bool) int {
	level := math.Sqrt(float64(b.current)/float64(b.max)) * float64(cfg.steps)
	debugPf("currentLevel %v", level)
	if increase {
		level++
	} else {
		level--
	}

	// Clamp level between 0 and TotalSteps
	if level < 0 {
		level = 0
	}
	if level > float64(cfg.steps) {
		level = float64(cfg.steps)
	}

	debugPf("newLevel %v", level)

	// Raw = (level / TotalSteps)^2 * Max
	raw := int(math.Pow(level/float64(cfg.steps), 2) * float64(b.max))
	debugPf("raw %v", raw)

	return raw
}

type brightness struct {
	current int
	max     int
	set     int
	device  string
}

func (b *brightness) willBeInPercents() int {
	return b.set * 100 / b.max
}

func (b *brightness) SetBrightness() error {
	f, err := os.OpenFile(b.device, os.O_WRONLY, 0)
	if err != nil {
		return xerrors.Errorf("failed to open brightness file: %w", err)
	}
	_, err = f.WriteString(strconv.Itoa(b.set))
	if err != nil {
		return xerrors.Errorf("failed to write to brightness file: %w", err)
	}

	if err = f.Close(); err != nil {
		return xerrors.Errorf("failed to close brightness file: %w", err)
	}

	return nil
}

func readBrightness() (b brightness, err error) {
	brightDevs := make(map[string]string)

	for _, p := range devicePaths() {
		dd, err := filepath.Glob(p.glob)
		if err != nil {
			return b, xerrors.Errorf("globbing %q: %w", p, err)
		}

		debugPf("looking for brightness devices: %+v", dd)

		// compile regexp and find all mathing lines
		r, err := regexp.Compile(p.regexp)
		if err != nil {
			return b, xerrors.Errorf("compiling regexp %q: %w", p.regexp, err)
		}

		for _, d := range dd {
			debugPf("d: '%+v'", d)
			debugPf("r: '%+v'", p.regexp)
			matches := r.FindAllStringSubmatch(d, -1)
			if matches == nil {
				continue
			}

			debugPf("matches: %+v", matches[0][1])

			brightDevs[matches[0][1]] = d
		}
	}

	debugPf("len(devices)=%d", len(brightDevs))
	for k, v := range brightDevs {
		debugPf("path: %s", v)
		if cfg.list {
			fmt.Printf("%s: %s\n", k, v)
		}
	}

	switch len(brightDevs) {
	case 0:
		return b, xerrors.Errorf("no brightness files found")
	case 1:
		if cfg.device != "" {
			var ok bool
			b.device, ok = brightDevs[cfg.device]
			if !ok {
				return b, xerrors.Errorf("device %q not found, please specify device from: %v", cfg.device,
					brightDevs)
			}
		}

		for _, v := range brightDevs {
			b.device = v
		}
	default:
		if cfg.device == "" && cfg.devicePath == "" {
			return b, xerrors.Errorf("multiple brightness files found, "+
				"please specify device path or a device name from from: %v",
				brightDevs)
		}

		if cfg.devicePath != "" {
			break
		}

		var ok bool
		b.device, ok = brightDevs[cfg.device]
		if !ok {
			return b, xerrors.Errorf("device %q not found, please specify device from: %v", cfg.device,
				brightDevs)
		}
	}

	if b.device == "" {
		if cfg.devicePath != "" {
			b.device = cfg.devicePath
		} else {
			return b, xerrors.Errorf("no device specified, please specify device from: %v", brightDevs)
		}
	}

	b.max, err = readFile(filepath.Join(filepath.Dir(b.device), "max_brightness"))
	if err != nil {
		return b, xerrors.Errorf("failed to get max brightness: %w", err)
	}

	b.current, err = readFile(b.device)
	if err != nil {
		return b, xerrors.Errorf("failed to get current brightness: %w", err)
	}

	debugPf("brightness: %+v", b)

	return b, nil
}

func sentNotification(b brightness) (uint32, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return 0, xerrors.Errorf("failed connecting to dbus: %w", err)
	}

	notifier, err := notify.New(conn)
	if err != nil {
		return 0, xerrors.Errorf("failed creating notifier: %w", err)
	}

	nID, err := notifier.SendNotification(notify.Notification{
		AppName:       "go-brightness",
		ReplacesID:    getID(),
		AppIcon:       brightnessIcon,
		Summary:       fmt.Sprintf("Brightness set to: %d%%", b.willBeInPercents()),
		Body:          fmt.Sprintf("Brightness set to: %d%%", b.willBeInPercents()),
		Actions:       nil,
		Hints:         map[string]dbus.Variant{"value": dbus.MakeVariant(b.willBeInPercents())},
		ExpireTimeout: time.Second * 2,
	})
	if err != nil {
		return 0, xerrors.Errorf("failed to send notification: %w", err)
	}

	debugPf("notification id =", nID)

	return nID, nil
}

func readArgs() error {
	if len(os.Args) < 2 {
		return xerrors.Errorf("no arguments provided")
	}

	cfg.steps = defaultSteps

	var scanStep bool

	for i, a := range os.Args[1:] {
		switch a {
		case "-h", "--help", "help", "-help":
			fmt.Println("Usage: go-brightness options/commands [device]")
			fmt.Println("options and commands:")
			fmt.Println("  -d: debug mode")
			fmt.Println("  inc: increase brightness")
			fmt.Println("  dec: decrease brightness")
			fmt.Println("  ls: list available devices")
			fmt.Println("device:")
			fmt.Println("  Device name to set brightness for. If omitted, first found device will be used")
			os.Exit(0)
		case "-d", "--debug", "debug", "-debug", "dbg", "-dbg", "--dbg":
			cfg.debug = true
		case "inc", "-inc", "--inc", "increase", "-increase", "--increase":
			if cfg.decrease {
				return xerrors.Errorf("conflicting arguments, you are trying to increase and decrease brightness")
			}
			cfg.increase = true
		case "dec", "-dec", "--dec", "decrease", "-decrease", "--decrease":
			if cfg.increase {
				return xerrors.Errorf("conflicting arguments, you are trying to increase and decrease brightness")
			}
			cfg.decrease = true
		case "list", "ls", "-list", "--list", "-l", "--ls":
			cfg.list = true
		case "steps", "s", "-s", "--steps", "-steps", "--st":
			scanStep = true
		default:
			if i == 0 {
				continue
			} else if scanStep {
				var err error
				cfg.steps, err = strconv.Atoi(a)
				if err != nil {
					fmt.Println("invalid arguemets for steps", os.Args[i+1], "err:", err)
					os.Exit(2)
				}
				scanStep = false
			} else if strings.Contains(a, "/") {
				cfg.devicePath = a
				debugPf("devicePath =", a)
			} else {
				cfg.device = a
				debugPf("device =", a)
			}
		}
	}

	if scanStep {
		fmt.Println("no arguments after steps, must be int")
		os.Exit(2)
	}

	return nil
}

func getID() uint32 {
	if _, err := os.Stat(homeID()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0
	}

	bb, err := os.ReadFile(homeID())
	if err != nil {
		return 0
	}

	ret, err := strconv.Atoi(string(bb))
	if err != nil {
		return 0
	}

	return uint32(ret)
}

func saveID(id uint32) error {
	return os.WriteFile(homeID(), []byte(strconv.Itoa(int(id))), 0o600)
}

func readFile(p string) (int, error) {
	buf, err := os.ReadFile(p)
	if err != nil {
		return 0, xerrors.Errorf("failed to read %q: %w", p, err)
	}

	buf = bytes.TrimSpace(buf)
	n, err := strconv.Atoi(string(buf))
	if err != nil {
		return 0, xerrors.Errorf("failed to parse %q: %w", p, err)
	}

	return n, nil
}

func debugPf(format string, a ...interface{}) {
	if cfg.debug {
		fmt.Printf(format+"\n", a...)
	}
}
