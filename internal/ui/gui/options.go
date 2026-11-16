package gui

import (
	"context"

	"github.com/gethiox/HIDI/internal/ipc"
)

// Controller is the running HIDI as the window needs it; *ipc.Client is
// one.
type Controller interface {
	Watch(ctx context.Context, logs int, onSnapshot func(ipc.Snapshot), onLog func(ipc.LogLine)) error
	Toggle(ctx context.Context) (ipc.Snapshot, error)
}

// Options change how the window behaves.
type Options struct {
	// BackgroundFPS limits how often the window is drawn while another
	// window has the focus. Zero means no limit of its own: the window
	// then keeps to the monitor's refresh rate, as it does in front.
	BackgroundFPS int
	// LogHeight is the height of the log when the window opens, in
	// pixels. Zero shows only its last line.
	LogHeight int
	// Screenshot, if set, is a file the window saves a picture of itself
	// to after a moment, and then closes. It is for the documentation and
	// for tests.
	Screenshot string
}
