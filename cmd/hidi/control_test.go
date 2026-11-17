package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
)

var twoInstruments = ipc.Snapshot{
	Version: "hidi test",
	Active:  true,
	Instruments: []ipc.Instrument{
		{Name: "Keychron K2", Type: "keyboard", Profile: "Default keyboard", Layout: "Piano", Octave: 1, Transpose: -2, Channel: 3, Velocity: 100, Sounding: 2, InputRate: 12, MIDIRate: 340},
		{Name: "Pad", Type: "gamepad", Profile: "Default gamepad", Layout: "Default", Channel: 1, Velocity: 64, Sounding: 1},
	},
}

func TestDescribe(t *testing.T) {
	var b bytes.Buffer
	describe(&b, twoInstruments)
	want := `hidi test is on

Keychron K2  (keyboard, profile "Default keyboard")
  octave  +1  transpose   -2  channel  3  velocity 100  notes  2  layout Piano
  input   12/s  MIDI  340/s

Pad  (gamepad, profile "Default gamepad")
  octave  +0  transpose   +0  channel  1  velocity  64  notes  1  layout Default
  input    0/s  MIDI    0/s
`
	if b.String() != want {
		t.Errorf("describe wrote:\n%s\nwant:\n%s", b.String(), want)
	}

	b.Reset()
	describe(&b, ipc.Snapshot{Version: "hidi test"})
	if got := b.String(); !strings.HasPrefix(got, "hidi test is off\n") || !strings.Contains(got, "No instruments") {
		t.Errorf("describe of an idle HIDI wrote:\n%s", got)
	}
}

// serveFake runs a control server over a state the test can change.
func serveFake(t *testing.T) (socket string, state *ipc.Snapshot, mu *sync.Mutex, changes *ipc.Notifier) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "control.sock")
	server, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	current := twoInstruments
	state, mu, changes = &current, &sync.Mutex{}, &ipc.Notifier{}
	host := ipc.Host{
		Snapshot: func() ipc.Snapshot {
			mu.Lock()
			defer mu.Unlock()
			return *state
		},
		SetActive: func(active bool) {
			mu.Lock()
			state.Active = active
			mu.Unlock()
			changes.Notify()
		},
		Changes: changes,
		LogLines: func(from, to int) []ipc.LogLine {
			if from <= 0 && to >= 1 {
				return []ipc.LogLine{{Text: "\x1b[32m INFO\x1b[0m instrument started"}}
			}
			return nil
		},
		LogTotal: func() int { return 1 },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.Serve(ctx, host)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return socket, state, mu, changes
}

func TestControlCommands(t *testing.T) {
	socket, state, mu, _ := serveFake(t)
	hidi := func(command string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), []string{command, "--socket", socket}, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	code, stdout, stderr := hidi("status")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "is on") || !strings.Contains(stdout, "Keychron K2") {
		t.Errorf("status: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, stdout, _ = hidi("off"); code != 0 || !strings.Contains(stdout, "is off") {
		t.Errorf("off: code %d, stdout %q", code, stdout)
	}
	mu.Lock()
	active := state.Active
	mu.Unlock()
	if active {
		t.Error("\"hidi off\" did not turn HIDI off")
	}
	if code, stdout, _ = hidi("toggle"); code != 0 || !strings.Contains(stdout, "is on") {
		t.Errorf("toggle: code %d, stdout %q", code, stdout)
	}
	if code, stdout, _ = hidi("on"); code != 0 || !strings.Contains(stdout, "is on") {
		t.Errorf("on: code %d, stdout %q", code, stdout)
	}
}

func TestControlCommandsWithoutHIDI(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "control.sock")
	for _, command := range []string{"status", "on", "off", "toggle", "monitor"} {
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), []string{command, "--socket", socket}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "HIDI is not running") || stdout.Len() != 0 {
			t.Errorf("%s: code %d, stdout %q, stderr %q", command, code, stdout.String(), stderr.String())
		}
	}
}

// lockedBuffer is a buffer the monitor writes while the test reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Away from a terminal the monitor writes every change below the last,
// without any escape sequence.
func TestMonitorPlain(t *testing.T) {
	socket, _, _, _ := serveFake(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr lockedBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"monitor", "--socket", socket}, &stdout, &stderr) }()

	waitFor := func(what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(stdout.String(), what) {
			if time.Now().After(deadline) {
				t.Fatalf("the monitor never showed %q; it wrote:\n%s", what, stdout.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("hidi test is on")
	waitFor(" INFO instrument started")

	var out, errOut bytes.Buffer
	if code := run(t.Context(), []string{"off", "--socket", socket}, &out, &errOut); code != 0 {
		t.Fatalf("hidi off: %s", errOut.String())
	}
	waitFor("hidi test is off")

	cancel()
	select {
	case code := <-done:
		if code != 0 || stderr.String() != "" {
			t.Errorf("monitor ended with code %d, stderr %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the monitor did not end when interrupted")
	}
	if strings.Contains(stdout.String(), "\x1b") {
		t.Errorf("escape sequences in the plain view:\n%q", stdout.String())
	}
}

func TestMonitorRedraw(t *testing.T) {
	var out bytes.Buffer
	v := &monitorView{out: &out, redraw: true}
	for i := range monitorLogLines + 3 {
		v.log(ipc.LogLine{Text: "line " + string(rune('a'+i))})
	}
	if out.Len() != 0 {
		t.Errorf("the view was drawn before there was a state: %q", out.String())
	}
	v.snapshot(twoInstruments)
	screen := out.String()
	if !strings.HasPrefix(screen, hideCursor+clearScreen+"hidi test is on") {
		t.Errorf("the screen starts %q", screen[:min(len(screen), 60)])
	}
	// Only the end of the log is kept.
	if strings.Contains(screen, "line a\x1b") || !strings.Contains(screen, "line d\x1b") || !strings.Contains(screen, "line o\x1b") {
		t.Errorf("the log part of the screen:\n%q", screen)
	}

	out.Reset()
	v.log(ipc.LogLine{Text: "newest"})
	if got := out.String(); strings.Count(got, clearScreen) != 1 || !strings.Contains(got, "newest") {
		t.Errorf("a new log line did not redraw the screen once: %q", got)
	}
}
