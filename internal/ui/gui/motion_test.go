package gui

import (
	"testing"
	"time"
)

// The position arrives, quickly, and in the same time whatever the frame
// rate is.
func TestGlide(t *testing.T) {
	arrive := func(frame time.Duration) time.Duration {
		g := glide{Target: 400}
		var took time.Duration
		for g.Step(frame) {
			took += frame
			if took > time.Second {
				t.Fatalf("at %v a frame the position did not arrive: %v", frame, g.At)
			}
		}
		if g.At != 400 {
			t.Fatalf("at %v a frame the position ended at %v", frame, g.At)
		}
		return took
	}
	slow, fast := arrive(time.Second/30), arrive(time.Second/240)
	if fast > 300*time.Millisecond || slow > 300*time.Millisecond {
		t.Errorf("scrolling 400 pixels takes %v at 240 frames a second and %v at 30", fast, slow)
	}
	if d := slow - fast; d > 50*time.Millisecond || d < -50*time.Millisecond {
		t.Errorf("the frame rate changes how long it takes: %v and %v", fast, slow)
	}
	// Most of the way is done at once.
	g := glide{Target: 100}
	for range 12 { // 100 ms at 120 frames a second
		g.Step(time.Second / 120)
	}
	if g.At < 90 {
		t.Errorf("after 100 ms the position is at %v of 100", g.At)
	}
	// A page that became shorter.
	g = glide{Target: 500, At: 500}
	g.Clamp(200)
	if g.Target != 200 || g.At != 200 {
		t.Errorf("after the page shrank: %+v", g)
	}
	g.Target = -50
	g.Clamp(200)
	if g.Target != 0 {
		t.Errorf("before the start: %+v", g)
	}
}

// A trail is drawn so that it runs evenly: at the start of a step the
// second value stands at the left edge and the last but one at the right,
// and by the end of the step each has moved one place to the left.
func TestTrailPlaces(t *testing.T) {
	const count, x, width = 122, 100, 595
	step := float32(width) / float32(count-3)
	near := func(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }
	if !near(TrailX(1, count, 0, x, width), x) || !near(TrailX(count-2, count, 0, x, width), x+width) {
		t.Error("at the start of a step the edges are not on the second value and the last but one")
	}
	if !near(TrailX(2, count, 1, x, width), x) || !near(TrailX(count-1, count, 1, x, width), x+width) {
		t.Error("at the end of a step the values have not moved one place")
	}
	// The oldest and the newest value are outside at the start of a step.
	if TrailX(0, count, 0, x, width) > x-step+0.01 || TrailX(count-1, count, 0, x, width) < x+width+step-0.01 {
		t.Error("the value at an end is in view when it should not be")
	}
	if TrailPhase(50*time.Millisecond, 100*time.Millisecond) != 0.5 || TrailPhase(time.Second, 100*time.Millisecond) != 1 ||
		TrailPhase(-time.Second, 100*time.Millisecond) != 0 || TrailPhase(time.Second, 0) != 1 {
		t.Error("TrailPhase")
	}
}

func TestMix(t *testing.T) {
	a, b := RGB{0, 100, 200}, RGB{100, 100, 0}
	if Mix(a, b, 0) != a || Mix(a, b, 1) != b || Mix(a, b, 0.5) != (RGB{50, 100, 100}) {
		t.Errorf("Mix: %v %v %v", Mix(a, b, 0), Mix(a, b, 1), Mix(a, b, 0.5))
	}
}
