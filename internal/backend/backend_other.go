//go:build !linux && !windows

package backend

import (
	"fmt"
	"io"
	"runtime"
	"time"

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

// CheckOutput reports whether virtual MIDI devices can be created. It is not
// implemented for this platform yet.
func CheckOutput(w io.Writer, name string) (output.Endpoint, error) {
	return nil, errUnsupported
}

// OpenEndpoint creates a virtual MIDI device with the given name. It is not
// implemented for this platform yet.
func OpenEndpoint(name string) (output.Endpoint, error) {
	return nil, errUnsupported
}

// OpenDuplexEndpoint creates a virtual MIDI device that other programs can
// also send to. It is not implemented for this platform yet.
func OpenDuplexEndpoint(name string) (output.Endpoint, error) {
	return nil, errUnsupported
}

// SetRemoval chooses how MIDI devices are given up, where there is a
// choice; here there is none.
func SetRemoval(how string, wait time.Duration) error { return nil }

// OpenTraced creates a virtual MIDI device and says so on w.
func OpenTraced(w io.Writer, name string) (output.Endpoint, error) {
	fmt.Fprintf(w, "Creating the MIDI device %q\n", name)
	return nil, errUnsupported
}
