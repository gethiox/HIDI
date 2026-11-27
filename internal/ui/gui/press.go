package gui

import "time"

// A button held down with the mouse shows that it is pressed, and some
// buttons, those that step a number, go on clicking while they are held:
// after repeatDelay, every repeatEvery. While a button repeats, it shows
// as pressed for the first half of every repeatEvery and not for the
// second, so that every click it makes can be seen.
const (
	repeatDelay = 500 * time.Millisecond
	repeatEvery = 100 * time.Millisecond
)

// press is a button the left mouse button went down on and is held on:
// since is when it went down, and fired when it last clicked.
type press struct {
	since, fired time.Time
}

// step says what the held button does at now: whether it clicks again,
// and whether it shows as pressed. A button that does not repeat only
// shows as pressed.
func (p *press) step(now time.Time, repeat bool) (fire, lit bool) {
	if !repeat || now.Sub(p.since) < repeatDelay {
		return false, true
	}
	next := p.fired.Add(repeatEvery)
	if p.fired.Equal(p.since) {
		next = p.since.Add(repeatDelay)
	}
	if !now.Before(next) {
		fire, p.fired = true, next
		if now.Sub(next) >= repeatEvery {
			p.fired = now // far behind, after a stall: one click, not a burst
		}
	}
	return fire, now.Sub(p.fired) < repeatEvery/2
}
