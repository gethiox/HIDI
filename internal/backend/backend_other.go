//go:build !linux

package backend

import (
	"fmt"
	"runtime"

	"github.com/gethiox/HIDI/internal/output"
)

var errUnsupported = fmt.Errorf("HIDI does not support %s yet", runtime.GOOS)

// Devices starts watching for input devices. It is not implemented for this
// platform yet.
func Devices(problem func(what string, err error)) (*Source, error) {
	return nil, errUnsupported
}

// Ports lists the existing MIDI ports an instrument can be connected to. It
// is not implemented for this platform yet.
func Ports() ([]output.Port, error) {
	return nil, errUnsupported
}

// OpenEndpoint creates a virtual MIDI device with the given name. It is not
// implemented for this platform yet.
func OpenEndpoint(name string) (output.Endpoint, error) {
	return nil, errUnsupported
}
