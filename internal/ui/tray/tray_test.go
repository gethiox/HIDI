package tray

import (
	"context"
	"errors"
	"image/color"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
)

func TestViewOf(t *testing.T) {
	two := []ipc.Instrument{{Name: "Keyboard", Channel: 1}, {Name: "Gamepad", Channel: 2}}
	var many []ipc.Instrument
	for range maxInstruments + 3 {
		many = append(many, ipc.Instrument{Name: "K", Channel: 1})
	}
	for _, tt := range []struct {
		name string
		in   *ipc.Snapshot
		want View
	}{
		{"not running", nil, View{Look: LookStopped, Title: "HIDI is not running", Lines: []string{"HIDI is not running"}}},
		{"off, nothing connected", &ipc.Snapshot{}, View{Look: LookOff, Title: "HIDI is off", Toggle: "Turn HIDI on", Lines: []string{"HIDI is off", "No instruments"}}},
		{"on, one", &ipc.Snapshot{Active: true, Instruments: two[:1]}, View{Look: LookOn, Title: "HIDI is on, 1 instrument", Toggle: "Turn HIDI off", Lines: []string{"HIDI is on, 1 instrument", "Keyboard (channel 1)"}}},
		{"on, two", &ipc.Snapshot{Active: true, Instruments: two}, View{Look: LookOn, Title: "HIDI is on, 2 instruments", Toggle: "Turn HIDI off", Lines: []string{"HIDI is on, 2 instruments", "Keyboard (channel 1)", "Gamepad (channel 2)"}}},
	} {
		got := ViewOf(tt.in)
		if got.Look != tt.want.Look || got.Title != tt.want.Title || got.Toggle != tt.want.Toggle || !slices.Equal(got.Lines, tt.want.Lines) {
			t.Errorf("%s:\n got %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
	got := ViewOf(&ipc.Snapshot{Instruments: many})
	if n := len(got.Lines); n != 1+maxInstruments+1 || got.Lines[n-1] != "and 3 more" {
		t.Errorf("many instruments: %d lines, the last %q", n, got.Lines[n-1])
	}
}

func TestIcon(t *testing.T) {
	for _, size := range []int{16, 22, 32, 64} {
		on, off, stopped := Icon(LookOn, size), Icon(LookOff, size), Icon(LookStopped, size)
		if on.Bounds().Dx() != size || on.Bounds().Dy() != size {
			t.Fatalf("size %d: the icon is %v", size, on.Bounds())
		}
		// The corners are clear, the background has the colour of the
		// look, and a key is white.
		if a := on.NRGBAAt(0, 0).A; a != 0 {
			t.Errorf("size %d: the corner has alpha %d", size, a)
		}
		edge := size / 8
		if got := on.NRGBAAt(size/2, edge); got != colourOn {
			t.Errorf("size %d: on is %v at the top", size, got)
		}
		if got := off.NRGBAAt(size/2, edge); got != colourOff {
			t.Errorf("size %d: off is %v at the top", size, got)
		}
		if got := on.NRGBAAt(size/2, size*2/3); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Errorf("size %d: the middle key is %v", size, got)
		}
		if a := stopped.NRGBAAt(size/2, edge).A; a == 0 || a >= 200 {
			t.Errorf("size %d: stopped has alpha %d, want faint", size, a)
		}
	}
}

// fakeHIDI is a running HIDI that can be stopped and started.
type fakeHIDI struct {
	mu      sync.Mutex
	running bool
	active  bool
	changed chan struct{}
	toggles int
}

func newFakeHIDI() *fakeHIDI { return &fakeHIDI{changed: make(chan struct{})} }

func (f *fakeHIDI) set(running, active bool) {
	f.mu.Lock()
	f.running, f.active = running, active
	close(f.changed)
	f.changed = make(chan struct{})
	f.mu.Unlock()
}

func (f *fakeHIDI) Watch(ctx context.Context, logs int, onSnapshot func(ipc.Snapshot), onLog func(ipc.LogLine)) error {
	for {
		f.mu.Lock()
		running, active, changed := f.running, f.active, f.changed
		f.mu.Unlock()
		if !running {
			return errors.New("HIDI stopped")
		}
		onSnapshot(ipc.Snapshot{Active: active})
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
		}
	}
}

func (f *fakeHIDI) Toggle(ctx context.Context) (ipc.Snapshot, error) {
	f.mu.Lock()
	f.toggles++
	active := !f.active
	f.mu.Unlock()
	f.set(true, active)
	return ipc.Snapshot{Active: active}, nil
}

// fakeHost records what it is shown.
type fakeHost struct {
	shown   chan View
	actions chan Action
}

func (h *fakeHost) Show(v View) error      { h.shown <- v; return nil }
func (h *fakeHost) Actions() <-chan Action { return h.actions }
func (h *fakeHost) Close() error           { return nil }

func (h *fakeHost) expect(t *testing.T, look Look) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case v := <-h.shown:
			if v.Look == look {
				return
			}
		case <-deadline:
			t.Fatalf("the icon never looked like %d", look)
		}
	}
}

func TestRun(t *testing.T) {
	hidi := newFakeHIDI()
	host := &fakeHost{shown: make(chan View, 64), actions: make(chan Action)}
	done := make(chan error, 1)
	go func() { done <- Run(t.Context(), hidi, host, slog.New(slog.DiscardHandler)) }()

	host.expect(t, LookStopped)
	host.actions <- ActionToggle // nothing to turn on: ignored
	hidi.set(true, false)        // HIDI starts; the icon finds it by itself
	host.expect(t, LookOff)
	host.actions <- ActionToggle
	host.expect(t, LookOn)
	host.actions <- ActionToggle
	host.expect(t, LookOff)
	hidi.set(false, false) // HIDI stops
	host.expect(t, LookStopped)

	hidi.mu.Lock()
	toggles := hidi.toggles
	hidi.mu.Unlock()
	if toggles != 2 {
		t.Errorf("HIDI was asked %d times, want 2", toggles)
	}
	host.actions <- ActionQuit
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the icon was removed")
	}
}
