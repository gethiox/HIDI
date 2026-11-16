//go:build linux

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/input/evdev/uinput"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output/alsaseq"
)

// Key codes from linux/input-event-codes.h.
const (
	keyLeftCtrl  = 29
	keyLeftShift = 42
	keyZ         = 44 // C0 in the built-in keyboard profile
	keyLeftAlt   = 56
)

const (
	deviceName = "E2E Keyboard"
	portName   = "HIDI " + deviceName
)

// syncBuffer collects the output of the running program.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// eventually waits for something another process brings about.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func findSource(t *testing.T, name string) (id string, found bool) {
	t.Helper()
	sources, err := alsaseq.Sources()
	if err != nil {
		t.Fatalf("listing MIDI sources: %v", err)
	}
	for _, s := range sources {
		if s.Name == name {
			return s.ID, true
		}
	}
	return "", false
}

// TestKeyboardToMIDI runs HIDI as a user would and plays a note on it.
//
// It needs the program built beforehand, named by HIDI_E2E_BINARY, and a
// kernel with uinput and the ALSA sequencer; CI provides all three in its
// virtual machine. Without the variable the test is skipped.
func TestKeyboardToMIDI(t *testing.T) {
	binary := os.Getenv("HIDI_E2E_BINARY")
	if binary == "" {
		t.Skip("HIDI_E2E_BINARY is not set")
	}

	// A keyboard with the usual keys, so that HIDI takes it for one.
	keys := make([]uint16, 120)
	for i := range keys {
		keys[i] = uint16(i) + 1
	}
	kbd, err := uinput.Create(uinput.Spec{
		Name: deviceName, Phys: "hidi-e2e/input0",
		Vendor: 0x1234, Product: 0x0e2e, Version: 1,
		Keys: keys,
	})
	if err != nil {
		t.Fatalf("creating the keyboard: %v", err)
	}
	defer kbd.Close()

	socket := filepath.Join(t.TempDir(), "control.sock")
	var log syncBuffer
	hidi := exec.Command(binary, "run", "-v", "-no-color", "-profiles", "", "-socket", socket)
	hidi.Stdout, hidi.Stderr = &log, &log
	if err := hidi.Start(); err != nil {
		t.Fatalf("starting %s: %v", binary, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- hidi.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			hidi.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("the log of hidi run:\n%s", log.String())
		}
	}()
	// control runs one of the commands that talk to the running HIDI.
	control := func(command string) string {
		t.Helper()
		out, err := exec.Command(binary, command, "-socket", socket).CombinedOutput()
		if err != nil {
			t.Fatalf("hidi %s: %v\n%s", command, err, out)
		}
		return string(out)
	}

	// The instrument appears as a MIDI device named after the keyboard.
	var source string
	eventually(t, "the MIDI device "+portName, func() bool {
		var found bool
		source, found = findSource(t, portName)
		return found
	})

	in, err := alsaseq.OpenInput("HIDI e2e receiver")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Connect(source); err != nil {
		t.Fatal(err)
	}
	received := make(chan midi.Message, 64)
	go func() {
		for {
			msgs, err := in.Read(nil)
			if err != nil {
				return
			}
			for _, m := range msgs {
				received <- m
			}
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case m := <-received:
			if m.String() != want {
				t.Fatalf("received %q, want %q", m.String(), want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("nothing received, want %q", want)
		}
	}
	press := func(code uint16, down bool) {
		t.Helper()
		if err := kbd.Key(code, down); err != nil {
			t.Fatal(err)
		}
	}

	// A key becomes a note.
	press(keyZ, true)
	expect("NoteOn ch=1 note=C0 velocity=64")
	press(keyZ, false)
	expect("NoteOff ch=1 note=C0 velocity=0")

	if status := control("status"); !strings.Contains(status, "is on") || !strings.Contains(status, deviceName) {
		t.Errorf("hidi status:\n%s", status)
	}

	// Turning HIDI off from outside releases the held note.
	press(keyZ, true)
	expect("NoteOn ch=1 note=C0 velocity=64")
	if out := control("off"); !strings.Contains(out, "is off") {
		t.Errorf("hidi off:\n%s", out)
	}
	expect("NoteOff ch=1 note=C0 velocity=0")
	press(keyZ, false)

	// The toggle combination on the keyboard turns it on again.
	for _, key := range []uint16{keyLeftCtrl, keyLeftShift, keyLeftAlt} {
		press(key, true)
	}
	for _, key := range []uint16{keyLeftAlt, keyLeftShift, keyLeftCtrl} {
		press(key, false)
	}
	eventually(t, "HIDI to be on again", func() bool { return strings.Contains(control("status"), "is on") })
	press(keyZ, true)
	expect("NoteOn ch=1 note=C0 velocity=64")

	// Interrupting HIDI releases the note, removes the MIDI device and
	// ends the program without an error.
	if err := hidi.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	expect("NoteOff ch=1 note=C0 velocity=0")
	select {
	case err := <-exited:
		stopped = true
		if err != nil {
			t.Errorf("hidi run ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hidi run did not end after the interrupt")
	}
	if _, found := findSource(t, portName); found {
		t.Error("the MIDI device is still there after HIDI ended")
	}
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the control socket is still there after HIDI ended: %v", err)
	}
	if !strings.Contains(log.String(), "instrument started") {
		t.Errorf("the log does not mention the instrument:\n%s", log.String())
	}
}
