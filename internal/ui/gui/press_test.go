package gui

import (
	"testing"
	"time"
)

// A held button that repeats clicks after half a second, then every tenth
// of a second, and shows as pressed throughout the wait and for the first
// half of every tenth after.
func TestPressRepeats(t *testing.T) {
	start := time.Unix(1000, 0)
	p := &press{since: start, fired: start}
	at := func(ms int) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	for _, tt := range []struct {
		ms        int
		fire, lit bool
	}{
		{16, false, true}, {250, false, true}, {499, false, true},
		{500, true, true}, {520, false, true}, {549, false, true}, {550, false, false}, {599, false, false},
		{600, true, true}, {617, false, true}, {660, false, false},
		{700, true, true}, {716, false, true},
		// A stall of a second: one click, and on from there.
		{1716, true, true}, {1800, false, false}, {1816, true, true},
	} {
		fire, lit := p.step(at(tt.ms), true)
		if fire != tt.fire || lit != tt.lit {
			t.Errorf("at %d ms: fire %v, lit %v; want %v, %v", tt.ms, fire, lit, tt.fire, tt.lit)
		}
	}
	// One that does not repeat is pressed for as long as it is held.
	q := &press{since: start, fired: start}
	for _, ms := range []int{10, 600, 5000} {
		if fire, lit := q.step(at(ms), false); fire || !lit {
			t.Errorf("a button that does not repeat, at %d ms: fire %v, lit %v", ms, fire, lit)
		}
	}
}
