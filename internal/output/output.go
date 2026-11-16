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

// Receiver is an endpoint that other programs can also send to.
type Receiver interface {
	// Receive waits for MIDI sent to the endpoint and appends the
	// messages to out. It returns an error once the endpoint is closed.
	// It must be called from one goroutine at a time, and be kept
	// called: what is not read piles up.
	Receive(out []midi.Message) ([]midi.Message, error)
}

// Port is a MIDI destination that already exists on the system: the input
// of a hardware MIDI interface, a synthesizer, or another program.
type Port struct {
	// ID identifies the port for [Connector.Connect]. It is only valid for
	// as long as the port exists; a device that is plugged in again may get
	// another one.
	ID string
	// Name is what the port is called, including the device or program it
	// belongs to.
	Name string
}

// Connector is an endpoint that can also deliver what it sends to existing
// ports.
type Connector interface {
	Endpoint
	// Connect makes the endpoint deliver to the port as well. It reports
	// whether the two were connected already, which is not an error.
	Connect(portID string) (already bool, err error)
}
