package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
	"golang.org/x/xerrors"
)

const (
	idPath                  = "/.local/.go-brightness-id-123458"
	brightnessctlDeviceName = "amdgpu_bl0"
	brightnessIcon          = "/usr/share/icons/Papirus/64x64/apps/display-brightness.svg"

	brightnessCommand = "brightnessctl -d " + brightnessctlDeviceName
)

func homeID() string {
	return os.Getenv("HOME") + idPath
}

var cfg config

func main() {
	readConfig()

	b, err := readBrightness()
	if err != nil {
		log.Fatalf("can't read brightness %+v\n", err)
	}

	sq := math.Sqrt(float64(b.current))
	if sq < 1 {
		sq = 1
	}

	debugPl("sq: ", sq)

	switch {
	case cfg.dec:
		b.set = int(math.Max(float64(b.current)-sq, 0))
	case cfg.inc:
		b.set = int(math.Min(math.Max(float64(b.current)+sq, 0), float64(b.max)))
	}

	debugPl("setting brightness to", b.set, ", which in percents will be", b.willBeInPercents())
	runShell(brightnessCommand + " s " + strconv.Itoa(b.set))

	nID, err := sentNotification(b)
	if err != nil {
		log.Printf("failed to create notification: %+v", err)
		os.Exit(3)
	}

	if err = saveID(nID); err != nil {
		log.Printf("failed to save notification id to %q: %+v", homeID(), err)
		os.Exit(4)
	}
}

func runShell(script string) string {
	cmd := exec.Command("bash", "-c", script)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		fmt.Printf("error running bash script %q: %+v", script, err)
		os.Exit(2)
	}

	result := strings.TrimSuffix(out.String(), "\n")
	debugPf("debug shell command output: %q\n", result)

	return result
}

type brightness struct {
	current int
	max     int
	set     int
}

func (b *brightness) willBeInPercents() int {
	return b.set * 100 / b.max
}

func readBrightness() (b brightness, err error) {
	b.current, err = strconv.Atoi(runShell((brightnessCommand + " g")))
	if err != nil {
		return b, xerrors.Errorf("parsing current brightness %+v\n", err)
	}

	b.max, err = strconv.Atoi(runShell((brightnessCommand + " m")))
	if err != nil {
		return b, xerrors.Errorf("parsing max brightness %+v\n", err)
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
	debug bool
	inc   bool
	dec   bool
}

func readConfig() {
	for _, a := range os.Args {
		switch a {
		case "-d":
			cfg.debug = true
		case "inc", "-inc", "--inc":
			if cfg.dec {
				log.Fatal("conflicting arguments")
			}
			cfg.inc = true
		case "dec", "-dec", "--dec":
			if cfg.inc {
				log.Fatal("conflicting arguments")
			}
			cfg.dec = true
		}
	}
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
