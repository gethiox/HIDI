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
		slices.SortFunc(t.buttons, orderButtons)
		t.buttonAt = make(map[inputKey]int, len(t.buttons))
		for i, b := range t.buttons {
			t.buttonAt[inputKey{b.Part, b.Code}] = i
		}
		return true
	case control.KindAxis:
		value := math.Round(ev.Value/axisStep) * axisStep
		if i, seen := t.axisAt[key]; seen {
			if t.axes[i].Value == value {
				return false
			}
			t.axes[i].Value = value
			return true
		}
		t.axes = append(t.axes, Axis{Code: ev.Code, Part: ev.Part, Value: value})
		slices.SortFunc(t.axes, orderAxes)
		t.axisAt = make(map[inputKey]int, len(t.axes))
		for i, a := range t.axes {
			t.axisAt[inputKey{a.Part, a.Code}] = i
		}
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

// axesNow returns the axes with where each stands, ordered by part and
// code.
func (t *inputTracker) axesNow() []Axis { return slices.Clone(t.axes) }

func (t *inputTracker) snapshot() Input {
	in := Input{Buttons: slices.Clone(t.buttons), Axes: slices.Clone(t.axes)}
	// Never nil: whoever shows the lists tells "none yet" from "not known".
	if in.Buttons == nil {
		in.Buttons = []Button{}
	}
	if in.Axes == nil {
		in.Axes = []Axis{}
	}
	return in
}
