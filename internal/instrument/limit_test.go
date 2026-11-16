package instrument

import (
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

func TestLimiter(t *testing.T) {
	var (
		axisX = control.Axis(0)
		axisY = control.Axis(1)
		start = time.Unix(1000, 0)
		at    = func(ms int) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
		ev    = func(code control.Code, v float64) control.Event { return control.Event{Code: code, Value: v} }
	)
	l := newLimiter(100) // one event per axis every 10 ms

	if !l.admit(ev(axisX, 0.1), at(0)) {
		t.Fatal("the first event of an axis was held back")
	}
	// Further events inside the interval are held; only the newest counts.
	for i, v := range []float64{0.2, 0.3, 0.4} {
		if l.admit(ev(axisX, v), at(1+i)) {
			t.Fatalf("event %d inside the interval went through", i)
		}
	}
	// Another axis has an interval of its own.
	if !l.admit(ev(axisY, 0.5), at(4)) {
		t.Error("one axis held back another")
	}

	out, wait := l.due(at(5), nil)
	if len(out) != 0 || wait != 5*time.Millisecond {
		t.Fatalf("at 5 ms: %d events due, next in %v; want none and 5ms", len(out), wait)
	}
	out, wait = l.due(at(10), nil)
	if len(out) != 1 || out[0].Value != 0.4 || wait != 0 {
		t.Fatalf("at 10 ms: %+v, next in %v; want the newest held event and nothing left", out, wait)
	}
	// The held event started a new interval.
	if l.admit(ev(axisX, 0.6), at(15)) {
		t.Error("an event went through 5 ms after a held one was released")
	}
	if out, _ := l.due(at(20), nil); len(out) != 1 || out[0].Value != 0.6 {
		t.Errorf("at 20 ms: %+v", out)
	}
	// After a pause the axis goes straight through again.
	if !l.admit(ev(axisX, 0.7), at(100)) {
		t.Error("an event after a pause was held back")
	}
}

func TestLimiterOrderOfAnAxisIsKept(t *testing.T) {
	l := newLimiter(100)
	start := time.Unix(1000, 0)
	x := control.Axis(0)
	l.admit(control.Event{Code: x, Value: 0.1}, start)
	l.admit(control.Event{Code: x, Value: 0.2}, start.Add(time.Millisecond))
	// The interval is over, but an older event is still held: a new one
	// must not overtake it and then be overwritten by it.
	if l.admit(control.Event{Code: x, Value: 0.3}, start.Add(50*time.Millisecond)) {
		t.Fatal("an event overtook a held one")
	}
	out, _ := l.due(start.Add(50*time.Millisecond), nil)
	if len(out) != 1 || out[0].Value != 0.3 {
		t.Errorf("due returned %+v, want only the newest position", out)
	}
}

func TestLimiterLeavesKeysAndRelativeAxesAlone(t *testing.T) {
	l := newLimiter(1)
	now := time.Unix(1000, 0)
	for range 5 {
		if !l.admit(control.Event{Code: control.Key(30), Value: 1}, now) {
			t.Fatal("a key was held back")
		}
		if !l.admit(control.Event{Code: control.Relative(8), Value: 1}, now) {
			t.Fatal("a relative axis was held back")
		}
	}
	if out, wait := l.due(now, nil); len(out) != 0 || wait != 0 {
		t.Errorf("something is held: %+v, %v", out, wait)
	}
}

func TestLimiterParts(t *testing.T) {
	l := newLimiter(1)
	now := time.Unix(1000, 0)
	x := control.Axis(0)
	if !l.admit(control.Event{Code: x, Part: "", Value: 1}, now) || !l.admit(control.Event{Code: x, Part: "touchpad", Value: 1}, now) {
		t.Error("the same axis on two parts of a device shares one interval")
	}
}

func TestLimiterUnlimitedAndDrop(t *testing.T) {
	now := time.Unix(1000, 0)
	x := control.Axis(0)
	unlimited := newLimiter(0)
	for range 5 {
		if !unlimited.admit(control.Event{Code: x}, now) {
			t.Fatal("a limiter without a rate held an event back")
		}
	}

	l := newLimiter(10)
	l.admit(control.Event{Code: x, Value: 0.1}, now)
	l.admit(control.Event{Code: x, Value: 0.2}, now)
	l.drop()
	if out, wait := l.due(now.Add(time.Second), nil); len(out) != 0 || wait != 0 {
		t.Errorf("after drop: %+v, %v", out, wait)
	}
}
