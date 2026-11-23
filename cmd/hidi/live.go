package main

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/logging"
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

// liveSettings makes the running HIDI follow settings that change: in
// the file, by hand or from the window, or tried by the window before
// they are saved. What the command line set stays as it was set there.
// It remembers what was last taken over, to tell what a change is about:
// the window writes the file for things only it cares about, too.
type liveSettings struct {
	started    runOptions
	manager    *instrument.Manager
	hub        *light.Hub
	address    *atomic.Pointer[string] // where the hub looks for OpenRGB
	log        *slog.Logger
	showToggle func([]control.Code)
	changed    func()

	// One at a time: the file and the window may both bring some.
	mu      sync.Mutex
	input   string // the toggle and what is grabbed, as one text
	devices settings.Devices
	lights  settings.Lights
	learn   int
	own     bool
}

func newLiveSettings(o runOptions, manager *instrument.Manager, hub *light.Hub, address *atomic.Pointer[string], log *slog.Logger, showToggle func([]control.Code), changed func()) *liveSettings {
	s := o.place.settings
	return &liveSettings{
		started: o, manager: manager, hub: hub, address: address, log: log, showToggle: showToggle, changed: changed,
		input:   fmt.Sprint(o.toggle, o.grabKeyboards, o.grabGamepads, o.toggleTap, o.alwaysGrab),
		devices: s.Devices,
		lights:  s.Lights,
		learn:   s.Input.LearnThreshold,
		own:     s.Input.ToggleExclusive,
	}
}

// apply takes settings over, as far as a running HIDI can. Settings that
// cannot be used change nothing, and that is logged.
func (l *liveSettings) apply(s settings.Settings) {
	l.mu.Lock()
	defer l.mu.Unlock()
	o := l.started
	toggle, keyboards, gamepads, tap, always := o.toggle, o.grabKeyboards, o.grabGamepads, o.toggleTap, o.alwaysGrab
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
		{"always-grab", &always, s.Input.AlwaysGrab},
	} {
		if !o.given[from.option] {
			*from.value = from.is
		}
	}
	switch {
	case len(toggle) == 0 && keyboards:
		l.log.Error("the changed settings are not used: grabbing keyboards needs a toggle combination")
		return
	case tap && len(toggle) != 1:
		l.log.Error("the changed settings are not used: toggle_tap needs a toggle of exactly one key")
		return
	}
	if input := fmt.Sprint(toggle, keyboards, gamepads, tap, always); input != l.input {
		l.input = input
		l.manager.SetToggleTap(tap, hidesToggle(always))
		l.manager.SetInput(toggle, grabPolicy(keyboards, gamepads))
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
	if s.Input.ToggleExclusive != l.own {
		l.own = s.Input.ToggleExclusive
		l.manager.SetOwnToggle(l.own)
		l.log.Info("settings changed", "toggle_exclusive", l.own)
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
		l.log.Info("settings changed: the colours of the keys")
	}
}
