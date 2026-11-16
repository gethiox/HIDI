package gui

import (
	"context"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
)

// logLines is how much of the log already written the window asks for.
const logLines = 500

// retryInterval is how long the window waits before it looks for HIDI
// again.
const retryInterval = time.Second

// Follow keeps the model in step with HIDI until ctx is cancelled: it
// watches HIDI, and when HIDI is gone, says so and looks for it again.
func Follow(ctx context.Context, hidi Controller, m *Model) {
	for ctx.Err() == nil {
		hidi.Watch(ctx, logLines, func(s ipc.Snapshot) { m.SetSnapshot(&s) }, m.AddLog)
		m.SetSnapshot(nil)
		select {
		case <-ctx.Done():
		case <-time.After(retryInterval):
		}
	}
}
