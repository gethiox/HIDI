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
)

// doctorDevice is the name of the MIDI device the check creates.
const doctorDevice = "HIDI test device"

// cmdDoctor checks, one step at a time and saying what it does, that this
// system can create a MIDI device, and then plays on it so that the result
// can be seen and heard in another program. Its output is meant to be
// copied into a bug report.
func cmdDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	seconds := fs.Int("seconds", 30, "how long to keep the test device and play on it")
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	fmt.Fprintln(stdout, buildinfo.Get())
	endpoint, err := backend.CheckOutput(stdout, doctorDevice)
	if err != nil {
		fmt.Fprintf(stdout, "\nMIDI output does not work: %v\n", err)
		return 1
	}
	defer endpoint.Close()

	fmt.Fprintf(stdout, "\nThe MIDI device %q exists now. Look for it in another program:\n", doctorDevice)
	fmt.Fprintf(stdout, "a DAW, a synthesizer or a MIDI monitor. It plays a C major chord, note by\n")
	fmt.Fprintf(stdout, "note, on channel 1 for %d seconds. Ctrl+C ends it earlier.\n\n", *seconds)

	chord := []midi.Note{60, 64, 67} // C3, E3, G3
	velocity := midi.From7(100)
	deadline := time.After(time.Duration(*seconds) * time.Second)
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
	fmt.Fprintf(stdout, "Sent %d messages without an error. MIDI output works.\n", sent)
	return 0
}
