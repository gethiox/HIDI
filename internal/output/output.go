// Package output defines what the rest of HIDI needs from a MIDI output. The
// implementations live in subpackages, one per transport, and are the only
// place that talks to the operating system's MIDI stack.
package output

import "github.com/gethiox/HIDI/internal/midi"

// Endpoint is one MIDI destination that an instrument writes to. For a
// virtual endpoint, opening it makes a MIDI device appear to other programs
// and closing it makes the device disappear.
//
// An Endpoint is safe for concurrent use.
type Endpoint interface {
	// Send delivers the messages in order. It returns an error if the
	// endpoint is closed or the transport fails; in that case some of the
	// messages may have been delivered.
	Send(msgs []midi.Message) error
	// Close releases the endpoint. Closing an endpoint twice is not an
	// error.
	Close() error
}
