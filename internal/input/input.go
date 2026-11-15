// Package input defines what the rest of HIDI needs from an input device.
// The implementations live in subpackages, one per platform, and are the
// only place that talks to the operating system's input stack.
package input

import "github.com/gethiox/HIDI/internal/control"

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
