package instrument

import (
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

func TestSteadier(t *testing.T) {
	axis := func(v float64) control.Event { return control.Event{Code: control.Axis(6), Value: v} }
	start := time.Unix(1000, 0)
	at := func(ms int) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }

	t.Run("a wavering axis is passed on once", func(t *testing.T) {
		s := newSteadier(0.01)
		if !s.admit(axis(0.500), at(0)) {
			t.Fatal("the first position was dropped")
		}
		for i := 1; i < 1000; i++ {
			v := 0.500
			if i%2 == 1 {
				v = 0.504
			}
			if s.admit(axis(v), at(i*8)) {
				t.Fatalf("position %v at step %d was passed on", v, i)
			}
		}
	})

	t.Run("a moving axis is passed on in full", func(t *testing.T) {
		s := newSteadier(0.01)
		s.admit(axis(0.5), at(0))
		if s.admit(axis(0.505), at(10)) {
			t.Error("a movement below the threshold was passed on")
		}
		if !s.admit(axis(0.52), at(20)) {
			t.Fatal("a clear movement was dropped")
		}
		// While it moves, steps smaller than the threshold arrive too.
		for i, v := range []float64{0.521, 0.522, 0.523, 0.522} {
			if !s.admit(axis(v), at(30+i*10)) {
				t.Errorf("position %v of a moving axis was dropped", v)
			}
		}
	})

	t.Run("an axis that stays put rests again", func(t *testing.T) {
		s := newSteadier(0.01)
		s.admit(axis(0.5), at(0))
		s.admit(axis(0.6), at(10))
		if !s.admit(axis(0.601), at(100)) {
			t.Error("dropped while still moving")
		}
		if s.admit(axis(0.602), at(600)) {
			t.Error("passed on after the axis had come to rest")
		}
		if !s.admit(axis(0.62), at(700)) {
			t.Error("the next clear movement was dropped")
		}
	})

	t.Run("a slow movement gets through", func(t *testing.T) {
		s := newSteadier(0.01)
		s.admit(axis(0.5), at(0))
		passed := 0
		// A thousandth of the travel each second: every step is far
		// below the threshold and far apart in time.
		for i := 1; i <= 100; i++ {
			if s.admit(axis(0.5+float64(i)*0.001), at(i*1000)) {
				passed++
			}
		}
		if passed < 8 {
			t.Errorf("only %d of 100 slow steps over a tenth of the travel were passed on", passed)
		}
	})

	t.Run("the ends always arrive", func(t *testing.T) {
		s := newSteadier(0.01)
		s.admit(axis(0.995), at(0))
		if !s.admit(axis(1), at(1000)) {
			t.Error("the end of the travel was dropped")
		}
		if s.admit(axis(1), at(1010)) {
			t.Error("the end was passed on twice")
		}
		if s.admit(axis(0.996), at(1020)) {
			t.Error("wavering next to the end was passed on")
		}
	})

	t.Run("keys and a threshold of zero pass", func(t *testing.T) {
		s := newSteadier(0.01)
		key := control.Event{Code: control.Key(30), Value: 1}
		if !s.admit(key, at(0)) || !s.admit(key, at(1)) {
			t.Error("a key was dropped")
		}
		off := newSteadier(0)
		if !off.admit(axis(0.5), at(0)) || !off.admit(axis(0.5001), at(1)) {
			t.Error("a threshold of zero dropped a position")
		}
	})
}
