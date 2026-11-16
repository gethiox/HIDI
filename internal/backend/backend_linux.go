//go:build linux

package backend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

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

// CheckInput reports to w whether the input devices can be read, and what
// to do if they cannot. It returns false if none can.
func CheckInput(w io.Writer) bool {
	nodes, _ := filepath.Glob("/dev/input/event*")
	if len(nodes) == 0 {
		fmt.Fprintln(w, "Input devices: none found in /dev/input")
		return false
	}
	readable := 0
	var why error
	for _, node := range nodes {
		f, err := os.OpenFile(node, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			why = err
			continue
		}
		f.Close()
		readable++
	}
	fmt.Fprintf(w, "Input devices: %d of %d can be read\n", readable, len(nodes))
	if readable == len(nodes) {
		return true
	}
	fmt.Fprintf(w, "  the others fail with: %v\n", why)
	fmt.Fprintln(w, "  To let the person logged in at this machine read them, install the udev")
	fmt.Fprintln(w, "  rule 70-hidi.rules that comes with HIDI; the file says how. Or add your")
	fmt.Fprintln(w, "  user to the input group: sudo usermod -aG input \"$USER\", then log in again.")
	return readable > 0
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
func SetRemoval(how string, wait time.Duration) error { return nil }

// OpenTraced creates a virtual MIDI device and says so on w.
func OpenTraced(w io.Writer, name string) (output.Endpoint, error) {
	fmt.Fprintf(w, "Creating the MIDI device %q\n", name)
	return alsaseq.Open(name)
}
