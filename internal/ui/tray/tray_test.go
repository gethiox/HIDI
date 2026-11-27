package tray

import (
	"context"
	"errors"
	"github.com/gethiox/HIDI/internal/assets/icon"
	"github.com/gethiox/HIDI/internal/blink"
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
		// The outer keys are of the letter: green while on, and of the
		// colour of every other key while off. A stopped HIDI is faint.
		x, y := size*15/100, size*6/10
		if got := on.NRGBAAt(x, y); got != icon.On[icon.H] {
			t.Errorf("size %d: on, the left key is %v", size, got)
		}
		if got := off.NRGBAAt(x, y); got != icon.Off[icon.H] {
			t.Errorf("size %d: off, the left key is %v", size, got)
		}
		if got := Icon(LookLearning, size).NRGBAAt(x, y); got != icon.Learning[icon.H] {
			t.Errorf("size %d: learning, the left key is %v", size, got)
		}
		if a := stopped.NRGBAAt(x, y).A; a == 0 || a >= 200 {
			t.Errorf("size %d: stopped has alpha %d, want faint", size, a)
		}
	}
}

// fakeHIDI is a running HIDI that can be stopped and started.
type fakeHIDI struct {
	mu       sync.Mutex
	running  bool
	active   bool
	changed  chan struct{}
	toggles  int
	learning bool
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
		running, active, changed, learning := f.running, f.active, f.changed, f.learning
		f.mu.Unlock()
		if !running {
			return errors.New("HIDI stopped")
		}
		onSnapshot(ipc.Snapshot{Active: active, Learning: learning})
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

func (f *fakeHIDI) ToggleLearning(ctx context.Context) (ipc.Snapshot, error) {
	f.mu.Lock()
	f.learning = !f.learning
	learning, running, active := f.learning, f.running, f.active
	f.mu.Unlock()
	f.set(running, active)
	return ipc.Snapshot{Active: active, Learning: learning}, nil
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
	go func() { done <- Run(t.Context(), hidi, host, slog.New(slog.DiscardHandler), Options{}) }()

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

// The icon of the application offers the window, and ends HIDI.
func TestRunWithWindow(t *testing.T) {
	hidi := newFakeHIDI()
	host := &fakeHost{shown: make(chan View, 64), actions: make(chan Action)}
	opened := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), hidi, host, slog.New(slog.DiscardHandler), Options{
			Open:      func() { opened <- struct{}{} },
			QuitLabel: QuitAllLabel,
		})
	}()
	v := <-host.shown
	if v.Open != OpenLabel || v.quit() != QuitAllLabel || v.click() != ActionOpen {
		t.Errorf("the view is %+v", v)
	}
	host.actions <- ActionOpen
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the window was not opened")
	}
	host.actions <- ActionQuit
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if plain := ViewOf(nil); plain.click() != ActionToggle || plain.quit() != QuitLabel {
		t.Errorf("an icon without a window clicks %v and quits with %q", plain.click(), plain.quit())
	}
}

// In learning mode the icon of a HIDI that is on blinks between two
// colours, and the menu turns the mode on and off.
func TestRunLearning(t *testing.T) {
	old := blink.Phase
	blink.Phase = 5 * time.Millisecond
	t.Cleanup(func() { blink.Phase = old })

	hidi := newFakeHIDI()
	host := &fakeHost{shown: make(chan View, 256), actions: make(chan Action)}
	done := make(chan error, 1)
	go func() { done <- Run(t.Context(), hidi, host, slog.New(slog.DiscardHandler), Options{}) }()
	// Drain what is shown beside the looking, so that Run never waits.
	last := make(chan View, 1)
	go func() {
		for v := range host.shown {
			select {
			case <-last:
			default:
			}
			last <- v
		}
	}()
	see := func(what string, ok func(View) bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case v := <-last:
				if ok(v) {
					return
				}
			case <-deadline:
				t.Fatalf("the icon never showed %s", what)
			}
		}
	}

	hidi.set(true, true)
	see("HIDI on with a way into learning mode", func(v View) bool {
		return v.Look == LookOn && v.Learn == "Turn learning mode on" && !v.Learning
	})
	host.actions <- ActionLearn
	// Both colours, one after the other, and more than once.
	for range 3 {
		see("the colour of learning", func(v View) bool { return v.Look == LookLearning && v.Learn == "Turn learning mode off" })
		see("the colour of on again", func(v View) bool { return v.Look == LookOn && v.Learning })
	}
	// Off, the icon is steady: grey, whatever the mode.
	hidi.set(true, false)
	see("off", func(v View) bool { return v.Look == LookOff })
	time.Sleep(30 * time.Millisecond)
	select {
	case v := <-last:
		if v.Look != LookOff {
			t.Errorf("the icon of a HIDI that is off blinks: %d", v.Look)
		}
	default:
	}

	hidi.set(true, true)
	host.actions <- ActionLearn
	see("on and steady after learning ended", func(v View) bool { return v.Look == LookOn && !v.Learning })
	time.Sleep(30 * time.Millisecond)
	select {
	case v := <-last:
		if v.Look != LookOn {
			t.Errorf("the icon still blinks after learning ended: %d", v.Look)
		}
	default:
	}
	host.actions <- ActionQuit
	<-done
	close(host.shown)
}
