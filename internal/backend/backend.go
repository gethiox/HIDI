// Package backend selects the input and output implementations for the
// platform HIDI is built for. It is the one place outside the input, output
// and ui trees that is built differently per platform, so that everything
// above it, the runtime included, is the same code everywhere.
package backend

import "github.com/gethiox/HIDI/internal/input"

// Source delivers the input devices of the system: those present when it
// was opened and those connected afterwards.
type Source struct {
	// Devices is the channel new devices arrive on. Whoever receives a
	// device owns it and must close it. The channel is closed when the
	// source is closed.
	Devices <-chan input.Device

	close func() error
}

// Close stops delivering devices. Devices already delivered are not
// affected.
func (s *Source) Close() error { return s.close() }
