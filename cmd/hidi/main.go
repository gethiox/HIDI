// Command hidi turns keyboards and game controllers into MIDI instruments.
package main

import (
	"context"
	"errors"
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
	"github.com/gethiox/HIDI/internal/crashlog"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/output"
)

const usage = `HIDI turns keyboards and game controllers into MIDI instruments.

Usage:
  hidi <command> [options]

Commands:
  run       run the instruments until interrupted
  devices   list the connected input devices and the profile each would use
  status    show what the running HIDI is doing
  monitor   keep showing it, with the log, until interrupted
  on, off, toggle
            turn the running HIDI on or off
  tray      show an icon in the notification area that turns HIDI on and off
  ports     list the MIDI ports an instrument can be connected to
  profiles  list the profiles, check them, or write the built-in ones out
  doctor    check that this system can do what HIDI needs, step by step
  version   print version information
  help      show this help

Run "hidi <command> -h" for the options of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The first interrupt asks HIDI to stop cleanly. From then on the
	// system's own handling is back, so that a second one ends a HIDI that
	// does not manage to.
	go func() {
		<-ctx.Done()
		stop()
	}()
	os.Exit(guarded(ctx, os.Args[1:]))
}

// guarded runs the command line with crashes recorded. The commands that
// run for long, and use the parts most likely to fail, keep a crash report:
// when HIDI dies, the reason is written to a file, and the next start says
// so. A crash needs no cleaning up after: the system takes back a grabbed
// keyboard and removes the MIDI devices of a program that has ended.
func guarded(ctx context.Context, args []string) (code int) {
	if len(args) == 0 || (args[0] != "run" && args[0] != "doctor") {
		return run(ctx, args, os.Stdout, os.Stderr)
	}
	dir, err := crashlog.Dir()
	if err == nil {
		var previous string
		if previous, err = crashlog.Start(dir); previous != "" {
			fmt.Fprintf(os.Stderr, "HIDI did not end properly the last time it ran. What happened is in\n  %s\nPlease include that file if you report it.\n\n", previous)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "hidi: crashes will not be recorded: %v\n", err)
		return run(ctx, args, os.Stdout, os.Stderr)
	}
	// This catches a failure in the command itself. One in another part
	// that runs alongside ends the program at once; the runtime then
	// prints the reason and writes it to the same file.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "\n"+crashlog.Report(r, dir))
			code = 3
		}
	}()
	return run(ctx, args, os.Stdout, os.Stderr)
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
	case "status", "on", "off", "toggle":
		return cmdControl(ctx, args[0], args[1:], stdout, stderr)
	case "monitor":
		return cmdMonitor(ctx, args[1:], stdout, stderr)
	case "tray":
		return cmdTray(ctx, args[1:], stderr)
	case "ports":
		return cmdPorts(args[1:], stdout, stderr)
	case "doctor":
		return cmdDoctor(ctx, args[1:], stdout, stderr)
	case "profiles":
		return cmdProfiles(args[1:], stdout, stderr)
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
	noColour := fs.Bool("no-color", false, "write the log without colours")
	profilesDir := profilesFlag(fs)
	var connect listFlag
	fs.Var(&connect, "connect", "also send to the existing MIDI `port` with this name or ID, as listed by \"hidi ports\"; may be repeated")
	startOff := fs.Bool("off", false, "start turned off; the toggle combination turns HIDI on")
	socket := socketFlag(fs)
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
	file, isFile := stderr.(*os.File)
	logs := logging.New(logging.Options{
		Output:    stderr,
		Colour:    !*noColour && isFile && logging.TerminalColour(file),
		Level:     level,
		KeepLevel: level,
	})
	log := logs.Logger("hidi")
	inputLog := logs.Logger("input")

	// The control socket comes first: it is also what tells a second HIDI
	// that one is running, before it would open any device.
	var server *ipc.Server
	if *socket != "" {
		server, err = ipc.Listen(*socket)
		switch {
		case errors.Is(err, ipc.ErrRunning):
			log.Error("HIDI is already running; see \"hidi status\"", "socket", *socket)
			return 1
		case err != nil:
			log.Warn("no control socket; \"hidi status\" and the like will not reach this HIDI", "error", err)
		default:
			defer server.Close() // for every way out before it is served
		}
	}

	configLog := logs.Logger("config")
	reportProblems := func(problems []config.Problem) {
		for _, p := range problems {
			configLog.Error("profile cannot be used", "file", p.File, "error", p.Err)
		}
	}
	profiles, problems, err := config.Load(*profilesDir)
	if err != nil {
		configLog.Error("loading the built-in profiles failed", "error", err)
		return 1
	}
	reportProblems(problems)
	if *profilesDir != "" {
		configLog.Info("profiles loaded", "directory", *profilesDir, "yours", countUser(profiles), "built-in", len(profiles)-countUser(profiles))
	}
	source, err := backend.Devices(func(what string, err error) {
		inputLog.Warn("device cannot be used", "device", what, "error", err)
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
	openEndpoint := backend.OpenEndpoint
	if len(connect) > 0 {
		// A port that is not there yet is connected when it appears, so a
		// MIDI interface can be plugged in later.
		router := output.NewRouter(backend.Ports, output.MatchPorts(connect), logs.Logger("output"))
		go router.Run(ctx, time.Second)
		openEndpoint = func(name string) (output.Endpoint, error) {
			e, err := backend.OpenEndpoint(name)
			if err != nil {
				return nil, err
			}
			return router.Add(strings.TrimPrefix(name, instrument.EndpointPrefix), e), nil
		}
		log.Info("instruments also send to the MIDI ports matching", "ports", strings.Join(connect, ", "))
	}
	var changes ipc.Notifier
	manager := instrument.NewManager(instrument.Options{
		Profiles:      profiles,
		OpenEndpoint:  openEndpoint,
		Logger:        logs.Logger("instrument"),
		Grab:          grab,
		Toggle:        toggle,
		StartInactive: *startOff,
		Changed:       changes.Notify,
	})
	served := make(chan struct{})
	if server != nil {
		go func() {
			defer close(served)
			server.Serve(ctx, controlHost(manager, logs, &changes))
		}()
	} else {
		close(served)
	}
	if *profilesDir != "" {
		go config.Watch(ctx, *profilesDir, time.Second, func(profiles []*config.Profile, problems []config.Problem) {
			configLog.Info("profiles changed; loading them again", "yours", countUser(profiles))
			reportProblems(problems)
			manager.SetProfiles(profiles)
		})
	}
	manager.Run(ctx, source.Devices)
	<-served
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

// listFlag is an option that may be given several times.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ", ") }

func (l *listFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("must not be empty")
	}
	*l = append(*l, strings.TrimSpace(value))
	return nil
}

// cmdPorts lists the MIDI ports that exist on the system and accept input.
func cmdPorts(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ports", flag.ContinueOnError)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	ports, err := backend.Ports()
	if err != nil {
		fmt.Fprintf(stderr, "hidi ports: %v\n", err)
		return 1
	}
	for _, p := range ports {
		fmt.Fprintf(stdout, "%-8s %s\n", p.ID, p.Name)
	}
	if len(ports) == 0 {
		fmt.Fprintln(stderr, "hidi ports: no MIDI ports found")
	}
	return 0
}

// profilesFlag adds the option that names the directory of the user's
// profiles.
func profilesFlag(fs *flag.FlagSet) *string {
	dir, err := config.DefaultDir()
	if err != nil {
		dir = "" // no home directory: built-in profiles only
	}
	return fs.String("profiles", dir, "`directory` of your profiles; empty uses the built-in profiles only")
}

// countUser returns how many of the profiles are the user's own.
func countUser(profiles []*config.Profile) int {
	n := 0
	for _, p := range profiles {
		if !p.IsBuiltin() {
			n++
		}
	}
	return n
}

// cmdProfiles lists the profiles and what is wrong with the files that
// cannot be used, or writes the built-in profiles out as files to edit.
func cmdProfiles(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("profiles", flag.ContinueOnError)
	dir := profilesFlag(fs)
	export := fs.Bool("export", false, "write the built-in profiles into the directory, as files to start from")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	if *export {
		if *dir == "" {
			fmt.Fprintln(stderr, "hidi profiles: -export needs a directory; set -profiles")
			return 2
		}
		written, skipped, err := config.WriteBuiltin(*dir)
		for _, file := range written {
			fmt.Fprintf(stdout, "written  %s\n", file)
		}
		for _, file := range skipped {
			fmt.Fprintf(stdout, "kept     %s (it exists already)\n", file)
		}
		if err != nil {
			fmt.Fprintf(stderr, "hidi profiles: %v\n", err)
			return 1
		}
		return 0
	}

	profiles, problems, err := config.Load(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "hidi profiles: loading the built-in profiles failed: %v\n", err)
		return 1
	}
	if *dir != "" {
		fmt.Fprintf(stdout, "Your profiles are read from %s\n\n", *dir)
	}
	// Later profiles take precedence, so the user's own come first here.
	for _, p := range slices.Backward(profiles) {
		fmt.Fprintf(stdout, "%-26q %-34s for %s\n", p.Name, p.Source, p.Match)
	}
	for _, p := range problems {
		fmt.Fprintf(stderr, "\nhidi profiles: cannot be used: %v\n", p)
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

func cmdDevices(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	profilesDir := profilesFlag(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	profiles, problems, err := config.Load(*profilesDir)
	if err != nil {
		fmt.Fprintf(stderr, "hidi devices: loading the built-in profiles failed: %v\n", err)
		return 1
	}
	for _, p := range problems {
		fmt.Fprintf(stderr, "hidi devices: profile cannot be used: %v\n", p)
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
