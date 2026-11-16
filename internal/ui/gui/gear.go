package gui

import "time"

// gearSpeed is how fast the gear turns while the settings are open, in
// degrees a second: once around in twenty seconds.
const gearSpeed = 18

// gear is the wheel in the corner of the window that opens the settings.
// It turns while they are open and stands still otherwise, keeping the
// angle it has: opening and closing never makes it jump.
type gear struct {
	open  bool
	angle float64 // degrees, clockwise
	last  time.Time
}

// set opens or closes the settings.
func (g *gear) set(open bool, now time.Time) {
	g.advance(now)
	g.open = open
}

// advance turns the gear by the time that has passed, if it is turning,
// and returns its angle.
func (g *gear) advance(now time.Time) float64 {
	if g.open && !g.last.IsZero() {
		g.angle += now.Sub(g.last).Seconds() * gearSpeed
		for g.angle >= 360 {
			g.angle -= 360
		}
	}
	g.last = now
	return g.angle
}
