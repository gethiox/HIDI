package gui

import (
	"math"
	"time"
)

// glide is a position that follows a target: the target is set at once,
// and the position moves to it quickly and evenly, however many frames a
// second are drawn. Scrolling uses it, so that a turn of the wheel is seen
// as a movement and not as a jump.
type glide struct {
	// Target is where the position is going, and At where it is.
	Target, At float32
}

// glideRate says how fast the position closes in on its target: in a
// time of 1/glideRate seconds the distance left falls to about a third.
// At this rate nine tenths of the way are done in 80 ms.
const glideRate = 28

// Step moves the position towards the target for the time that has
// passed and reports whether it is still on its way.
func (g *glide) Step(dt time.Duration) bool {
	left := g.Target - g.At
	if left > -0.5 && left < 0.5 {
		g.At = g.Target
		return false
	}
	g.At += left * float32(1-math.Exp(-dt.Seconds()*glideRate))
	return true
}

// Clamp holds the target, and with it the position in the end, between 0
// and most.
func (g *glide) Clamp(most float32) {
	g.Target = min(max(g.Target, 0), max(most, 0))
	// A page that became shorter must not stay scrolled past its end.
	g.At = min(max(g.At, 0), max(most, 0))
}

// trail is how something went over the last while: values taken at even
// steps of time, the newest last.
type trail struct {
	values []int
	last   time.Time
}

const (
	// trailStep is the time between two values of a trail, and trailLen
	// how many are kept: what is drawn covers twelve seconds.
	trailStep = 100 * time.Millisecond
	trailLen  = 120
)

// Add brings the trail up to now with the value as it is now. Time in
// which nobody added anything is filled with that value too: nothing is
// drawn while nothing changes, and nothing changing means the value
// stayed.
func (t *trail) Add(value int, now time.Time) {
	if t.values == nil {
		t.values = make([]int, trailLen)
		t.last = now
	}
	steps := int(now.Sub(t.last) / trailStep)
	if steps <= 0 {
		t.values[trailLen-1] = value
		return
	}
	steps = min(steps, trailLen)
	copy(t.values, t.values[steps:])
	for i := trailLen - steps; i < trailLen; i++ {
		t.values[i] = value
	}
	t.last = t.last.Add(time.Duration(steps) * trailStep)
	if now.Sub(t.last) > trailStep {
		t.last = now // long idle: start counting from here
	}
}

// Max returns the highest value of the trail.
func (t *trail) Max() int {
	most := 0
	for _, v := range t.values {
		most = max(most, v)
	}
	return most
}

// Mix returns the colour that lies a part of the way from a to b, with
// part from 0 to 1.
func Mix(a, b RGB, part float32) RGB {
	mix := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*part + 0.5) }
	return RGB{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B)}
}
