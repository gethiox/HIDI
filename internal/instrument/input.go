package instrument

import (
	"cmp"
	"math"
	"slices"

	"github.com/gethiox/HIDI/internal/control"
)

// Input is what a device reports about its controls, for showing: every
// button and axis it has reported so far, and where each stands now. A
// control appears once the device has reported it.
type Input struct {
	// Buttons are the keys and buttons, ordered by part and code.
	Buttons []Button
	// Axes are the absolute axes, ordered by part and code.
	Axes []Axis
	// Complete says that the lists hold every control the device has,
	// used or not: the place of a control in its list is then its
	// number, the same whenever a device of that kind is plugged in.
	// Otherwise the lists hold what was used so far.
	Complete bool
}

// Button is a key or button of a device.
type Button struct {
	Code control.Code
	// Part names the part of the device, as in [control.Event].
	Part string
	Down bool
}

// Axis is an absolute axis of a device.
type Axis struct {
	Code control.Code
	Part string
	// Value is the position, from 0 to 1.
	Value float64
	// Said says that the device has reported where the axis stands. An
	// axis the device only listed has not, and its Value means nothing.
	Said bool
}

type inputKey struct {
	part string
	code control.Code
}

// inputTracker follows the events of a device. Both lists are kept in
// order, by part and code, and the maps say where in them a control is: a
// control is new once and reports thousands of times, and the lists are
// asked for many times a second while a window is open.
type inputTracker struct {
	buttons  []Button
	axes     []Axis
	buttonAt map[inputKey]int
	axisAt   map[inputKey]int
	// complete says that seed gave every control the device has.
	complete bool
}

// seed takes in the controls a device says it has, before any is used:
// the buttons up, and the axes where an unmoved axis is not known to be.
func (t *inputTracker) seed(controls []control.Input) {
	known := map[inputKey]bool{}
	for _, b := range t.buttons {
		known[inputKey{b.Part, b.Code}] = true
	}
	for _, a := range t.axes {
		known[inputKey{a.Part, a.Code}] = true
	}
	for _, c := range controls {
		key := inputKey{c.Part, c.Code}
		if known[key] {
			continue
		}
		known[key] = true
		switch c.Code.Kind {
		case control.KindKey:
			t.buttons = append(t.buttons, Button{Code: c.Code, Part: c.Part})
			t.buttonAt = nil
		case control.KindAxis:
			t.axes = append(t.axes, Axis{Code: c.Code, Part: c.Part})
			t.axisAt = nil
		}
	}
	t.index()
	t.complete = len(controls) > 0
}

// index puts the lists in order and notes where everything is.
func (t *inputTracker) index() {
	if t.buttonAt == nil {
		slices.SortFunc(t.buttons, orderButtons)
		t.buttonAt = make(map[inputKey]int, len(t.buttons))
		for i, b := range t.buttons {
			t.buttonAt[inputKey{b.Part, b.Code}] = i
		}
	}
	if t.axisAt == nil {
		slices.SortFunc(t.axes, orderAxes)
		t.axisAt = make(map[inputKey]int, len(t.axes))
		for i, a := range t.axes {
			t.axisAt[inputKey{a.Part, a.Code}] = i
		}
	}
}

func byPartAndCode[T any](part func(T) string, code func(T) uint16) func(a, b T) int {
	return func(a, b T) int {
		return cmp.Or(cmp.Compare(part(a), part(b)), cmp.Compare(code(a), code(b)))
	}
}

var (
	orderButtons = byPartAndCode(func(b Button) string { return b.Part }, func(b Button) uint16 { return b.Code.Num })
	orderAxes    = byPartAndCode(func(a Axis) string { return a.Part }, func(a Axis) uint16 { return a.Code.Num })
)

// axisStep is the smallest change of an axis worth showing: a thousandth
// of its travel.
const axisStep = 0.001

// record takes in an event and reports whether what would be shown
// changed. Relative axes, such as the movement of a mouse, have no
// position to show.
func (t *inputTracker) record(ev control.Event) bool {
	key := inputKey{ev.Part, ev.Code}
	switch ev.Code.Kind {
	case control.KindKey:
		down := ev.Pressed()
		if i, seen := t.buttonAt[key]; seen {
			if t.buttons[i].Down == down {
				return false
			}
			t.buttons[i].Down = down
			return true
		}
		// A new control: put in its place, and the places counted anew.
		t.buttons = append(t.buttons, Button{Code: ev.Code, Part: ev.Part, Down: down})
		t.buttonAt = nil
		t.index()
		return true
	case control.KindAxis:
		value := math.Round(ev.Value/axisStep) * axisStep
		if i, seen := t.axisAt[key]; seen {
			if t.axes[i].Value == value && t.axes[i].Said {
				return false
			}
			t.axes[i].Value, t.axes[i].Said = value, true
			return true
		}
		t.axes = append(t.axes, Axis{Code: ev.Code, Part: ev.Part, Value: value, Said: true})
		t.axisAt = nil
		t.index()
		return true
	}
	return false
}

// keysDown returns the keys of the main part that are down, ordered by
// code.
func (t *inputTracker) keysDown() []control.Code {
	var out []control.Code
	for _, b := range t.buttons {
		if b.Down && b.Part == "" {
			out = append(out, b.Code)
		}
	}
	return out
}

// axesNow returns the axes the device has reported, with where each
// stands, ordered by part and code. An axis the device listed and has
// said nothing of is left out: nobody knows where it stands.
func (t *inputTracker) axesNow() []Axis {
	var out []Axis
	for _, a := range t.axes {
		if a.Said {
			out = append(out, a)
		}
	}
	return out
}

func (t *inputTracker) snapshot() Input {
	in := Input{Buttons: slices.Clone(t.buttons), Axes: slices.Clone(t.axes), Complete: t.complete}
	// Never nil: whoever shows the lists tells "none yet" from "not known".
	if in.Buttons == nil {
		in.Buttons = []Button{}
	}
	if in.Axes == nil {
		in.Axes = []Axis{}
	}
	return in
}
