//go:build linux

package backend

import (
	"fmt"
	"io"

	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/input/evdev"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/output/alsaseq"
)

// Devices starts watching for input devices. problem, if not nil, is called
// when a device cannot be opened, most often for lack of permission.
func Devices(problem func(what string, err error)) (*Source, error) {
	w, err := evdev.Watch(evdev.WatchOptions{Problem: problem})
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

// Ports lists the existing MIDI ports an instrument can be connected to.
func Ports() ([]output.Port, error) {
	return alsaseq.Ports()
}

// CheckOutput reports to w whether virtual MIDI devices can be created, and
// creates one.
func CheckOutput(w io.Writer, name string) (output.Endpoint, error) {
	fmt.Fprintf(w, "Creating the MIDI device %q through the ALSA sequencer\n", name)
	return alsaseq.Open(name)
}

// OpenEndpoint creates a virtual MIDI device with the given name.
func OpenEndpoint(name string) (output.Endpoint, error) {
	return alsaseq.Open(name)
}

// OpenDuplexEndpoint creates a virtual MIDI device that other programs
// can also send to; it is an [output.Receiver].
func OpenDuplexEndpoint(name string) (output.Endpoint, error) {
	return alsaseq.OpenDuplex(name)
}

// SetRemoval chooses how MIDI devices are given up, where there is a
// choice; here there is none.
func SetRemoval(how string) error { return nil }

// OpenTraced creates a virtual MIDI device and says so on w.
func OpenTraced(w io.Writer, name string) (output.Endpoint, error) {
	fmt.Fprintf(w, "Creating the MIDI device %q\n", name)
	return alsaseq.Open(name)
}
