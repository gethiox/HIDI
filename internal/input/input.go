// Package input defines what the rest of HIDI needs from an input device.
// The implementations live in subpackages, one per platform, and are the
// only place that talks to the operating system's input stack.
package input

import (
	"errors"

	"github.com/gethiox/HIDI/internal/control"
)

// ErrGrabbedElsewhere is what Grab returns when another program has taken
// the device for itself. Such a program, an input remapper for one,
// usually passes the input on through a device of its own; the device it
// took then reports nothing to anyone else.
var ErrGrabbedElsewhere = errors.New("another program has the device to itself")

// Device is one connected piece of input hardware: a keyboard, a game
// controller. Hardware that the operating system presents in several pieces
// is merged into a single Device.
//
// A Device is safe for concurrent use.
type Device interface {
	// Info describes the device.
	Info() control.DeviceInfo
	// Events returns the channel the device's input arrives on. The channel
	// is closed when the device is unplugged or closed; Err then tells
	// which.
	Events() <-chan control.Event
	// Err returns why Events was closed, or nil while it is open.
	Err() error
	// Grab takes the device away from the rest of the system, so that its
	// input reaches only HIDI. Release gives it back.
	Grab() error
	Release() error
	// Close stops reading the device and releases a grab. Closing a device
	// twice is not an error.
	Close() error
}

// Passer is a [Device] that can also be shared with the rest of the
// system in part: its input goes on to the system, except for some keys
// that are kept back. HIDI uses it for a key that turns it off and on with
// a tap, which the system should not see.
//
// Share is the other way round: the device is kept from the system as by
// a grab, except for some keys that still reach it. HIDI uses it for the
// keys a profile leaves to the system while the others play.
//
// Pass, Share, Grab and Release each end what the others began.
type Passer interface {
	// Pass gives the device's input to the rest of the system except for
	// the hidden keys, which reach only HIDI.
	Pass(hidden []control.Code) error
	// Share takes the device away from the rest of the system like Grab,
	// except for the open keys, which the system gets as well as HIDI.
	Share(open []control.Code) error
	// Type presses or releases a key for the rest of the system, as if on
	// this device.
	Type(key control.Code, down bool) error
}
