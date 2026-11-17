package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/ui/gui"
)

// cmdGui opens HIDI's window until it is closed or the command is
// interrupted. The window is a client of the running HIDI and does not
// start or stop it.
func cmdGui(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	socket := socketFlag(fs)
	var opts gui.Options
	fs.IntVar(&opts.FPS, "fps", 0, "draw at most this many times a second; 0 keeps to the monitor's refresh rate")
	fs.IntVar(&opts.BackgroundFPS, "background-fps", 0, "draw at most this many times a second while another window has the focus; 0 sets no limit of its own")
	fs.IntVar(&opts.LogHeight, "log-height", 0, "open the window with the log pulled up this many pixels")
	fs.StringVar(&opts.Screenshot, "screenshot", "", "save a picture of the window to this file after a moment, then close it")
	fs.BoolVar(&opts.ShowSettings, "settings", false, "open the window with the settings showing")
	fs.StringVar(&opts.EditProfile, "edit", "", "open the window with this profile `file`, ready to be changed")
	root, _ := placeFlags(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	// The settings file gives what the command line leaves open.
	pl := findPlace(*root, "", false)
	pl.tell(stderr, "gui")
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if !given["fps"] {
		opts.FPS = pl.settings.Window.FPS
	}
	if !given["background-fps"] {
		opts.BackgroundFPS = pl.settings.Window.BackgroundFPS
	}
	opts.Paths = pl.paths
	if !given["log-height"] {
		opts.LogHeight = pl.settings.Window.LogHeight
	}
	if opts.FPS < 0 || opts.BackgroundFPS < 0 || opts.LogHeight < 0 {
		fmt.Fprintln(stderr, "hidi gui: -fps, -background-fps and -log-height cannot be negative")
		return 2
	}
	// The window draws moving axes, so it asks for HIDI's state as often
	// as a fast monitor shows a picture.
	client := ipc.NewClient(*socket)
	client.SnapshotEvery = 4 * time.Millisecond
	if err := gui.Run(ctx, client, opts); err != nil {
		fmt.Fprintf(stderr, "hidi gui: %v\n", err)
		return 1
	}
	return 0
}
