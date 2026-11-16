package backend

import (
	"errors"
	"fmt"
	"io"

	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/input/rawinput"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/output/winmidi"
)

// Devices starts watching for input devices. On Windows these are the
// keyboards so far; game controllers are not read yet.
func Devices(problem func(what string, err error)) (*Source, error) {
	w, err := rawinput.Watch()
	if err != nil {
		return nil, err
	}
	devices := make(chan input.Device)
	go func() {
		defer close(devices)
		for d := range w.Devices() {
			devices <- d
		}
	}()
	return &Source{Devices: devices, close: w.Close}, nil
}

// Ports lists the existing MIDI ports an instrument can be connected to. It
// is not implemented for Windows yet.
func Ports() ([]output.Port, error) {
	return nil, errors.New("HIDI cannot list MIDI ports on Windows yet")
}

// OpenEndpoint creates a virtual MIDI device with the given name.
func OpenEndpoint(name string) (output.Endpoint, error) {
	return winmidi.Open(name)
}

// CheckOutput reports to w whether virtual MIDI devices can be created, and
// creates one, saying at which stage it fails if it does.
func CheckOutput(w io.Writer, name string) (output.Endpoint, error) {
	st, err := winmidi.Check()
	fmt.Fprintf(w, "Windows build %d\n", st.Build)
	fmt.Fprintf(w, "Windows MIDI Services API found in: %s\n", st.Found)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(w, "The API is part of Windows: %v\n", st.SystemProvided)
	fmt.Fprintf(w, "The MIDI service is available: %v\n", st.ServiceAvailable)
	fmt.Fprintf(w, "Creating the MIDI device %q:\n", name)
	return winmidi.OpenTraced(name, w)
}
