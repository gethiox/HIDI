package main

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/ui/tray"
)

// fakeTray is a tray that shows nothing and is clicked by the test.
type fakeTray struct {
	shown   chan tray.View
	actions chan tray.Action
}

func (f *fakeTray) Show(v tray.View) error {
	select {
	case f.shown <- v:
	default:
	}
	return nil
}
func (f *fakeTray) Actions() <-chan tray.Action { return f.actions }
func (f *fakeTray) Close() error                { return nil }

// fakeWindows replaces the starting of the window. Every window stays
// open until the test closes it or HIDI ends.
type fakeWindows struct {
	opened atomic.Int32
	close  chan struct{}
}

func (f *fakeWindows) install(t *testing.T) {
	old := startWindow
	startWindow = func(ctx context.Context, args []string) error {
		f.opened.Add(1)
		select {
		case <-f.close:
		case <-ctx.Done():
		}
		return nil
	}
	t.Cleanup(func() { startWindow = old })
}

func testDesktop(t *testing.T, host tray.Host, hostErr error, show bool) (d desktop, quit <-chan struct{}, done <-chan struct{}) {
	t.Helper()
	old := newTrayHost
	newTrayHost = func() (tray.Host, error) { return host, hostErr }
	t.Cleanup(func() { newTrayHost = old })

	ctx, cancel := context.WithCancel(t.Context())
	quitCh, doneCh := make(chan struct{}), make(chan struct{})
	var changes ipc.Notifier
	active := true
	d = desktop{
		windowArgs: []string{"--socket", "x"},
		show:       show,
		host: ipc.Host{
			Snapshot:  func() ipc.Snapshot { return ipc.Snapshot{Active: active} },
			SetActive: func(a bool) { active = a; changes.Notify() },
			Changes:   &changes,
		},
		log:  slog.New(slog.DiscardHandler),
		quit: func() { close(quitCh); cancel() },
	}
	go func() {
		defer close(doneCh)
		d.run(ctx)
	}()
	return d, quitCh, doneCh
}

func within(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// The icon opens the window, one at a time, and ends HIDI.
func TestDesktopWithTray(t *testing.T) {
	windows := &fakeWindows{close: make(chan struct{})}
	windows.install(t)
	host := &fakeTray{shown: make(chan tray.View, 16), actions: make(chan tray.Action)}
	_, quit, done := testDesktop(t, host, nil, false)

	v := <-host.shown
	if v.Open == "" || v.Quit != tray.QuitAllLabel {
		t.Errorf("the icon shows %+v", v)
	}
	if n := windows.opened.Load(); n != 0 {
		t.Fatalf("%d windows opened without being asked for", n)
	}
	host.actions <- tray.ActionOpen
	eventually(t, "the window", func() bool { return windows.opened.Load() == 1 })
	host.actions <- tray.ActionOpen // it is open already
	host.actions <- tray.ActionToggle
	if n := windows.opened.Load(); n != 1 {
		t.Errorf("%d windows are open, want 1", n)
	}
	windows.close <- struct{}{}
	// Closed, it can be opened again.
	eventually(t, "the window again", func() bool {
		select {
		case host.actions <- tray.ActionOpen:
		default:
		}
		return windows.opened.Load() == 2
	})
	host.actions <- tray.ActionQuit
	within(t, "HIDI to be ended", quit)
	within(t, "the desktop to finish", done)
}

// With the setting, the window opens with HIDI.
func TestDesktopShowsWindowAtStart(t *testing.T) {
	windows := &fakeWindows{close: make(chan struct{})}
	windows.install(t)
	host := &fakeTray{shown: make(chan tray.View, 16), actions: make(chan tray.Action)}
	_, _, _ = testDesktop(t, host, nil, true)
	eventually(t, "the window", func() bool { return windows.opened.Load() == 1 })
}

// Where there is no tray, the window stands in for the icon.
func TestDesktopWithoutTray(t *testing.T) {
	windows := &fakeWindows{close: make(chan struct{})}
	windows.install(t)
	_, quit, done := testDesktop(t, nil, errors.New("no tray here"), false)
	eventually(t, "the window", func() bool { return windows.opened.Load() == 1 })
	select {
	case <-quit:
		t.Fatal("HIDI ended while its window was open")
	case <-time.After(50 * time.Millisecond):
	}
	windows.close <- struct{}{}
	within(t, "HIDI to end with its window", quit)
	within(t, "the desktop to finish", done)
}

func TestLocalHIDI(t *testing.T) {
	var changes ipc.Notifier
	active := false
	l := localHIDI{ipc.Host{
		Snapshot:  func() ipc.Snapshot { return ipc.Snapshot{Active: active} },
		SetActive: func(a bool) { active = a; changes.Notify() },
		Changes:   &changes,
	}}
	if s, err := l.Toggle(t.Context()); err != nil || !s.Active {
		t.Errorf("Toggle = %+v, %v", s, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	seen := make(chan ipc.Snapshot, 4)
	done := make(chan error, 1)
	go func() { done <- l.Watch(ctx, 0, func(s ipc.Snapshot) { seen <- s }, nil) }()
	if s := <-seen; !s.Active {
		t.Error("the first state is not the present one")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Watch = %v", err)
	}
}
