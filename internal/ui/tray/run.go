package tray

import (
	"context"
	"github.com/gethiox/HIDI/internal/blink"
	"log/slog"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
)

// Controller is the running HIDI as the icon needs it; *ipc.Client is one.
type Controller interface {
	Watch(ctx context.Context, logs int, onSnapshot func(ipc.Snapshot), onLog func(ipc.LogLine)) error
	Toggle(ctx context.Context) (ipc.Snapshot, error)
	ToggleLearning(ctx context.Context) (ipc.Snapshot, error)
}

const (
	// retryInterval is how long the icon waits before it looks for HIDI
	// again.
	retryInterval = time.Second
	// askTimeout limits a request to HIDI.
	askTimeout = 2 * time.Second
)

// Options change what the icon offers.
type Options struct {
	// Open, if set, opens HIDI's window. The menu then has an entry for
	// it, and a click on the icon opens the window instead of turning
	// HIDI on or off.
	Open func()
	// QuitLabel is the label of the last menu entry, which makes Run
	// return. Empty means [QuitLabel].
	QuitLabel string
}

// Run keeps the icon in step with HIDI and carries out what the user asks
// through it. It returns when ctx is cancelled or the user chooses the
// last menu entry.
func Run(ctx context.Context, hidi Controller, host Host, log *slog.Logger, opts Options) error {
	view := func(s *ipc.Snapshot) View {
		v := ViewOf(s)
		if opts.Open != nil {
			v.Open = OpenLabel
		}
		v.Quit = opts.QuitLabel
		return v
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// One goroutine follows HIDI and reports every state, nil when HIDI
	// is gone.
	states := make(chan *ipc.Snapshot, 1)
	report := func(s *ipc.Snapshot) {
		for {
			select {
			case states <- s:
				return
			case <-ctx.Done():
				return
			default:
			}
			select { // make room: only the newest state matters
			case <-states:
			default:
			}
		}
	}
	go func() {
		for ctx.Err() == nil {
			hidi.Watch(ctx, 0, func(s ipc.Snapshot) { report(&s) }, nil)
			report(nil)
			select {
			case <-ctx.Done():
			case <-time.After(retryInterval):
			}
		}
	}()

	if err := host.Show(view(nil)); err != nil {
		return err
	}
	running := false
	// While HIDI is on and learning, the icon blinks between the colour
	// of on and that of learning, in step with the window and the lights
	// (package blink): a timer wakes the icon at the start of each half.
	var (
		shown View
		turn  <-chan time.Time
		timer *time.Timer
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	show := func() {
		v := shown
		now := time.Now()
		if blinks := v.Learning && v.Look == LookOn; blinks {
			if blink.Lit(now) {
				v.Look = LookLearning
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(time.Until(blink.Next(now)))
			turn = timer.C
		} else {
			turn = nil
		}
		if err := host.Show(v); err != nil {
			log.Warn("the icon could not be updated", "error", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-states:
			running = s != nil
			shown = view(s)
			show()
		case <-turn:
			show()
		case action, ok := <-host.Actions():
			if !ok {
				return nil
			}
			switch action {
			case ActionQuit:
				return nil
			case ActionOpen:
				if opts.Open != nil {
					opts.Open()
				}
			case ActionLearn:
				if !running {
					continue
				}
				askCtx, done := context.WithTimeout(ctx, askTimeout)
				_, err := hidi.ToggleLearning(askCtx)
				done()
				if err != nil && ctx.Err() == nil {
					log.Warn("learning mode could not be turned on or off", "error", err)
				}
			case ActionToggle:
				if !running {
					continue
				}
				askCtx, done := context.WithTimeout(ctx, askTimeout)
				_, err := hidi.Toggle(askCtx)
				done()
				if err != nil && ctx.Err() == nil {
					log.Warn("HIDI could not be turned on or off", "error", err)
				}
			}
		}
	}
}
