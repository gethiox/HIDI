package main

import (
	"context"
	"flag"
	"fmt"
	"io"

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
	fs.IntVar(&opts.BackgroundFPS, "background-fps", 0, "draw at most this many times a second while another window has the focus; 0 keeps to the monitor's refresh rate")
	fs.IntVar(&opts.LogHeight, "log-height", 0, "open the window with the log pulled up this many pixels")
	fs.StringVar(&opts.Screenshot, "screenshot", "", "save a picture of the window to this file after a moment, then close it")
	root, _ := placeFlags(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	// The settings file gives what the command line leaves open.
	pl := findPlace(*root, "", false)
	pl.tell(stderr, "gui")
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if !given["background-fps"] {
		opts.BackgroundFPS = pl.settings.Window.BackgroundFPS
	}
	if !given["log-height"] {
		opts.LogHeight = pl.settings.Window.LogHeight
	}
	if opts.BackgroundFPS < 0 || opts.LogHeight < 0 {
		fmt.Fprintln(stderr, "hidi gui: -background-fps and -log-height cannot be negative")
		return 2
	}
	if err := gui.Run(ctx, ipc.NewClient(*socket), opts); err != nil {
		fmt.Fprintf(stderr, "hidi gui: %v\n", err)
		return 1
	}
	return 0
}
