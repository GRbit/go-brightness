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
	"strings"
	"time"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
	"golang.org/x/exp/maps"
	"golang.org/x/xerrors"
)

const (
	idPath         = "/.local/.go-brightness-id-123458"
	brightnessIcon = "/usr/share/icons/Papirus/64x64/apps/display-brightness.svg"
)

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
		log.Printf("failed to read brightness: %+v\n", err)
		os.Exit(2)
	}

	sq := math.Sqrt(float64(b.current))
	if sq < 1 {
		sq = 1
	}

	debugPl("sq: ", sq)

	switch {
	case cfg.decrease:
		b.set = int(math.Max(float64(b.current)-sq, 0))
	case cfg.increase:
		b.set = int(math.Min(math.Max(float64(b.current)+sq, 0), float64(b.max)))
	}

	debugPl("setting brightness to", b.set, ", which in percents will be", b.willBeInPercents())
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
	drm, err := filepath.Glob("/sys/devices/pci*/*/drm/card*/*/*/brightness")
	if err != nil {
		return b, xerrors.Errorf("globbing drm card brightness: %w", err)
	}

	debugPf("drm: %+v\n", drm)

	bl, err := filepath.Glob("/sys/devices/pci*/*/*/backlight/*/brightness")
	if err != nil {
		return b, xerrors.Errorf("globbing pci backlight brightness: %w", err)
	}

	debugPf("bl: %+v\n", bl)

	ddevices := make(map[string]string)
	for _, m := range append(drm, bl...) {
		tmp := filepath.Dir(filepath.Dir(m))
		card := filepath.Base(filepath.Dir(tmp))
		d := strings.TrimPrefix(filepath.Base(tmp), card)
		d = strings.TrimPrefix(d, "-")
		ddevices[d] = m
	}

	debugPf("len(devices)=%d\nnames: %+v\ndevices: %+v\n", len(ddevices), maps.Keys(ddevices), maps.Values(ddevices))

	switch len(ddevices) {
	case 0:
		return b, xerrors.Errorf("no brightness files found")
	case 1:
		b.device = maps.Values(ddevices)[0]
	default:
		if cfg.device == "" {
			return b, xerrors.Errorf("multiple brightness files found, please specify device from: %s",
				strings.Join(maps.Keys(ddevices), ","))
		}

		var ok bool
		b.device, ok = ddevices[cfg.device]
		if !ok {
			return b, xerrors.Errorf("device %q not found, please specify device from: %s", cfg.device,
				strings.Join(maps.Keys(ddevices), ","))
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

	debugPf("brightness: %+v\n", b)

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

	debugPl("notification id =", nID)

	return nID, nil
}

type config struct {
	debug    bool
	increase bool
	decrease bool
	device   string
}

func readArgs() error {
	for i, a := range os.Args {
		switch a {
		case "-d":
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
		default:
			if i == 0 {
				continue
			}

			cfg.device = a
			debugPl("device =", a)
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

func debugPf(format string, a ...interface{}) {
	if cfg.debug {
		fmt.Printf(format, a...)
	}
}

func debugPl(a ...interface{}) {
	if cfg.debug {
		fmt.Println(a...)
	}
}
