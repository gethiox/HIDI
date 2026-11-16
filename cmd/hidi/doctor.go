package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/gethiox/HIDI/internal/backend"
	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

// doctorDevice is the name of the MIDI device the check creates.
const doctorDevice = "HIDI test device"

// cmdDoctor checks, one step at a time and saying what it does, that this
// system can create a MIDI device, and then plays on it so that the result
// can be seen and heard in another program. Its output is meant to be
// copied into a bug report.
func cmdDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	seconds := fs.Int("seconds", 0, "how long to keep the test device and play on it; 0 means until Ctrl+C")
	devices := fs.Int("devices", 3, "how many MIDI devices to create in all, as HIDI makes one for each instrument")
	removal := fs.String("removal", "session", "on Windows, how devices are given up: session, disconnect, release,\nor none, which leaves them to the end of the program")
	wait := fs.Duration("wait", -1, "on Windows, how long a device is left alone before it is removed,\nsuch as 0 or 2s; the usual time when not given")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	if err := backend.SetRemoval(*removal, *wait); err != nil {
		fmt.Fprintf(stderr, "hidi doctor: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, buildinfo.Get())
	backend.CheckInput(stdout)
	endpoint, err := backend.CheckOutput(stdout, doctorDevice)
	if err != nil {
		fmt.Fprintf(stdout, "\nMIDI output does not work: %v\n", err)
		return 1
	}
	// HIDI makes a device for every instrument, so more than one has to
	// work, and so has making one after others were removed.
	var more []output.Endpoint
	defer func() {
		for _, e := range more {
			e.Close()
		}
	}()
	for n := 2; n <= *devices; n++ {
		e, err := backend.OpenTraced(stdout, fmt.Sprintf("%s %d", doctorDevice, n))
		if err != nil {
			fmt.Fprintf(stdout, "\nA further MIDI device cannot be created: %v\n", err)
			return 1
		}
		more = append(more, e)
	}
	closed := false
	defer func() {
		if !closed {
			endpoint.Close()
		}
	}()

	fmt.Fprintf(stdout, "\nThe MIDI device %q exists now. Look for it in another program:\n", doctorDevice)
	fmt.Fprintf(stdout, "a DAW, a synthesizer or a MIDI monitor. It plays a C major chord, note by\n")
	if *seconds > 0 {
		fmt.Fprintf(stdout, "note, on channel 1 for %d seconds. Ctrl+C ends it earlier.\n\n", *seconds)
	} else {
		fmt.Fprintf(stdout, "note, on channel 1 until you press Ctrl+C.\n\n")
	}

	chord := []midi.Note{60, 64, 67} // C3, E3, G3
	velocity := midi.From7(100)
	var deadline <-chan time.Time // never, unless a duration was asked for
	if *seconds > 0 {
		deadline = time.After(time.Duration(*seconds) * time.Second)
	}
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	sent := 0
	for i := 0; ; i++ {
		note := chord[i%len(chord)]
		if err := endpoint.Send([]midi.Message{midi.NoteOn(0, note, velocity)}); err != nil {
			fmt.Fprintf(stdout, "Sending failed after %d messages: %v\n", sent, err)
			return 1
		}
		sent++
		stop := false
		select {
		case <-ctx.Done():
			stop = true
		case <-deadline:
			stop = true
		case <-tick.C:
		}
		if err := endpoint.Send([]midi.Message{midi.NoteOff(0, note, 0)}); err != nil {
			fmt.Fprintf(stdout, "Sending failed after %d messages: %v\n", sent, err)
			return 1
		}
		sent++
		if stop {
			break
		}
	}
	fmt.Fprintf(stdout, "Sent %d messages without an error.\nRemoving the MIDI device:\n", sent)
	closed = true
	removed := endpoint.Close()
	for _, e := range more {
		if err := e.Close(); err != nil && removed == nil {
			removed = err
		}
	}
	more = nil
	if removed != nil {
		fmt.Fprintf(stdout, "\nMIDI output works, but %v\n", removed)
		return 1
	}
	// A device made after the others were removed shows that removing
	// them left the MIDI service in good order.
	fmt.Fprintln(stdout, "\nAfter the removal:")
	again, err := backend.OpenTraced(stdout, doctorDevice+" again")
	if err != nil {
		fmt.Fprintf(stdout, "\nMIDI output works, but after devices were removed no new one can be created: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Removing it:")
	if err := again.Close(); err != nil {
		fmt.Fprintf(stdout, "\nMIDI output works, but %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "\nMIDI output works, with several devices, and removing them leaves it working.")
	return 0
}
