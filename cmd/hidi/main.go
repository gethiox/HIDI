// Command hidi turns keyboards and game controllers into MIDI instruments.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/backend"
	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/instrument"
)

const usage = `HIDI turns keyboards and game controllers into MIDI instruments.

Usage:
  hidi <command> [options]

Commands:
  run       run the instruments until interrupted
  devices   list the connected input devices and the profile each would use
  version   print version information
  help      show this help

Run "hidi <command> -h" for the options of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "run":
		return cmdRun(ctx, args[1:], stderr)
	case "devices":
		return cmdDevices(args[1:], stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, buildinfo.Get())
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "hidi: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// parseFlags parses the options of a command. It returns false if the
// command should not run, with the exit code to use.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) (ok bool, code int) {
	fs.SetOutput(stderr)
	switch err := fs.Parse(args); {
	case err == flag.ErrHelp:
		return false, 0
	case err != nil:
		return false, 2
	case fs.NArg() > 0:
		fmt.Fprintf(stderr, "hidi %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return false, 2
	}
	return true, 0
}

func cmdRun(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "also log every MIDI message and every ignored device")
	toggleFlag := fs.String("toggle", defaultToggle,
		"keys that, pressed together on one device, turn HIDI off and on;\nan empty value disables the combination")
	noGrab := fs.Bool("no-grab", false, "leave keyboards usable by the rest of the system while HIDI is on")
	grabGamepads := fs.Bool("grab-gamepads", false, "also take game controllers away from the rest of the system")
	startOff := fs.Bool("off", false, "start turned off; the toggle combination turns HIDI on")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	toggle, err := parseToggle(*toggleFlag)
	if err != nil {
		fmt.Fprintf(stderr, "hidi run: -toggle: %v\n", err)
		return 2
	}
	if len(toggle) == 0 && !*noGrab {
		// A grabbed keyboard cannot type Ctrl+C, so without the combination
		// a user with one keyboard could not get it back.
		fmt.Fprintln(stderr, "hidi run: grabbing keyboards needs a toggle combination to release them; set -toggle or pass -no-grab")
		return 2
	}
	if len(toggle) == 0 && *startOff {
		fmt.Fprintln(stderr, "hidi run: -off needs a toggle combination to turn HIDI on")
		return 2
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	profiles, err := config.Builtin()
	if err != nil {
		log.Error("loading the built-in profiles failed", "error", err)
		return 1
	}
	source, err := backend.Devices(func(what string, err error) {
		log.Warn("input device cannot be used", "device", what, "error", err)
	})
	if err != nil {
		log.Error("watching for input devices failed", "error", err)
		return 1
	}
	defer source.Close()

	grab := func(d control.DeviceInfo) bool {
		switch d.Type {
		case control.DeviceGamepad:
			return *grabGamepads
		case control.DeviceMouse:
			return false // taking the pointer away is never wanted
		}
		return !*noGrab
	}

	log.Info("HIDI started; press Ctrl+C to stop", "version", buildinfo.Get().String())
	if len(toggle) > 0 {
		log.Info("press the toggle combination to turn HIDI off and on", "keys", formatToggle(toggle))
	}
	if !*noGrab {
		log.Info("keyboards do not type while HIDI is on; turn it off to get them back")
	}
	manager := instrument.NewManager(instrument.Options{
		Profiles:      profiles,
		OpenEndpoint:  backend.OpenEndpoint,
		Logger:        log,
		Grab:          grab,
		Toggle:        toggle,
		StartInactive: *startOff,
	})
	manager.Run(ctx, source.Devices)
	log.Info("HIDI stopped")
	return 0
}

// defaultToggle is the combination that turns HIDI off and on. It consists
// of modifier keys only, so that pressing it while HIDI is off, when the
// keys reach the desktop, does not trigger anything there, and every
// keyboard has the three keys.
const defaultToggle = "KEY_LEFTCTRL+KEY_LEFTSHIFT+KEY_LEFTALT"

// parseToggle reads a key combination written like "KEY_A+KEY_B".
func parseToggle(s string) ([]control.Code, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var keys []control.Code
	for _, name := range strings.Split(s, "+") {
		code, err := control.ParseCode(strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if code.Kind != control.KindKey {
			return nil, fmt.Errorf("%v is not a key", code)
		}
		if slices.Contains(keys, code) {
			return nil, fmt.Errorf("%v is listed twice", code)
		}
		keys = append(keys, code)
	}
	return keys, nil
}

func formatToggle(keys []control.Code) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.String()
	}
	return strings.Join(names, "+")
}

func cmdDevices(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	profiles, err := config.Builtin()
	if err != nil {
		fmt.Fprintf(stderr, "hidi devices: loading the built-in profiles failed: %v\n", err)
		return 1
	}
	source, err := backend.Devices(func(what string, err error) {
		fmt.Fprintf(stderr, "hidi devices: %s cannot be used: %v\n", what, err)
	})
	if err != nil {
		fmt.Fprintf(stderr, "hidi devices: %v\n", err)
		return 1
	}
	defer source.Close()

	// The devices that are already connected arrive right away. Listing
	// ends once nothing new has arrived for a moment.
	var lines []string
	quiet := time.NewTimer(time.Second)
	defer quiet.Stop()
collect:
	for {
		select {
		case dev, ok := <-source.Devices:
			if !ok {
				break collect
			}
			info := dev.Info()
			dev.Close()
			profile := "-"
			if p := config.Select(profiles, info); p != nil {
				profile = p.Name
			}
			lines = append(lines, fmt.Sprintf("%-10s %s  %-40q profile: %s", info.Type, info.Hardware, info.Name, profile))
			quiet.Reset(500 * time.Millisecond)
		case <-quiet.C:
			break collect
		}
	}

	sort.Strings(lines)
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	if len(lines) == 0 {
		fmt.Fprintln(stderr, "hidi devices: no input devices found")
	}
	return 0
}
