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

func TestTrail(t *testing.T) {
	var tr trail
	start := time.Unix(1000, 0)
	tr.Add(5, start)
	tr.Add(7, start.Add(50*time.Millisecond)) // the same step: the newest value counts
	if tr.values[trailLen-1] != 7 || tr.Max() != 7 {
		t.Errorf("within one step: %v", tr.values[trailLen-3:])
	}
	tr.Add(9, start.Add(250*time.Millisecond)) // two steps on
	if got := tr.values[trailLen-3:]; got[0] != 7 || got[1] != 9 || got[2] != 9 {
		t.Errorf("after two steps: %v", got)
	}
	// A long time without anything: the whole trail is the value of now.
	tr.Add(0, start.Add(time.Hour))
	if tr.Max() != 0 {
		t.Errorf("after an hour the trail still has %d in it", tr.Max())
	}
}

func TestMix(t *testing.T) {
	a, b := RGB{0, 100, 200}, RGB{100, 100, 0}
	if Mix(a, b, 0) != a || Mix(a, b, 1) != b || Mix(a, b, 0.5) != (RGB{50, 100, 100}) {
		t.Errorf("Mix: %v %v %v", Mix(a, b, 0), Mix(a, b, 1), Mix(a, b, 0.5))
	}
}
