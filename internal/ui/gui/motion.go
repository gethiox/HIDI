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

// TrailPhase says how far the time has gone from one step of a trail to
// the next, from 0 to 1: the trail is drawn moved to the left by that
// part of a step, so that it runs evenly and does not jump once a step.
func TrailPhase(since, step time.Duration) float32 {
	if step <= 0 {
		return 1
	}
	return float32(min(max(float64(since)/float64(step), 0), 1))
}

// TrailX is where value n of a trail of count values is drawn, in an
// area that begins at x and is width wide. One value at each end lies
// outside the area: the oldest on its way out, and the newest on its way
// in, so that none appears or goes all at once.
func TrailX(n, count int, phase, x, width float32) float32 {
	step := width / float32(count-3)
	return x + (float32(n)-1-phase)*step
}

// Mix returns the colour that lies a part of the way from a to b, with
// part from 0 to 1.
func Mix(a, b RGB, part float32) RGB {
	mix := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*part + 0.5) }
	return RGB{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B)}
}
