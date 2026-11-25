// Command hidi turns keyboards and game controllers into MIDI instruments.
package main

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/spf13/pflag"

	"github.com/gethiox/HIDI/internal/backend"
	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/crashlog"
	"github.com/gethiox/HIDI/internal/engine"
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

On Windows hidi.exe opens no console window. Started from a command line it
writes there; --console, with any command, gives it a console window of its
own.
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
	os.Exit(guarded(ctx, findConsole(os.Args[1:])))
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
	o, code, ok := parseRunOptions(args, stderr, app)
	if !ok {
		return code
	}
	if !o.headless {
		// Started by a double click on Windows, HIDI has a console
		// window of its own that nobody asked for.
		leaveOwnConsole()
	}
	pl := o.place
	var err error

	level := slog.LevelInfo
	if o.verbose || o.logMIDI {
		level = slog.LevelDebug
	}
	file, isFile := stderr.(*os.File)
	colour := !o.noColour && isFile && logging.TerminalColour(file)
	// On a terminal the last lines show the state of HIDI, and the log
	// runs above them.
	var footer *logging.Footer
	logOutput := stderr
	if !o.noStatus && isFile && logging.IsTerminal(file) {
		footer = logging.NewFooter(stderr)
		footer.Rows = func() int { return logging.TerminalRows(file) }
		logOutput = footer
	}
	logs := logging.New(logging.Options{
		Output:    logOutput,
		Colour:    colour,
		Level:     level,
		KeepLevel: level,
		// Writing the log must never hold up an instrument.
		Queue: 4096,
	})
	defer logs.Close()
	log := logs.Logger("hidi")
	inputLog := logs.Logger("input")

	// The control socket comes first: it is also what tells a second HIDI
	// that one is running, before it would open any device.
	var server *ipc.Server
	if o.socket != "" {
		server, err = ipc.Listen(o.socket)
		switch {
		case errors.Is(err, ipc.ErrRunning) && !o.headless:
			// Starting the application again shows the one that runs.
			windowArgs := []string{"--socket", o.socket}
			if o.given["config"] {
				windowArgs = append(windowArgs, "--config", o.root)
			}
			return cmdGui(ctx, windowArgs, stderr)
		case errors.Is(err, ipc.ErrRunning):
			log.Error("HIDI is already running; see \"hidi status\"", "socket", o.socket)
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
	pl = findPlace(o.root, o.profilesDir, true)
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

	grab := grabPolicy(o.grabKeyboards, o.grabGamepads)

	log.Info("HIDI started; press Ctrl+C to stop", "version", buildinfo.Get().String())
	switch {
	case len(o.toggle) == 0:
		log.Info("the toggle that turns HIDI off and on is each profile's own (" + engine.ToggleName + "); the keyboard profile HIDI comes with has it on Caps Lock")
	case o.toggleTap && len(o.toggle) == 1:
		log.Info("tap the toggle key to turn HIDI off and on", "key", formatToggle(o.toggle))
	default:
		log.Info("press the toggle combination to turn HIDI off and on", "keys", formatToggle(o.toggle))
	}
	if o.grabKeyboards {
		log.Info("keyboards do not type while HIDI is on; turn it off to get them back")
	}
	if o.startOff {
		// Said, because a HIDI that plays nothing looks broken: without
		// a window nothing else tells.
		log.Info("HIDI starts turned off; turn it on with the toggle, with \"hidi on\", or start it with --on")
	}
	openEndpoint := backend.OpenEndpoint
	// Lighting the keys: a hub that talks to OpenRGB, and MIDI devices
	// that other programs can send to, so that their notes show too.
	// The hub is always there and reaches for a server only while the
	// lights are turned on, which the settings can change while HIDI runs.
	lightsOn, lightsAddress := o.lightsOn, o.lightsAddress
	var changes ipc.Notifier
	var address atomic.Pointer[string]
	address.Store(&lightsAddress)
	hub := light.NewHub(light.Options{
		Connect:  func() (light.Server, error) { return openrgb.Connect(*address.Load(), "HIDI") },
		Colours:  pl.settings.Lights.Colours,
		Changed:  changes.Notify,
		Logger:   logs.Logger("lights"),
		Disabled: !lightsOn,
	})
	lightsDone := make(chan struct{})
	go func() {
		defer close(lightsDone)
		hub.Run(ctx)
	}()
	lights := func(info control.DeviceInfo) instrument.Light { return hub.Attach(info) }
	// Two-way MIDI devices, so that the notes other programs send show on
	// the keys when the lights are on.
	openEndpoint = backend.OpenDuplexEndpoint
	if lightsOn {
		log.Info("keys are lit through OpenRGB", "address", lightsAddress)
	}
	// The ports every instrument also sends to. A port that is not there
	// yet is connected when it appears, so a MIDI interface can be
	// plugged in later; and which ports these are can change while HIDI
	// runs.
	plainEndpoint := openEndpoint
	router := output.NewRouter(backend.Ports, output.MatchPorts(nil), logs.Logger("output"))
	router.Choose(o.connect)
	go router.Run(ctx, time.Second)
	openEndpoint = func(name string) (output.Endpoint, error) {
		e, err := plainEndpoint(name)
		if err != nil {
			return nil, err
		}
		return router.Add(strings.TrimPrefix(name, instrument.EndpointPrefix), e), nil
	}
	if len(o.connect) > 0 {
		log.Info("instruments also send to the MIDI ports matching", "ports", strings.Join(o.connect, ", "))
	}
	manager := instrument.NewManager(instrument.Options{
		Allow:          allowed(pl.settings.Devices),
		LearnThreshold: float64(pl.settings.Input.LearnThreshold) / 100,
		Hysteresis:     float64(pl.settings.Input.Hysteresis) / 100,
		Rest:           restOf(pl.settings.Output),
		Profiles:       profiles,
		OpenEndpoint:   openEndpoint,
		Logger:         logs.Logger("instrument"),
		Grab:           grab,
		Toggle:         o.toggle,
		ToggleTap:      o.toggleTap,
		HideToggle:     hidesToggle(o.alwaysGrab),
		OwnToggle:      pl.settings.Input.ToggleExclusive,
		StartInactive:  o.startOff,
		Lights:         lights,
		LogMIDI:        o.logMIDI,
		Changed:        changes.Notify,
	})
	var shownToggle atomic.Pointer[string]
	showToggle := func(keys []control.Code) {
		text := formatToggle(keys)
		shownToggle.Store(&text)
		changes.Notify()
	}
	showToggle(o.toggle)
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
	// Profiles the window tries before they are saved take the place of
	// their files.
	tried := &previews{profiles: profiles}
	host := controlHost(manager, logs, &changes, &seenDevices{})
	// The settings HIDI runs by. The window changes them as the user
	// does, without writing anything; a changed file changes them too.
	live := newLiveSettings(o, pl.settings, manager, hub, &address, router, configLog, func(keys []control.Code) { showToggle(keys) }, changes.Notify)
	host.TrySettings = func(text string) error {
		s, err := settings.Parse([]byte(text))
		if err != nil {
			return err
		}
		live.try(s)
		return nil
	}
	host.Settings = live.text
	host.Preview = func(file, text string) error {
		now, err := tried.set(file, text)
		if err != nil {
			return err
		}
		manager.SetProfiles(now)
		if text == "" {
			configLog.Info("a profile is no longer tried; its file counts again", "file", file)
		} else {
			configLog.Debug("trying a profile that is not saved", "file", file)
		}
		return nil
	}
	if server != nil {
		go func() {
			defer close(served)
			server.Serve(ctx, host)
		}()
	} else {
		close(served)
	}
	desktopDone := make(chan struct{})
	var nothingToShow atomic.Bool
	if o.headless {
		close(desktopDone)
	} else {
		d := desktop{show: o.showWindow || pl.settings.Window.ShowAtStart, host: host, log: logs.Logger("desktop"), quit: quit}
		d.failed = func() { nothingToShow.Store(true) }
		if server != nil {
			d.windowArgs = []string{"--socket", o.socket}
			if o.given["config"] {
				d.windowArgs = append(d.windowArgs, "--config", o.root)
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
			manager.SetProfiles(tried.loaded(profiles))
		})
	}
	if pl.settingsFile != "" {
		settingsFile := pl.settingsFile
		go settings.Watch(ctx, settingsFile, time.Second, func(s settings.Settings, err error) {
			if err != nil {
				configLog.Error("the changed settings have mistakes and are not used", "file", settingsFile, "error", err)
				return
			}
			live.fileChanged(s)
		})
	}
	manager.Run(ctx, source.Devices)
	quit() // also when the manager ended by itself
	stopFooter()
	<-footerDone
	<-served
	<-desktopDone
	<-lightsDone // the lights are left evenly lit
	if nothingToShow.Load() {
		log.Error("HIDI stopped: it had neither an icon nor a window to show itself with; run it with --headless where there is no desktop")
		return 1
	}
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
		if *profilesDir == "" {
			// HIDI's own directory has them already, and keeps them.
			if pl.paths.Factory != "" {
				fmt.Fprintf(stderr, "hidi profiles: the profiles HIDI comes with are kept in\n  %s\nCopy one into\n  %s\nto change it; the window does that with \"Customise\". --export writes them into a directory named with --profiles.\n", pl.paths.Factory, pl.paths.User)
			} else {
				fmt.Fprintln(stderr, "hidi profiles: --export needs a directory; set --profiles")
			}
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
		fmt.Fprintf(stdout, "The factory profiles are in %s\nYour own are in %s and come first where both fit a device\n\n", pl.paths.Factory, pl.paths.User)
	case pl.profiles != "":
		fmt.Fprintf(stdout, "Your profiles are read from %s, beside the built-in ones\n\n", pl.profiles)
	default:
		fmt.Fprint(stdout, "Only the built-in profiles are in use\n\n")
	}
	// Later profiles take precedence, so the user's own come first here.
	for _, p := range slices.Backward(profiles) {
		fmt.Fprintf(stdout, "%-26q %-8s %-34s for %s\n", p.Name, p.Kind, p.Source, p.Match)
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
