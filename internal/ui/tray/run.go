package tray

import (
	"context"
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

// BlinkPhase is how long the icon of a HIDI that is on and in learning
// mode shows each of its two colours. Twice a second is nothing to a
// notification area: an icon there is a few hundred pixels.
var BlinkPhase = 500 * time.Millisecond

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
	// of on and that of learning.
	var (
		shown  View
		blink  <-chan time.Time
		ticker *time.Ticker
		other  bool // the half of the blinking that is the learning colour
	)
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	show := func() {
		v := shown
		if blinks := v.Learning && v.Look == LookOn; blinks && other {
			v.Look = LookLearning
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
			switch blinks := shown.Learning && shown.Look == LookOn; {
			case blinks && ticker == nil:
				ticker = time.NewTicker(BlinkPhase)
				blink, other = ticker.C, true
			case !blinks && ticker != nil:
				ticker.Stop()
				ticker, blink, other = nil, nil, false
			}
			show()
		case <-blink:
			other = !other
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
