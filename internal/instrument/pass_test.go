package instrument

import (
	"errors"
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/light"
)

// passProfile has a layout that leaves the space bar to the system and
// one that does not.
func passProfile(t *testing.T) *config.Profile {
	t.Helper()
	p, err := config.Parse([]byte(`schema = 1
name = "With a passed key"
[match]
type = "keyboard"
[[layout]]
name = "Open"
[layout.bindings]
KEY_Z = "C3"
KEY_SPACE = "passthrough"
KEY_F12 = "layout_next"
[[layout]]
name = "Closed"
[layout.bindings]
KEY_Z = "C3"
KEY_F11 = "layout_previous"
`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var keySpace, keyF11, keyF12 = control.Key(57), control.Key(87), control.Key(88)

// A key the layout leaves to the system gets through the grab, plays
// nothing, and stops getting through in a layout that does not name it.
func TestPassedKeys(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: toggleKeys}, passProfile(t))
	kbd := &passingDevice{fakeDevice: newFakeDevice("Test Keyboard", control.DeviceKeyboard)}
	r.devices <- kbd
	eventually(t, "the keyboard to be grabbed with the space bar open", func() bool {
		return kbd.isGrabbed() && slices.Equal(kbd.openKeys(), []control.Code{keySpace})
	})
	e := r.endpoint(EndpointPrefix + "Test Keyboard")

	// The passed key sends nothing; the others play.
	kbd.set(keySpace, 1)
	kbd.set(keySpace, 0)
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")
	kbd.set(keyZ, 0)

	// The status says what the key is.
	passed := false
	for _, st := range r.manager.Status() {
		for _, key := range st.Keys {
			if _, ok := key.Action.(engine.Passthrough); ok && key.Code == keySpace {
				passed = true
			}
		}
	}
	if !passed {
		t.Error("the status does not say that the space bar is passed on")
	}

	// Another layout: the whole keyboard is held.
	kbd.set(keyF12, 1)
	kbd.set(keyF12, 0)
	eventually(t, "the grab to close", func() bool { return kbd.isGrabbed() && len(kbd.openKeys()) == 0 })
	kbd.set(keyF11, 1)
	kbd.set(keyF11, 0)
	eventually(t, "the grab to open again", func() bool { return slices.Equal(kbd.openKeys(), []control.Code{keySpace}) })

	// Off: the system has the keyboard; on again: as before.
	r.manager.SetActive(false)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() })
	r.manager.SetActive(true)
	eventually(t, "the grab with the open key to return", func() bool {
		return kbd.isGrabbed() && slices.Equal(kbd.openKeys(), []control.Code{keySpace})
	})
}

// A device that cannot let single keys through is grabbed whole.
func TestPassedKeysFallBackToAGrab(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: toggleKeys}, passProfile(t))
	kbd := &passingDevice{fakeDevice: newFakeDevice("Test Keyboard", control.DeviceKeyboard), shareErr: errors.New("no uinput")}
	r.devices <- kbd
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	if open := kbd.openKeys(); len(open) != 0 {
		t.Errorf("open keys on a device that cannot share: %v", open)
	}
	// A plain device, which cannot share at all, is grabbed too.
	plain := newFakeDevice("Plain Keyboard", control.DeviceKeyboard)
	r.devices <- plain
	eventually(t, "the plain keyboard to be grabbed", plain.isGrabbed)
}

// Only allowed devices become instruments, and that follows a change.
func TestAllowedDevices(t *testing.T) {
	allow := func(names ...string) func(control.DeviceInfo) bool {
		return func(info control.DeviceInfo) bool { return slices.Contains(names, info.Name) }
	}
	r := newRigWith(t, Options{Allow: allow("Test Keyboard")})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	pad := newFakeDevice("Test Pad", control.DeviceGamepad)
	mouse := newFakeDevice("Test Mouse", control.DeviceMouse)
	r.plug(kbd)
	r.devices <- pad
	r.devices <- mouse
	instruments := func() []string {
		var names []string
		for _, st := range r.manager.Status() {
			names = append(names, st.Device.Name)
		}
		slices.Sort(names)
		return names
	}
	devices := func() map[string]bool {
		out := map[string]bool{}
		for _, d := range r.manager.Devices() {
			out[d.Info.Name] = d.Allowed
		}
		return out
	}
	eventually(t, "both devices to be known, one of them allowed", func() bool {
		d := devices()
		return len(d) == 2 && d["Test Keyboard"] && !d["Test Pad"]
	})
	if got := instruments(); !slices.Equal(got, []string{"Test Keyboard"}) {
		t.Errorf("instruments: %v", got)
	}
	// The mouse is no instrument with any setting, so it is not offered.
	if _, listed := devices()["Test Mouse"]; listed {
		t.Error("a mouse no profile selects is listed")
	}

	r.manager.SetAllow(allow("Test Pad"))
	eventually(t, "the instruments to swap", func() bool { return slices.Equal(instruments(), []string{"Test Pad"}) })
	r.manager.SetAllow(nil)
	eventually(t, "both to play", func() bool { return len(instruments()) == 2 })
}

// While HIDI is off the lights get the keys that are down, and the status
// tells what the lights show.
func TestLightsShowPressedKeysWhileOff(t *testing.T) {
	lamp := &fakeLight{}
	r := newRigWith(t, Options{Toggle: toggleKeys, StartInactive: true, Lights: func(control.DeviceInfo) Light { return lamp }})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.plug(kbd)
	kbd.set(keyZ, 1)
	eventually(t, "the held key to reach the lights", func() bool {
		s, _ := lamp.last()
		return !s.Active && slices.Equal(s.Pressed, []control.Code{keyZ})
	})
	eventually(t, "the status to tell what the lights show", func() bool {
		st := r.manager.Status()
		return len(st) == 1 && st[0].Lit && st[0].Lights[keyZ] == light.RGB{R: 255} && st[0].LightRest == light.RGB{B: 1}
	})
	kbd.set(keyZ, 0)
	eventually(t, "the released key to go dark", func() bool { s, _ := lamp.last(); return len(s.Pressed) == 0 })
}
