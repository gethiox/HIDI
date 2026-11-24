package instrument

import (
	"testing"

	"github.com/gethiox/HIDI/internal/control"
)

func TestInputTracker(t *testing.T) {
	var tr inputTracker
	step := func(ev control.Event, want bool) {
		t.Helper()
		if got := tr.record(ev); got != want {
			t.Errorf("record(%v %v) = %v, want %v", ev.Code, ev.Value, got, want)
		}
	}
	a, b := control.Key(30), control.Key(48)
	x, hat := control.Axis(0), control.Axis(0x10)

	step(control.Event{Code: b, Value: 1}, true)
	step(control.Event{Code: a, Value: 1}, true)
	step(control.Event{Code: a, Value: 1}, false) // a held key repeating
	step(control.Event{Code: b, Value: 0}, true)
	step(control.Event{Code: hat, Value: 0.5}, true)
	step(control.Event{Code: x, Value: 0.25}, true)
	step(control.Event{Code: x, Value: 0.2502}, false) // too little to show
	step(control.Event{Code: x, Value: 0.26}, true)
	step(control.Event{Code: x, Part: "Touchpad", Value: 1}, true)
	step(control.Event{Code: control.Relative(0), Value: 1}, false)

	in := tr.snapshot()
	if len(in.Buttons) != 2 || in.Buttons[0] != (Button{Code: a, Down: true}) || in.Buttons[1] != (Button{Code: b}) {
		t.Errorf("buttons: %+v", in.Buttons)
	}
	want := []Axis{{Code: x, Value: 0.26, Said: true}, {Code: hat, Value: 0.5, Said: true}, {Code: x, Part: "Touchpad", Value: 1, Said: true}}
	if len(in.Axes) != len(want) {
		t.Fatalf("axes: %+v", in.Axes)
	}
	for i, w := range want {
		if in.Axes[i] != w {
			t.Errorf("axis %d = %+v, want %+v", i, in.Axes[i], w)
		}
	}
	if in := (&inputTracker{}).snapshot(); in.Buttons == nil || in.Axes == nil {
		t.Error("an empty snapshot has nil lists")
	}
}

// An axis the device lists and has said nothing of is shown, and is not
// among those whose position is known: it would be sent as standing at
// its lower end, a pitch bent all the way down for one.
func TestListedAxisHasNoPosition(t *testing.T) {
	x, z := control.Axis(0), control.Axis(2)
	var tr inputTracker
	tr.seed([]control.Input{{Code: x}, {Code: z}, {Code: control.Axis(0x10)}})
	if in := tr.snapshot(); len(in.Axes) != 3 || in.Axes[0].Said {
		t.Fatalf("the listed axes: %+v", in.Axes)
	}
	if now := tr.axesNow(); len(now) != 0 {
		t.Fatalf("axes with a known position before any report: %+v", now)
	}
	// A report of the lower end is a report all the same.
	if !tr.record(control.Event{Code: z, Value: 0}) {
		t.Error("the first report of an axis at 0 counted as nothing new")
	}
	if now := tr.axesNow(); len(now) != 1 || now[0].Code != z || now[0].Value != 0 {
		t.Errorf("axes with a known position: %+v", now)
	}
}
