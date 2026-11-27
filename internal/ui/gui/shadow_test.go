package gui

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/ipc"
)

const shadowProfile = `schema = 1
name = "Shadow"
[match]
type = "gamepad"
[[layout]]
name = "One"
[layout.bindings]
ABS_Z = { note = "C3", threshold = 0.5 }
ABS_X = { cc = 7 }
ABS_Y = { pitch_bend = true, centered = true }
"ABS_RX+" = { cc = 20 }
"ABS_RX-" = { cc = 21 }
BTN_A = "D3"
BTN_START = "layout_next"
[[layout]]
name = "Two"
[layout.bindings]
ABS_Z = { cc = 9, invert = true }
`

// The page shows what the engine HIDI plays with makes of what the device
// does: held follows the hysteresis, values are those that would be sent.
func TestShadow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shadow.toml")
	if err := os.WriteFile(path, []byte(shadowProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	x, y, z, rx := control.Axis(0), control.Axis(1), control.Axis(2), control.Axis(3)
	btnA := control.Key(0x130)
	device := func(zAt float64, extra ...ipc.Axis) *ipc.Instrument {
		in := &ipc.Instrument{
			Buttons: []ipc.Button{{Name: "BTN_SOUTH", Down: true}, {Name: "BTN_START"}},
			Axes:    append([]ipc.Axis{{Name: "ABS_Z", Value: zAt, Known: true}}, extra...),
		}
		return in
	}
	var s Shadow
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }

	// A hysteresis of a fifth: pressed at 0.6, let go at 0.4, and in
	// between as it was.
	for _, step := range []struct {
		at   float64
		held bool
	}{{0.55, false}, {0.61, true}, {0.5, true}, {0.41, true}, {0.39, false}, {0.55, false}} {
		s.Follow(doc, "One", 0.2, device(step.at))
		if got := s.Held(Whole(z)); got != step.held {
			t.Errorf("the axis at %v: held %v, want %v", step.at, got, step.held)
		}
	}
	if !s.Held(Whole(btnA)) {
		t.Error("a button that is down does not count as held")
	}

	// Values: nothing before the axis said where it stands, then what
	// MIDI would carry.
	s.Follow(doc, "One", 0.2, device(0, ipc.Axis{Name: "ABS_X", Value: 0.3}))
	if _, _, ok := s.Level(Whole(x)); ok {
		t.Error("an axis that has reported nothing has a level")
	}
	s.Follow(doc, "One", 0.2, device(0,
		ipc.Axis{Name: "ABS_X", Value: 0.5, Known: true},
		ipc.Axis{Name: "ABS_Y", Value: 0.25, Known: true},
		ipc.Axis{Name: "ABS_RX", Value: 1, Known: true}))
	if from, to, ok := s.Level(Whole(x)); !ok || from != 0 || !near(to, 64.0/127) {
		t.Errorf("the controller: %v to %v, %v", from, to, ok)
	}
	if from, to, ok := s.Level(Whole(y)); !ok || !near(from, 0.25) || !near(to, 0.5) {
		t.Errorf("the pitch bend: %v to %v, %v", from, to, ok)
	}
	if _, to, ok := s.Level(Spot{rx, engine.RangeUpper}); !ok || !near(to, 1) {
		t.Errorf("the upper half pushed all the way: %v, %v", to, ok)
	}
	if _, to, ok := s.Level(Spot{rx, engine.RangeLower}); !ok || !near(to, 0) {
		t.Errorf("the lower half meanwhile: %v, %v", to, ok)
	}
	if _, _, ok := s.Level(Whole(z)); ok {
		t.Error("an axis that plays a note has a level")
	}

	// Where the axes rest.
	for code, want := range map[control.Code]float64{x: 0, y: 0.5, rx: 0.5, z: 0} {
		if at, ok := s.Neutral(code); !ok || at != want {
			t.Errorf("%s rests at %v, %v; want %v", code, at, ok, want)
		}
	}
	if _, ok := s.Neutral(control.Axis(9)); ok {
		t.Error("an axis nothing reads has a place to rest")
	}

	// The command of the device that steps through the layouts leaves
	// the shadow in the layout the page shows.
	pressed := device(0)
	pressed.Buttons[1].Down = true
	s.Follow(doc, "One", 0.2, pressed)
	if at, ok := s.Neutral(z); !ok || at != 0 {
		t.Errorf("after layout_next on the device, the axis rests at %v, %v", at, ok)
	}
	// Another layout of the page is another engine: there the axis is a
	// reversed controller.
	s.Follow(doc, "Two", 0.2, device(0.25))
	if at, _ := s.Neutral(z); at != 1 {
		t.Errorf("in the second layout the axis rests at %v, want 1", at)
	}
	if _, to, ok := s.Level(Whole(z)); !ok || !near(to, 0.75) {
		t.Errorf("in the second layout the axis sends %v, %v", to, ok)
	}
	// A change to the profile is followed too: the axis plays a note
	// now, reversed as it was.
	if err := doc.SetNote("Two", z, 60); err != nil {
		t.Fatal(err)
	}
	s.Follow(doc, "Two", 0.2, device(0.1))
	if !s.Held(Whole(z)) {
		t.Error("the changed profile is not what the shadow plays")
	}
	// With no device, what was held stays, and nothing breaks.
	s.Follow(doc, "Two", 0.2, nil)
	if !s.Held(Whole(z)) {
		t.Error("without a device the shadow forgot what was held")
	}
	var empty Shadow
	if empty.Held(Whole(z)) {
		t.Error("a shadow of nothing holds something")
	}
	if _, _, ok := empty.Level(Whole(x)); ok {
		t.Error("a shadow of nothing sends something")
	}
}
