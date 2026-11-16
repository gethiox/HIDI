package gui

import (
	"math"
	"testing"
	"time"
)

func TestGear(t *testing.T) {
	var g gear
	at := time.Now()
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

	// Closed, it stands still however much time passes.
	if a := g.advance(at); a != 0 {
		t.Errorf("a new gear stands at %v", a)
	}
	at = at.Add(time.Hour)
	if a := g.advance(at); a != 0 {
		t.Errorf("a closed gear turned to %v", a)
	}
	// Opened, it starts from where it stands, not from the time it was
	// closed for.
	g.set(true, at)
	if a := g.advance(at); a != 0 {
		t.Errorf("opening made the gear jump to %v", a)
	}
	at = at.Add(2 * time.Second)
	if a := g.advance(at); !near(a, 2*gearSpeed) {
		t.Errorf("after two seconds open: %v", a)
	}
	// Closed again, it keeps its angle; opened again, it goes on from it.
	at = at.Add(time.Second)
	g.set(false, at)
	stopped := g.advance(at)
	if !near(stopped, 3*gearSpeed) {
		t.Errorf("closing: %v", stopped)
	}
	at = at.Add(time.Minute)
	if a := g.advance(at); a != stopped {
		t.Errorf("a closed gear turned from %v to %v", stopped, a)
	}
	g.set(true, at)
	at = at.Add(time.Second)
	if a := g.advance(at); !near(a, 4*gearSpeed) {
		t.Errorf("after opening again: %v", a)
	}
	// It turns clockwise and stays within one turn.
	at = at.Add(10 * time.Minute)
	if a := g.advance(at); a < 0 || a >= 360 {
		t.Errorf("the angle left the circle: %v", a)
	}
}
