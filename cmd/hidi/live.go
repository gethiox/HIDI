package main

import (
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/settings"
)

// grabPolicy says which devices are kept from the system while HIDI is
// on.
func grabPolicy(keyboards, gamepads bool) func(control.DeviceInfo) bool {
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

// allowed says which devices play, by the settings.
func allowed(d settings.Devices) func(control.DeviceInfo) bool {
	return func(info control.DeviceInfo) bool { return d.Allowed(logging.Printable(info.Name)) }
}

// liveSettings holds the settings the running HIDI goes by, and makes it
// follow them when they change. They change in two ways. The window
// hands over every change the user makes there, at once and without
// writing anything: saving is the window's own affair and means nothing
// here. And the file may change, by hand or by that saving.
//
// What the file says is taken over only where the file itself has
// changed: the window also writes it for things only the window cares
// about, and what the user is trying must not be undone by that. What the
// command line set stays as it was set there.
type liveSettings struct {
	started    runOptions
	manager    *instrument.Manager
	hub        *light.Hub
	address    *atomic.Pointer[string] // where the hub looks for OpenRGB
	router     *output.Router
	log        *slog.Logger
	showToggle func([]control.Code)
	changed    func()

	// One at a time: the file and the window may both bring some.
	mu sync.Mutex
	// current are the settings in use, and file what the settings file
	// held when it was last read.
	current, file settings.Settings
	// What was last taken over, to tell what a change is about.
	input   string // the toggle and what is grabbed, as one text
	devices settings.Devices
	lights  settings.Lights
	connect []string
	learn   int
	playing instrument.Playing
}

func newLiveSettings(o runOptions, s settings.Settings, manager *instrument.Manager, hub *light.Hub, address *atomic.Pointer[string], router *output.Router, log *slog.Logger, showToggle func([]control.Code), changed func()) *liveSettings {
	return &liveSettings{
		started: o, manager: manager, hub: hub, address: address, router: router, log: log, showToggle: showToggle, changed: changed,
		current: s, file: s,
		input:   fmt.Sprint(o.toggle, o.grabKeyboards, o.grabGamepads, o.toggleTap),
		devices: s.Devices,
		lights:  s.Lights,
		connect: o.connect,
		learn:   s.Input.LearnThreshold,
		playing: playingOf(s),
	}
}

// playingOf is how every instrument plays beside its profile, by the
// settings.
func playingOf(s settings.Settings) instrument.Playing {
	return instrument.Playing{
		Hysteresis:   float64(s.Input.Hysteresis) / 100,
		VelocityStep: s.Input.VelocityStep,
		Rest:         engine.Rest{PitchBend: s.Output.ResetPitchBend, Controllers: s.Output.ResetControllers},
	}
}

// text returns the settings in use, written as the settings file is.
func (l *liveSettings) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.current.Text())
}

// try takes over settings the window hands over.
func (l *liveSettings) try(s settings.Settings) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.apply(s)
	l.current = s
}

// fileChanged takes over the settings file after it changed, if what it
// says about HIDI changed: a file that was only written for what the
// window shows leaves the settings in use alone.
func (l *liveSettings) fileChanged(s settings.Settings) {
	l.mu.Lock()
	defer l.mu.Unlock()
	was := l.file
	l.file = s
	if reflect.DeepEqual(was, s) {
		return
	}
	l.apply(s)
	l.current = l.file
}

// apply makes HIDI go by s, as far as a running HIDI can. Settings that
// cannot be used change nothing, and that is logged. The caller holds
// l.mu.
func (l *liveSettings) apply(s settings.Settings) {
	o := l.started
	toggle, keyboards, gamepads, tap := o.toggle, o.grabKeyboards, o.grabGamepads, o.toggleTap
	if !o.given["toggle"] {
		toggle = s.Input.Toggle
	}
	for _, from := range []struct {
		option string
		value  *bool
		is     bool
	}{
		{"no-grab", &keyboards, s.Input.GrabKeyboards},
		{"grab-gamepads", &gamepads, s.Input.GrabGamepads},
		{"toggle-tap", &tap, s.Input.ToggleTap},
	} {
		if !o.given[from.option] {
			*from.value = from.is
		}
	}
	if input := fmt.Sprint(toggle, keyboards, gamepads, tap); input != l.input {
		l.input = input
		l.manager.SetToggleAndGrab(toggle, grabPolicy(keyboards, gamepads), tap)
		l.showToggle(toggle)
		l.log.Info("settings changed", "toggle", formatToggle(toggle), "grab_keyboards", keyboards, "grab_gamepads", gamepads)
	}
	if s.Devices.Mode != l.devices.Mode || !slices.Equal(s.Devices.List, l.devices.List) {
		l.devices = s.Devices
		l.manager.SetAllow(allowed(s.Devices))
		l.changed()
		l.log.Info("settings changed", "devices", s.Devices.Mode, "listed", len(s.Devices.List))
	}
	if s.Input.LearnThreshold != l.learn {
		l.learn = s.Input.LearnThreshold
		l.manager.SetLearnThreshold(float64(l.learn) / 100)
		l.log.Info("settings changed", "learn_threshold", l.learn)
	}
	if playing := playingOf(s); playing != l.playing {
		l.playing = playing
		l.manager.SetPlaying(playing)
		l.log.Info("settings changed", "hysteresis", s.Input.Hysteresis, "velocity_step", s.Input.VelocityStep,
			"reset_pitch_bend", s.Output.ResetPitchBend, "reset_controllers", s.Output.ResetControllers)
	}
	// The lights follow at once, unless the command line said where the
	// server is.
	if !o.given["openrgb"] && (s.Lights.OpenRGB != l.lights.OpenRGB || s.Lights.Endpoint() != l.lights.Endpoint()) {
		if s.Lights.Endpoint() != l.lights.Endpoint() {
			to := s.Lights.Endpoint()
			l.address.Store(&to)
			l.hub.Reconnect()
		}
		l.lights.OpenRGB, l.lights.Address = s.Lights.OpenRGB, s.Lights.Address
		l.hub.SetEnabled(s.Lights.OpenRGB)
		l.log.Info("settings changed", "openrgb", s.Lights.OpenRGB, "address", s.Lights.Endpoint())
	}
	if s.Lights.Colours != l.lights.Colours {
		l.lights.Colours = s.Lights.Colours
		l.hub.SetColours(s.Lights.Colours)
		l.changed() // also where the lights are only shown
		l.log.Debug("settings changed: the colours of the keys")
	}
	if !o.given["connect"] && !slices.Equal(s.Output.Connect, l.connect) {
		l.connect = slices.Clone(s.Output.Connect)
		l.router.Choose(l.connect)
		l.log.Info("settings changed", "connect", strings.Join(l.connect, ", "))
	}
}
