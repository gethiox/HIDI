package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/ui/tray"
)

// HIDI started without a command is an application: the instruments run,
// an icon sits in the tray, and the window is opened from the icon, or at
// once where the settings say so. The window is a process of its own, the
// same "hidi gui" anyone can start: it holds what a graphics driver may do
// away from the instruments, and gives all its memory back when closed.

// windowProcess is the window of the application.
type windowProcess struct {
	ctx  context.Context
	args []string // of "hidi gui"
	log  *slog.Logger

	mu      sync.Mutex
	running bool
	done    chan struct{} // closed when the window that runs has closed
	failed  bool          // the last window ended with an error
}

// broke reports whether the last window ended with an error, as it does
// when there is no display to open it on.
func (w *windowProcess) broke() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

// start is what runs the window; tests replace it.
var startWindow = func(ctx context.Context, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, append([]string{"gui"}, args...)...)
	prepareWindow(cmd)
	// Why a window could not be opened is said by the window.
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

// open opens the window unless it is open, and returns a channel that is
// closed when it has closed.
func (w *windowProcess) open() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return w.done
	}
	w.running, w.done = true, make(chan struct{})
	done := w.done
	go func() {
		err := startWindow(w.ctx, w.args)
		if err != nil && w.ctx.Err() == nil {
			w.log.Error("the window could not be opened, or did not close properly", "error", err)
		}
		w.mu.Lock()
		w.running, w.failed = false, err != nil && w.ctx.Err() == nil
		w.mu.Unlock()
		close(done)
	}()
	return done
}

// wait returns once no window is open.
func (w *windowProcess) wait() {
	w.mu.Lock()
	running, done := w.running, w.done
	w.mu.Unlock()
	if running {
		<-done
	}
}

// localHIDI is the HIDI of this very process, as the icon needs it.
type localHIDI struct {
	host ipc.Host
}

func (l localHIDI) Watch(ctx context.Context, logs int, onSnapshot func(ipc.Snapshot), onLog func(ipc.LogLine)) error {
	changes, stop := l.host.Changes.Listen()
	defer stop()
	for {
		onSnapshot(l.host.Snapshot())
		select {
		case <-ctx.Done():
			return nil
		case <-changes:
		}
		// An icon need not follow every note.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (l localHIDI) Toggle(ctx context.Context) (ipc.Snapshot, error) {
	l.host.SetActive(!l.host.Snapshot().Active)
	return l.host.Snapshot(), nil
}

// newTrayHost shows the icon; tests replace it.
var newTrayHost = tray.NewHost

// trayWait is how long a desktop is given to show the icon before HIDI
// takes it for a desktop without icons.
var trayWait = 2 * time.Second

// desktop is what the application has beside the instruments.
type desktop struct {
	// windowArgs are the options of the window, or nil if there cannot
	// be one: it needs the control socket.
	windowArgs []string
	// show opens the window at once.
	show bool
	host ipc.Host
	log  *slog.Logger
	// quit ends HIDI.
	quit func()
	// failed, if set, is told when HIDI ends because it had nothing to
	// show itself with: no icon, and a window that could not be opened.
	failed func()
}

// fail ends HIDI after the window that stood in for the icon has closed.
func (d desktop) fail(w *windowProcess) {
	if w.broke() && d.failed != nil {
		d.failed()
	}
	d.quit()
}

// run shows the icon and opens the window when asked to, until ctx ends.
// Choosing to quit from the icon ends HIDI. Where the desktop has no place
// for an icon, the window is opened instead, and closing it ends HIDI:
// nothing else would show that HIDI runs, or offer a way to end it.
func (d desktop) run(ctx context.Context) {
	window := &windowProcess{ctx: ctx, args: d.windowArgs, log: d.log}
	host, err := newTrayHost()
	if err == nil {
		defer host.Close()
		// A desktop that is still starting gets a moment to show icons.
		for wait := trayWait; !tray.Shown(host) && wait > 0 && ctx.Err() == nil; wait -= trayWait / 20 {
			time.Sleep(trayWait / 20)
		}
		if !tray.Shown(host) {
			err = errors.New("this desktop shows no icons of programs; on GNOME, the AppIndicator extension adds that")
		}
	}
	if err != nil {
		if d.windowArgs == nil {
			d.log.Warn("no icon in the tray, and no window without the control socket; HIDI runs as with --headless", "error", err)
			<-ctx.Done()
			return
		}
		d.log.Warn("no icon in the tray; the window is opened instead, and closing it ends HIDI", "error", err)
		closed := window.open()
		if host == nil {
			<-closed
			d.fail(window)
			return
		}
		// The icon may still come, with a panel that starts late. If it
		// has not by the time the window is closed, HIDI ends.
		go func() {
			<-closed
			if !tray.Shown(host) {
				d.fail(window)
			}
		}()
	}
	opts := tray.Options{QuitLabel: tray.QuitAllLabel}
	if d.windowArgs != nil {
		opts.Open = func() { window.open() }
		if d.show {
			window.open()
		}
	}
	if err := tray.Run(ctx, localHIDI{d.host}, host, d.log, opts); err != nil {
		d.log.Error("the icon in the tray failed", "error", err)
	}
	d.quit()
	window.wait()
}
