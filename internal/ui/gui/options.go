package gui

import (
	"context"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/settings"
)

// Controller is the running HIDI as the window needs it; *ipc.Client is
// one.
type Controller interface {
	Watch(ctx context.Context, logs int, onSnapshot func(ipc.Snapshot), onLog func(ipc.LogLine)) error
	Toggle(ctx context.Context) (ipc.Snapshot, error)
	ToggleLearning(ctx context.Context) (ipc.Snapshot, error)
	// Preview makes HIDI use text in place of a profile file until the
	// file changes; an empty text ends that.
	Preview(ctx context.Context, file, text string) error
}

// Options change how the window behaves.
type Options struct {
	// FPS limits how often the window is drawn. Zero means no limit: the
	// window keeps to the monitor's refresh rate.
	FPS int
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
	// Paths says where HIDI's settings and profiles are, for the settings
	// view. An empty Root means there is no such directory.
	Paths settings.Paths
	// ShowSettings opens the window with the settings showing.
	ShowSettings bool
	// SnapshotPace, if set, is told how often the window wants HIDI's
	// state at the most, once the window knows its monitor and before it
	// starts to follow HIDI.
	SnapshotPace func(time.Duration)
	// Ports, if set, lists the MIDI ports that exist, for choosing those
	// every instrument also sends to.
	Ports func() ([]output.Port, error)
	// ShowAll draws the input and the notes of every instrument,
	// whatever the settings say. It is for pictures.
	ShowAll bool
	// EditProfile, if set, is a profile file the window opens with, ready
	// to be changed.
	EditProfile string
}
