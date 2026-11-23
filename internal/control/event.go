package control

import "time"

// Event reports a change of one control.
type Event struct {
	// Code identifies the control.
	Code Code
	// Value is the new state of the control, from 0 to 1. See the package
	// documentation for how each kind of control uses the range.
	Value float64
	// Part names the part of the device the control belongs to, for devices
	// that expose several sets of controls with overlapping codes, such as a
	// game controller with a touchpad. It is empty for the main part.
	Part string
	// Time is when the input backend received the event.
	Time time.Time
}

// Pressed reports whether a key or button event is a press rather than a
// release. Controls that report intermediate values count as pressed past
// the halfway point.
func (e Event) Pressed() bool { return e.Value >= 0.5 }

// Input names one control of a device: its code, and the part of the
// device it is on, as [Event.Part] has it.
type Input struct {
	Code Code
	Part string
}
