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

// inputTracker follows the events of a device.
type inputTracker struct {
	buttons map[inputKey]bool
	axes    map[inputKey]float64
}

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
		if t.buttons == nil {
			t.buttons = map[inputKey]bool{}
		}
		down := ev.Pressed()
		if was, seen := t.buttons[key]; seen && was == down {
			return false
		}
		t.buttons[key] = down
		return true
	case control.KindAxis:
		if t.axes == nil {
			t.axes = map[inputKey]float64{}
		}
		value := math.Round(ev.Value/axisStep) * axisStep
		if was, seen := t.axes[key]; seen && was == value {
			return false
		}
		t.axes[key] = value
		return true
	}
	return false
}

func (t *inputTracker) snapshot() Input {
	in := Input{Buttons: make([]Button, 0, len(t.buttons)), Axes: make([]Axis, 0, len(t.axes))}
	for key, down := range t.buttons {
		in.Buttons = append(in.Buttons, Button{Code: key.code, Part: key.part, Down: down})
	}
	for key, value := range t.axes {
		in.Axes = append(in.Axes, Axis{Code: key.code, Part: key.part, Value: value})
	}
	slices.SortFunc(in.Buttons, func(a, b Button) int {
		return cmp.Or(cmp.Compare(a.Part, b.Part), cmp.Compare(a.Code.Num, b.Code.Num))
	})
	slices.SortFunc(in.Axes, func(a, b Axis) int {
		return cmp.Or(cmp.Compare(a.Part, b.Part), cmp.Compare(a.Code.Num, b.Code.Num))
	})
	return in
}
