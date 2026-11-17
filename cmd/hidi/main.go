// Command hidi turns keyboards and game controllers into MIDI instruments.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/spf13/pflag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/backend"
	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/crashlog"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/light/openrgb"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/settings"
)

const usage = `HIDI turns keyboards and game controllers into MIDI instruments.

Usage:
  hidi [options]            start HIDI: the instruments run, and an icon in
                            the tray opens the window and turns HIDI on and off
  hidi --headless [options] run the instruments only, in the terminal
  hidi <command> [options]

Commands:
  run       the same as "hidi --headless"
  devices   list the connected input devices and the profile each would use
  status    show what the running HIDI is doing
  monitor   keep showing it, with the log, until interrupted
  on, off, toggle
            turn the running HIDI on or off
  gui       open the window of the running HIDI: the instruments, what is
            played, the log and the settings
  tray      show an icon for the running HIDI in the tray
  ports     list the MIDI ports an instrument can be connected to
  config    show where HIDI keeps its settings and profiles
  profiles  list the profiles, check them, or write the built-in ones out
  doctor    check that this system can do what HIDI needs, step by step
  version   print version information
  help      show this help

Options are written with two dashes, as in --no-grab or --toggle=KEY_CAPSLOCK;
those of one letter take one, as in -v. "hidi run --help" lists the options
of HIDI itself, and "hidi <command> --help" those of a command.
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
	if !startsHIDI(args) && args[0] != "doctor" {
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

// startsHIDI reports whether the command line starts HIDI itself: as the
// application, which is what no command means, or with "run".
func startsHIDI(args []string) bool {
	if len(args) == 0 || args[0] == "run" {
		return true
	}
	switch args[0] {
	case "-h", "--help", "--version":
		return false
	}
	return strings.HasPrefix(args[0], "-")
}

// run executes the command line and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if startsHIDI(args) {
		if len(args) > 0 && args[0] == "run" {
			return cmdRun(ctx, args[1:], stderr, false)
		}
		return cmdRun(ctx, args, stderr, true)
	}
	switch args[0] {
	case "devices":
		return cmdDevices(args[1:], stdout, stderr)
	case "status", "on", "off", "toggle":
		return cmdControl(ctx, args[0], args[1:], stdout, stderr)
	case "monitor":
		return cmdMonitor(ctx, args[1:], stdout, stderr)
	case "gui":
		return cmdGui(ctx, args[1:], stderr)
	case "tray":
		return cmdTray(ctx, args[1:], stderr)
	case "ports":
		return cmdPorts(args[1:], stdout, stderr)
	case "doctor":
		return cmdDoctor(ctx, args[1:], stdout, stderr)
	case "config":
		return cmdConfig(args[1:], stdout, stderr)
	case "profiles":
		return cmdProfiles(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, buildinfo.Get())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "hidi: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// parseFlags parses the options of a command. It returns false if the
// command should not run, with the exit code to use.
func parseFlags(fs *pflag.FlagSet, args []string, stderr io.Writer) (ok bool, code int) {
	fs.SetOutput(stderr)
	who := "hidi " + fs.Name()
	if fs.Name() == "hidi" {
		who = "hidi"
	}
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Options of %q:\n%s", who, fs.FlagUsages())
	}
	switch err := fs.Parse(args); {
	case err == pflag.ErrHelp:
		return false, 0
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", who, err)
		if long := oneDashLong(args); long != "" {
			fmt.Fprintf(stderr, "Options of more than one letter take two dashes: -%s\n", long)
		}
		fmt.Fprintf(stderr, "Run \"%s --help\" for the options.\n", who)
		return false, 2
	case fs.NArg() > 0:
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", who, fs.Arg(0))
		return false, 2
	}
	return true, 0
}

// oneDashLong returns the first argument that looks like an option of
// several letters written with one dash, as Go's own tools take them, or
// an empty string.
func oneDashLong(args []string) string {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		name, _, _ := strings.Cut(arg, "=")
		if len(name) > 2 && name[0] == '-' && name[1] != '-' {
			return arg
		}
	}
	return ""
}

// cmdRun runs the instruments. As the application it also shows the icon
// in the tray and opens the window; see [desktop].
func cmdRun(ctx context.Context, args []string, stderr io.Writer, app bool) int {
	ctx, quit := context.WithCancel(ctx)
	defer quit()
	who, name := "hidi run", "run"
	if app {
		who, name = "hidi", "hidi"
	}
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	headless, showWindow := new(bool), new(bool)
	*headless = !app
	if app {
		fs.BoolVar(headless, "headless", false, "run the instruments only, without the icon in the tray and the window")
		fs.BoolVar(showWindow, "window", false, "open the window at once, whatever the settings say")
	}
	verbose := fs.BoolP("verbose", "v", false, "also log details, such as every ignored device")
	logMIDI := fs.Bool("log-midi", false, "also log every MIDI message sent, for finding faults; implies -v")
	noStatus := fs.Bool("no-status", false, "write the log only, without the lines about HIDI's state at the bottom of the terminal")
	toggleFlag := fs.String("toggle", defaultToggle,
		"keys that, pressed together on one device, turn HIDI off and on;\nan empty value disables the combination")
	toggleTap := fs.Bool("toggle-tap", false, "the toggle is one key and works by a tap; held for longer than half a second,\nthe key does what it normally does")
	alwaysGrab := fs.Bool("always-grab", false, "on Linux, keep keyboards grabbed while HIDI is off too and pass their keys on,\nso that the tapped toggle key is hidden from the system")
	noGrab := fs.Bool("no-grab", false, "leave keyboards usable by the rest of the system while HIDI is on")
	grabGamepads := fs.Bool("grab-gamepads", false, "also take game controllers away from the rest of the system")
	noColour := fs.Bool("no-color", false, "write the log without colours")
	root, profilesDir := placeFlags(fs)
	var connect listFlag
	fs.Var(&connect, "connect", "also send to the existing MIDI `port` with this name or ID, as listed by \"hidi ports\"; may be repeated")
	lightsFlag := fs.String("openrgb", "", "light the keys through the OpenRGB server at this `address`, such as "+settings.DefaultOpenRGBAddress)
	startOff := fs.Bool("off", false, "start turned off; the toggle combination turns HIDI on")
	socket := socketFlag(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	if !*headless {
		// Started by a double click on Windows, HIDI has a console
		// window of its own that nobody asked for.
		leaveOwnConsole()
	}

	// The settings file gives what the command line leaves open. Nothing
	// is written yet: a HIDI that stops over its options, or because one
	// is running already, leaves the directory as it is.
	pl := findPlace(*root, *profilesDir, false)
	given := map[string]bool{}
	fs.Visit(func(f *pflag.Flag) { given[f.Name] = true })
	toggle := pl.settings.Input.Toggle
	if given["toggle"] {
		var err error
		if toggle, err = parseToggle(*toggleFlag); err != nil {
			fmt.Fprintf(stderr, "%s: --toggle: %v\n", who, err)
			return 2
		}
	}
	if !given["no-grab"] {
		*noGrab = !pl.settings.Input.GrabKeyboards
	}
	if !given["grab-gamepads"] {
		*grabGamepads = pl.settings.Input.GrabGamepads
	}
	if !given["off"] {
		*startOff = !pl.settings.Input.StartOn
	}
	if !given["toggle-tap"] {
		*toggleTap = pl.settings.Input.ToggleTap
	}
	if !given["always-grab"] {
		*alwaysGrab = pl.settings.Input.AlwaysGrab
	}
	if *toggleTap && len(toggle) != 1 {
		fmt.Fprintln(stderr, who+": --toggle-tap needs a toggle of exactly one key, such as --toggle=KEY_CAPSLOCK")
		return 2
	}
	if !given["connect"] {
		connect = pl.settings.Output.Connect
	}
	var err error
	if len(toggle) == 0 && !*noGrab {
		// A grabbed keyboard cannot type Ctrl+C, so without the combination
		// a user with one keyboard could not get it back.
		fmt.Fprintln(stderr, who+": grabbing keyboards needs a toggle combination to release them; set --toggle or pass --no-grab")
		return 2
	}
	if len(toggle) == 0 && *startOff {
		fmt.Fprintln(stderr, who+": --off needs a toggle combination to turn HIDI on")
		return 2
	}

	level := slog.LevelInfo
	if *verbose || *logMIDI {
		level = slog.LevelDebug
	}
	file, isFile := stderr.(*os.File)
	colour := !*noColour && isFile && logging.TerminalColour(file)
	// On a terminal the last lines show the state of HIDI, and the log
	// runs above them.
	var footer *logging.Footer
	logOutput := stderr
	if !*noStatus && isFile && logging.IsTerminal(file) {
		footer = logging.NewFooter(stderr)
		logOutput = footer
	}
	logs := logging.New(logging.Options{
		Output:    logOutput,
		Colour:    colour,
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
		case errors.Is(err, ipc.ErrRunning) && !*headless:
			// Starting the application again shows the one that runs.
			windowArgs := []string{"--socket", *socket}
			if given["config"] {
				windowArgs = append(windowArgs, "--config", *root)
			}
			return cmdGui(ctx, windowArgs, stderr)
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
	// Now HIDI's directory is made ready: filled on the first start.
	pl = findPlace(*root, *profilesDir, true)
	for _, note := range pl.notes {
		configLog.Info(note)
	}
	for _, problem := range pl.problems {
		configLog.Warn(problem)
	}
	profiles, problems, err := pl.load()
	if err != nil {
		configLog.Error("loading the built-in profiles failed", "error", err)
		return 1
	}
	reportProblems(problems)
	if pl.profiles != "" {
		configLog.Info("profiles loaded", "directory", pl.profiles, "profiles", len(profiles))
	}
	source, err := backend.Devices(func(what string, err error) {
		inputLog.Warn("device cannot be used", "device", what, "error", err)
	})
	if err != nil {
		log.Error("watching for input devices failed", "error", err)
		return 1
	}
	defer source.Close()

	grabPolicy := func(keyboards, gamepads bool) func(control.DeviceInfo) bool {
		return func(d control.DeviceInfo) bool {
			switch d.Type {
			case control.DeviceGamepad:
				return gamepads
			case control.DeviceMouse:
				return false // taking the pointer away is never wanted
			}
			return keyboards
		}
	}
	grab := grabPolicy(!*noGrab, *grabGamepads)

	log.Info("HIDI started; press Ctrl+C to stop", "version", buildinfo.Get().String())
	if len(toggle) > 0 {
		log.Info("press the toggle combination to turn HIDI off and on", "keys", formatToggle(toggle))
	}
	if !*noGrab {
		log.Info("keyboards do not type while HIDI is on; turn it off to get them back")
	}
	openEndpoint := backend.OpenEndpoint
	// Lighting the keys: a hub that talks to OpenRGB, and MIDI devices
	// that other programs can send to, so that their notes show too.
	lightsAddress := ""
	switch {
	case given["openrgb"]:
		lightsAddress = *lightsFlag
	case pl.settings.Lights.OpenRGB:
		lightsAddress = pl.settings.Lights.Address
	}
	var lights func(control.DeviceInfo) instrument.Light
	lightsDone := make(chan struct{})
	if lightsAddress != "" {
		hub := light.NewHub(light.Options{
			Connect: func() (light.Server, error) { return openrgb.Connect(lightsAddress, "HIDI") },
			Logger:  logs.Logger("lights"),
		})
		go func() {
			defer close(lightsDone)
			hub.Run(ctx)
		}()
		lights = func(info control.DeviceInfo) instrument.Light { return hub.Attach(info) }
		openEndpoint = backend.OpenDuplexEndpoint
		log.Info("keys are lit through OpenRGB", "address", lightsAddress)
	} else {
		close(lightsDone)
	}
	plainEndpoint := openEndpoint
	if len(connect) > 0 {
		// A port that is not there yet is connected when it appears, so a
		// MIDI interface can be plugged in later.
		router := output.NewRouter(backend.Ports, output.MatchPorts(connect), logs.Logger("output"))
		go router.Run(ctx, time.Second)
		openEndpoint = func(name string) (output.Endpoint, error) {
			e, err := plainEndpoint(name)
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
		ToggleTap:     *toggleTap,
		HideToggle:    hidesToggle(*alwaysGrab),
		StartInactive: *startOff,
		Lights:        lights,
		LogMIDI:       *logMIDI,
		Changed:       changes.Notify,
	})
	var shownToggle atomic.Pointer[string]
	showToggle := func(keys []control.Code) {
		text := formatToggle(keys)
		shownToggle.Store(&text)
		changes.Notify()
	}
	showToggle(toggle)
	footerDone := make(chan struct{})
	stopFooter := func() {}
	if footer != nil {
		changed, stop := changes.Listen()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stopFooter = cancel
		go func() {
			defer close(footerDone)
			defer stop()
			for {
				footer.Set(footerLines(snapshot(manager), *shownToggle.Load(), colour))
				select {
				case <-ctx.Done():
					// What is logged while stopping stays on the screen.
					footer.Close()
					return
				case <-changed:
				}
				// A played instrument changes with every key; the eye
				// does not need more than this.
				time.Sleep(footerPause)
			}
		}()
	} else {
		close(footerDone)
	}
	served := make(chan struct{})
	host := controlHost(manager, logs, &changes)
	if server != nil {
		go func() {
			defer close(served)
			server.Serve(ctx, host)
		}()
	} else {
		close(served)
	}
	desktopDone := make(chan struct{})
	if *headless {
		close(desktopDone)
	} else {
		d := desktop{show: *showWindow || pl.settings.Window.ShowAtStart, host: host, log: logs.Logger("desktop"), quit: quit}
		if server != nil {
			d.windowArgs = []string{"--socket", *socket}
			if given["config"] {
				d.windowArgs = append(d.windowArgs, "--config", *root)
			}
		}
		go func() {
			defer close(desktopDone)
			d.run(ctx)
		}()
	}
	if pl.profiles != "" {
		go config.Watch(ctx, pl.profiles, time.Second, pl.load, func(profiles []*config.Profile, problems []config.Problem) {
			configLog.Info("profiles changed; loading them again", "profiles", len(profiles))
			reportProblems(problems)
			manager.SetProfiles(profiles)
		})
	}
	if pl.managed {
		// Settings changed in the file, by hand or from the window, are
		// taken over while HIDI runs, as far as they can be: the toggle
		// combination and what is grabbed. What the command line set
		// stays as it was set there.
		settingsFile := pl.paths.Settings
		go settings.Watch(ctx, settingsFile, time.Second, func(s settings.Settings, err error) {
			if err != nil {
				configLog.Error("the changed settings have mistakes and are not used", "file", settingsFile, "error", err)
				return
			}
			newToggle, keyboards, gamepads := toggle, !*noGrab, *grabGamepads
			if !given["toggle"] {
				newToggle = s.Input.Toggle
			}
			if !given["no-grab"] {
				keyboards = s.Input.GrabKeyboards
			}
			if !given["grab-gamepads"] {
				gamepads = s.Input.GrabGamepads
			}
			if len(newToggle) == 0 && keyboards {
				configLog.Error("the changed settings are not used: grabbing keyboards needs a toggle combination", "file", settingsFile)
				return
			}
			tap, always := *toggleTap, *alwaysGrab
			if !given["toggle-tap"] {
				tap = s.Input.ToggleTap
			}
			if !given["always-grab"] {
				always = s.Input.AlwaysGrab
			}
			if tap && len(newToggle) != 1 {
				configLog.Error("the changed settings are not used: toggle_tap needs a toggle of exactly one key", "file", settingsFile)
				return
			}
			manager.SetToggleTap(tap, hidesToggle(always))
			manager.SetInput(newToggle, grabPolicy(keyboards, gamepads))
			showToggle(newToggle)
			configLog.Info("settings changed",
				"toggle", formatToggle(newToggle), "grab_keyboards", keyboards, "grab_gamepads", gamepads)
		})
	}
	manager.Run(ctx, source.Devices)
	stopFooter() // also when the manager ended by itself
	<-footerDone
	<-served
	<-desktopDone
	<-lightsDone // the lights are left evenly lit
	log.Info("HIDI stopped")
	return 0
}

// hidesToggle reports whether a tapped toggle key is kept from the system.
// Windows can do that for single keys. Linux can only by holding the whole
// keyboard and typing everything else again, which the user has to ask
// for.
func hidesToggle(alwaysGrab bool) bool {
	return runtime.GOOS == "windows" || alwaysGrab
}

// footerPause is the least time between two drawings of the lines at the
// bottom of the terminal.
const footerPause = 100 * time.Millisecond

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

// Type names the value in the help.
func (l *listFlag) Type() string { return "port" }

func (l *listFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("must not be empty")
	}
	*l = append(*l, strings.TrimSpace(value))
	return nil
}

// cmdPorts lists the MIDI ports that exist on the system and accept input.
func cmdPorts(args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("ports", pflag.ContinueOnError)
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

// cmdProfiles lists the profiles and what is wrong with the files that
// cannot be used, or writes the built-in profiles out as files to edit.
func cmdProfiles(args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("profiles", pflag.ContinueOnError)
	root, profilesDir := placeFlags(fs)
	export := fs.Bool("export", false, "write the built-in profiles into the directory of profiles, as files to start from;\nfiles that are there are kept")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	pl := findPlace(*root, *profilesDir, false)
	pl.tell(stderr, "profiles")
	dir := &pl.profiles
	if *dir == "" && pl.paths.Root != "" {
		dir = &pl.paths.Profiles // not in use yet: where the profiles will be
	}

	if *export {
		if *dir == "" {
			fmt.Fprintln(stderr, "hidi profiles: --export needs a directory; set --profiles")
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

	profiles, problems, err := pl.load()
	if err != nil {
		fmt.Fprintf(stderr, "hidi profiles: loading the built-in profiles failed: %v\n", err)
		return 1
	}
	switch {
	case pl.managed:
		fmt.Fprintf(stdout, "The profiles are the files in %s\n\n", pl.profiles)
	case pl.profiles != "":
		fmt.Fprintf(stdout, "Your profiles are read from %s, beside the built-in ones\n\n", pl.profiles)
	default:
		fmt.Fprint(stdout, "Only the built-in profiles are in use\n\n")
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
	fs := pflag.NewFlagSet("devices", pflag.ContinueOnError)
	root, profilesDir := placeFlags(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	pl := findPlace(*root, *profilesDir, false)
	pl.tell(stderr, "devices")
	profiles, problems, err := pl.load()
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
			line := fmt.Sprintf("%-10s %s  %-40q profile: %s", info.Type, info.Hardware, info.Name, profile)
			if info.Location != "" {
				// Where it is plugged in tells two devices of one
				// model apart, and shows what HIDI took for one device.
				line += "  at " + info.Location
			}
			lines = append(lines, line)
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
