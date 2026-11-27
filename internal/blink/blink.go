// Package blink is the blinking of learning mode: HIDI's icon in the
// tray and in the window, and the key of learning mode on a keyboard's
// lights, change between the two colours of learning mode together.
//
// Each of them asks the clock, not one another: the half the blinking is
// in follows from the time alone, the same for every part of HIDI and for
// the window, which may be a program of its own. Nothing has to be
// started, stopped or told when learning mode, the lights or the window
// come and go.
package blink

import "time"

// Phase is how long each half of the blinking lasts.
var Phase = 500 * time.Millisecond

// Lit reports whether the blinking is in the half that shows the colour
// of learning at t, as opposed to the colour of HIDI being on.
func Lit(t time.Time) bool {
	return t.UnixNano()/int64(Phase)%2 == 1
}

// Next returns when the half after the one t is in begins.
func Next(t time.Time) time.Time {
	p := int64(Phase)
	return time.Unix(0, (t.UnixNano()/p+1)*p)
}
