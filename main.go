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
)

const (
	idPath                  = "/tmp/go-br-id"
	brightnessctlDeviceName = "amdgpu_bl0"
	brightnessIcon          = "/usr/share/icons/Papirus/64x64/apps/display-brightness.svg"
)

var cfg config

type config struct {
	debug bool
	inc   bool
	dec   bool
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

func main() {
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

	xb := "brightnessctl -d " + brightnessctlDeviceName

	var (
		b   brightness
		err error
	)

	b.current, err = strconv.Atoi(runShell((xb + " g")))
	if err != nil {
		log.Fatalf("can't parse current brightness %+v\n", err)
	}

	b.max, err = strconv.Atoi(runShell((xb + " m")))
	if err != nil {
		log.Fatalf("can't parse max brightness %+v\n", err)
	}

	debugPf("brightness: %+v\n", b)

	sq := math.Sqrt(float64(b.current))
	if sq < 1 {
		sq = 1
	}

	debugPl(sq)

	switch {
	case cfg.dec:
		b.set = int(math.Max(float64(b.current)-sq, 0))
	case cfg.inc:
		b.set = int(math.Min(math.Max(float64(b.current)+sq, 0), float64(b.max)))
	}

	debugPl("will be in percents", b.willBeInPercents())
	debugPl("will set to", b.set)

	runShell(xb + " s " + strconv.Itoa(b.set))

	notifier, err := newNotifier()
	if err != nil {
		log.Printf("failed to created notifier: %+v", err)
		os.Exit(3)
	}

	nID, err := notifier.SendNotification(notify.Notification{
		AppName:       "go-brightness",
		ReplacesID:    getID(),
		AppIcon:       brightnessIcon,
		Body:          fmt.Sprintf("Brightness set to: %d%%", b.willBeInPercents()),
		Hints:         map[string]dbus.Variant{"value": dbus.MakeVariant(b.willBeInPercents())},
		ExpireTimeout: time.Second,
	})
	// n.set_hint("value", now100) # only hardware brightness
	if err != nil {
		log.Printf("failed to created notification: %+v", err)
		os.Exit(3)
	}

	debugPl("notification id =", nID)

	if err := saveID(nID); err != nil {
		log.Printf("failed to save notification id to %q: %+v", idPath, err)
		os.Exit(4)
	}
}

func newNotifier() (notify.Notifier, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed connecting to dbus: %w", err)
	}

	notifier, err := notify.New(conn)
	if err != nil {
		return nil, fmt.Errorf("failed creating notifier: %w", err)
	}

	return notifier, nil
}

func getID() uint32 {
	if _, err := os.Stat(idPath); errors.Is(err, os.ErrNotExist) {
	} else if err != nil {
		return 0
	}

	bb, err := os.ReadFile(idPath)
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
	return os.WriteFile(idPath, []byte(strconv.Itoa(int(id))), 0o777)
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
