package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/esiqveland/notify"
	"golang.org/x/xerrors"
	"github.com/godbus/dbus/v5"
	"regexp"
	"strings"
)

const (
	idPath         = "/.local/.go-brightness-id-123458"
	brightnessIcon = "/usr/share/icons/Papirus/64x64/apps/display-brightness.svg"

	pciBrightness     = "/sys/devices/pci*/*/drm/card*/*/*/brightness"
	pciBrightnessReg  = "/sys/devices/pci[^/]*/[^/]*/drm/card[^/]*/[^/]*/[^/]*/brightness"
	backlight         = "/sys/devices/pci*/*/*/backlight/*/brightness"
	backlightReg      = "/sys/devices/pci[^/]*/[^/]*/[^/]*/backlight/[^/]*/brightness"
	classBacklight    = "/sys/class/backlight/*/brightness"
	classBacklightReg = "/sys/class/backlight/(.*)/brightness"
)

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
	return os.Getenv("HOME") + idPath
}

var cfg config

func main() {
	if err := readArgs(); err != nil {
		log.Fatalf("failed to read args: %+v\n", err)
	}

	b, err := readBrightness()
	if err != nil {
		log.Println("failed to read brightness:", err)
		debugP("verbose error: %+v", err)
		os.Exit(2)
	}

	if !cfg.decrease && !cfg.increase {
		return
	}

	sq := math.Sqrt(float64(b.current))
	if sq < 1 {
		sq = 1
	}

	debugP("sq: ", sq)

	switch {
	case cfg.decrease:
		b.set = int(math.Max(float64(b.current)-sq, 0))
	case cfg.increase:
		b.set = int(math.Min(math.Max(float64(b.current)+sq, 0), float64(b.max)))
	}

	debugP("setting brightness to", b.set, ", which in percents will be", b.willBeInPercents())
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

		debugP("looking for brightness devices: %+v", dd)

		// compile regexp and find all mathing lines
		r, err := regexp.Compile(p.regexp)
		if err != nil {
			return b, xerrors.Errorf("compiling regexp %q: %w", p.regexp, err)
		}

		for _, d := range dd {
			debugP("d: '%+v'", d)
			debugP("r: '%+v'", p.regexp)
			matches := r.FindAllStringSubmatch(d, -1)
			if matches == nil {
				continue
			}

			debugP("matches: %+v", matches[0][1])

			brightDevs[matches[0][1]] = d
		}
	}

	debugP("len(devices)=%d", len(brightDevs))
	for k, v := range brightDevs {
		debugP("path: %s", v)
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

	debugP("brightness: %+v", b)

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
		Body:          fmt.Sprintf("Brightness set to: %d%%", b.willBeInPercents()),
		Summary:       fmt.Sprintf("Brightness set to: %d%%", b.willBeInPercents()),
		Hints:         map[string]dbus.Variant{"value": dbus.MakeVariant(b.willBeInPercents())},
		ExpireTimeout: time.Second * 2,
	})
	if err != nil {
		return 0, xerrors.Errorf("failed to send notification: %w", err)
	}

	debugP("notification id =", nID)

	return nID, nil
}

type config struct {
	debug      bool
	increase   bool
	decrease   bool
	list       bool
	device     string
	devicePath string
}

func readArgs() error {
	if len(os.Args) < 2 {
		return xerrors.Errorf("no arguments provided")
	}

	for i, a := range os.Args {
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
		default:
			if i == 0 {
				continue
			}

			if strings.Contains(a, "/") {
				cfg.devicePath = a
				debugP("devicePath =", a)
			} else {
				cfg.device = a
				debugP("device =", a)
			}
		}
	}

	return nil
}

func getID() uint32 {
	if _, err := os.Stat(homeID()); errors.Is(err, os.ErrNotExist) {
	} else if err != nil {
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
	return os.WriteFile(homeID(), []byte(strconv.Itoa(int(id))), 0o777)
}

func readFile(path string) (int, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return 0, xerrors.Errorf("failed to read %q: %w", path, err)
	}

	buf = bytes.TrimSpace(buf)
	n, err := strconv.Atoi(string(buf))
	if err != nil {
		return 0, xerrors.Errorf("failed to parse %q: %w", path, err)
	}

	return n, nil
}

func debugP(format string, a ...interface{}) {
	if cfg.debug {
		fmt.Printf(format+"\n", a...)
	}
}
