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
}

const (
	// retryInterval is how long the icon waits before it looks for HIDI
	// again.
	retryInterval = time.Second
	// askTimeout limits a request to HIDI.
	askTimeout = 2 * time.Second
)

// Run keeps the icon in step with HIDI and carries out what the user asks
// through it. It returns when ctx is cancelled or the user removes the
// icon.
func Run(ctx context.Context, hidi Controller, host Host, log *slog.Logger) error {
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

	if err := host.Show(ViewOf(nil)); err != nil {
		return err
	}
	running := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-states:
			running = s != nil
			if err := host.Show(ViewOf(s)); err != nil {
				log.Warn("the icon could not be updated", "error", err)
			}
		case action, ok := <-host.Actions():
			if !ok {
				return nil
			}
			switch action {
			case ActionQuit:
				return nil
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
