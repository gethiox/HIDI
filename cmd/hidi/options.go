package main

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/settings"
)

// runOptions is what HIDI was started with: the command line and, for
// what that leaves open, the settings.
type runOptions struct {
	// headless runs the instruments only; showWindow opens the window
	// at once.
	headless, showWindow bool

	verbose, logMIDI   bool
	noStatus, noColour bool

	// toggle are the keys that turn HIDI off and on.
	toggle                []control.Code
	toggleTap, alwaysGrab bool
	// grabKeyboards and grabGamepads say what is kept from the system
	// while HIDI is on, and startOff that HIDI starts turned off.
	grabKeyboards, grabGamepads bool
	startOff                    bool

	// connect names the MIDI ports every instrument also sends to.
	connect []string
	// lightsOn says that the keys are lit through the OpenRGB server at
	// lightsAddress.
	lightsOn      bool
	lightsAddress string

	root, profilesDir, socket string
	// given says which options the command line set: those stay as they
	// are when the settings change while HIDI runs.
	given map[string]bool
	// place is HIDI's directory as it was found, before anything is
	// written there.
	place place
}

// parseRunOptions reads the command line of "hidi", or of "hidi run"
// where app is false, and fills in the rest from the settings. It reports
// false, with the status to end with, where HIDI is not to start.
func parseRunOptions(args []string, stderr io.Writer, app bool) (o runOptions, code int, ok bool) {
	who, name := "hidi run", "run"
	if app {
		who, name = "hidi", "hidi"
	}
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	o.headless = !app
	if app {
		fs.BoolVar(&o.headless, "headless", false, "run the instruments only, without the icon in the tray and the window")
		fs.BoolVar(&o.showWindow, "window", false, "open the window at once, whatever the settings say")
	}
	fs.BoolVarP(&o.verbose, "verbose", "v", false, "also log details, such as every ignored device")
	fs.BoolVar(&o.logMIDI, "log-midi", false, "also log every MIDI message sent, for finding faults; implies -v")
	fs.BoolVar(&o.noStatus, "no-status", false, "write the log only, without the lines about HIDI's state at the bottom of the terminal")
	toggle := fs.String("toggle", "",
		"keys that, pressed together, turn HIDI off and on from every device, such as KEY_SCROLLLOCK\nor KEY_LEFTCTRL+KEY_RIGHTCTRL; empty leaves it to the profiles (hidi_toggle)")
	fs.BoolVar(&o.toggleTap, "toggle-tap", false, "a toggle of one key works by a tap; held for longer than half a second,\nthe key does what it normally does")
	fs.BoolVar(&o.alwaysGrab, "always-grab", false, "on Linux, keep keyboards grabbed while HIDI is off too and pass their keys on,\nso that the tapped toggle key is hidden from the system")
	noGrab := fs.Bool("no-grab", false, "leave keyboards usable by the rest of the system while HIDI is on")
	fs.BoolVar(&o.grabGamepads, "grab-gamepads", false, "also take game controllers away from the rest of the system")
	fs.BoolVar(&o.noColour, "no-color", false, "write the log without colours")
	root, profilesDir := placeFlags(fs)
	var connect listFlag
	fs.Var(&connect, "connect", "also send to the existing MIDI `port` with this name or ID, as listed by \"hidi ports\"; may be repeated")
	lights := fs.String("openrgb", "", "light the keys through the OpenRGB server at this `address`, such as "+settings.DefaultOpenRGBAddress)
	fs.BoolVar(&o.startOff, "off", false, "start turned off; the toggle turns HIDI on")
	startOn := fs.Bool("on", false, "start turned on, whatever the settings say")
	socket := socketFlag(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return o, code, false
	}
	o.root, o.profilesDir, o.socket = *root, *profilesDir, *socket
	o.given = map[string]bool{}
	fs.Visit(func(f *pflag.Flag) { o.given[f.Name] = true })

	// The settings file gives what the command line leaves open. Nothing
	// is written yet: a HIDI that stops over its options, or because one
	// is running already, leaves the directory as it is.
	o.place = findPlace(o.root, o.profilesDir, false)
	s := o.place.settings
	fail := func(format string, args ...any) (runOptions, int, bool) {
		fmt.Fprintf(stderr, who+": "+format+"\n", args...)
		return o, 2, false
	}
	o.toggle = s.Input.Toggle
	if o.given["toggle"] {
		var err error
		if o.toggle, err = parseToggle(*toggle); err != nil {
			return fail("--toggle: %v", err)
		}
	}
	o.grabKeyboards = !*noGrab
	for _, from := range []struct {
		option string
		value  *bool
		is     bool
	}{
		{"no-grab", &o.grabKeyboards, s.Input.GrabKeyboards},
		{"grab-gamepads", &o.grabGamepads, s.Input.GrabGamepads},
		{"off", &o.startOff, !s.Input.StartOn},
		{"toggle-tap", &o.toggleTap, s.Input.ToggleTap},
		{"always-grab", &o.alwaysGrab, s.Input.AlwaysGrab},
	} {
		if !o.given[from.option] {
			*from.value = from.is
		}
	}
	switch {
	case *startOn && o.given["off"] && o.startOff:
		return fail("--on and --off contradict each other")
	case *startOn:
		o.startOff = false
	}
	o.connect = connect
	if !o.given["connect"] {
		o.connect = s.Output.Connect
	}
	o.lightsOn, o.lightsAddress = s.Lights.OpenRGB, s.Lights.Endpoint()
	if o.given["openrgb"] {
		o.lightsOn, o.lightsAddress = true, *lights
	}
	// Nothing here depends on there being a toggle: without one for
	// every device each goes by its profile's, and a keyboard that has
	// none is not taken from the system.
	return o, 0, true
}
