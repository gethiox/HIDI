package backend

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/input/rawinput"
	"github.com/gethiox/HIDI/internal/input/xinput"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/output/winmidi"
)

// Devices starts watching for input devices: keyboards and game
// controllers. Xbox controllers, and those that imitate one, are read
// through XInput; keyboards and every other controller through Raw Input.
// Should XInput not be there, Raw Input reads all of them.
func Devices(problem func(what string, err error)) (*Source, error) {
	pads, err := xinput.Watch()
	if err != nil && problem != nil {
		problem("Xbox controllers are read the plain way, on which both triggers share one axis", err)
	}
	if err != nil {
		pads = nil
	}
	w, err := rawinput.Watch(rawinput.Options{SkipXInput: pads != nil})
	if err != nil {
		if pads != nil {
			pads.Close()
		}
		return nil, err
	}
	devices := make(chan input.Device)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for d := range w.Devices() {
			devices <- d
		}
	}()
	if pads != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range pads.Devices() {
				devices <- d
			}
		}()
	}
	go func() {
		wg.Wait()
		close(devices)
	}()
	return &Source{Devices: devices, close: func() error {
		if pads != nil {
			pads.Close()
		}
		return w.Close()
	}}, nil
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

// OpenDuplexEndpoint creates a virtual MIDI device. Receiving what other
// programs send to it is not implemented for Windows yet, so the device
// only sends.
func OpenDuplexEndpoint(name string) (output.Endpoint, error) {
	return winmidi.Open(name)
}

// SetRemoval chooses how MIDI devices are given up: "session",
// "disconnect" or "release". It is for the diagnostic command.
func SetRemoval(how string) error {
	switch r := winmidi.Removal(how); r {
	case winmidi.RemoveBySession, winmidi.RemoveByDisconnect, winmidi.RemoveByRelease:
		winmidi.HowToRemove = r
		return nil
	}
	return fmt.Errorf("unknown way to remove a device %q; it is session, disconnect or release", how)
}

// OpenTraced creates a virtual MIDI device and reports every stage to w.
func OpenTraced(w io.Writer, name string) (output.Endpoint, error) {
	fmt.Fprintf(w, "Creating the MIDI device %q:\n", name)
	return winmidi.OpenTraced(name, w)
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
