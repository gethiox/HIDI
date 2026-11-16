//go:build linux

package backend

import (
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

// OpenEndpoint creates a virtual MIDI device with the given name.
func OpenEndpoint(name string) (output.Endpoint, error) {
	return alsaseq.Open(name)
}
