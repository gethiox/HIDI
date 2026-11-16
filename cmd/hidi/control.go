package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
)

// socketFlag adds the option that names the control socket.
func socketFlag(fs *flag.FlagSet) *string {
	path, err := ipc.DefaultPath()
	if err != nil {
		path = ""
	}
	return fs.String("socket", path, "`path` of the control socket of the running HIDI")
}

// controlHost is the running HIDI as the control interface sees it.
func controlHost(manager *instrument.Manager, logs *logging.Log, changes *ipc.Notifier) ipc.Host {
	return ipc.Host{
		Snapshot:  func() ipc.Snapshot { return snapshot(manager) },
		SetActive: manager.SetActive,
		Changes:   changes,
		LogLines: func(from, to int) []ipc.LogLine {
			lines, first := logs.Lines(from, to)
			out := make([]ipc.LogLine, len(lines))
			for i, l := range lines {
				out[i] = ipc.LogLine{Seq: first + i, Time: l.Time, Level: l.Level.String(), Component: l.Component, Text: l.Text}
			}
			return out
		},
		LogTotal: logs.Total,
	}
}

func snapshot(manager *instrument.Manager) ipc.Snapshot {
	s := ipc.Snapshot{Version: buildinfo.Get().String(), Active: manager.Active(), Instruments: []ipc.Instrument{}}
	for _, st := range manager.Status() {
		s.Instruments = append(s.Instruments, ipc.Instrument{
			Name:      st.Device.Name,
			Type:      st.Device.Type.String(),
			Hardware:  st.Device.Hardware.String(),
			Location:  st.Device.Location,
			Profile:   st.Profile,
			Octave:    st.State.Octave,
			Transpose: st.State.Transpose,
			Channel:   int(st.State.Channel) + 1,
			Velocity:  int(st.State.Velocity),
			Layout:    st.State.Layout,
			Sounding:  st.State.Sounding,
		})
	}
	// Two devices of the same model have the same name; where they are
	// plugged in keeps their order steady.
	slices.SortFunc(s.Instruments, func(a, b ipc.Instrument) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Location, b.Location))
	})
	return s
}

// describe writes a snapshot as text: a line for HIDI and two for each
// instrument.
func describe(w io.Writer, s ipc.Snapshot) {
	state := "on"
	if !s.Active {
		state = "off"
	}
	fmt.Fprintf(w, "%s is %s\n", s.Version, state)
	if len(s.Instruments) == 0 {
		fmt.Fprintln(w, "\nNo instruments. Connect a keyboard or a game controller.")
		return
	}
	for _, in := range s.Instruments {
		fmt.Fprintf(w, "\n%s  (%s, profile %q)\n", in.Name, in.Type, in.Profile)
		fmt.Fprintf(w, "  layout %s  octave %+d  transpose %+d  channel %d  velocity %d  %s\n",
			in.Layout, in.Octave, in.Transpose, in.Channel, in.Velocity, sounding(in.Sounding))
	}
}

func sounding(n int) string {
	switch n {
	case 0:
		return "silent"
	case 1:
		return "1 note sounding"
	}
	return fmt.Sprintf("%d notes sounding", n)
}

// controlError explains why the running HIDI could not be reached.
func controlError(stderr io.Writer, command string, err error) int {
	if errors.Is(err, ipc.ErrNotRunning) {
		fmt.Fprintf(stderr, "hidi %s: HIDI is not running; start it with \"hidi run\"\n", command)
	} else {
		fmt.Fprintf(stderr, "hidi %s: %v\n", command, err)
	}
	return 1
}

// cmdControl implements status, on, off and toggle.
func cmdControl(ctx context.Context, command string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	socket := socketFlag(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	client := ipc.NewClient(*socket)
	var s ipc.Snapshot
	var err error
	switch command {
	case "status":
		s, err = client.Status(ctx)
	case "on":
		s, err = client.SetActive(ctx, true)
	case "off":
		s, err = client.SetActive(ctx, false)
	case "toggle":
		s, err = client.Toggle(ctx)
	}
	if err != nil {
		return controlError(stderr, command, err)
	}
	describe(stdout, s)
	return 0
}

// monitorLogLines is how much of the log the monitor shows.
const monitorLogLines = 12

// cmdMonitor keeps showing the state of the running HIDI and the end of
// its log. On a terminal the view is redrawn in place; elsewhere, and
// with colours turned off, every change is written below the last.
func cmdMonitor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	socket := socketFlag(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	file, isFile := stdout.(*os.File)
	view := &monitorView{out: stdout, redraw: isFile && logging.TerminalColour(file)}

	err := ipc.NewClient(*socket).Watch(ctx, monitorLogLines, view.snapshot, view.log)
	if view.redraw {
		fmt.Fprint(stdout, showCursor)
	}
	if err != nil {
		return controlError(stderr, "monitor", err)
	}
	return 0
}

// Escape sequences of the redrawn view.
const (
	clearScreen = "\x1b[H\x1b[2J" // cursor to the top left, erase everything
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
)

// monitorView draws what the monitor receives.
type monitorView struct {
	out    io.Writer
	redraw bool

	mu    sync.Mutex
	state ipc.Snapshot
	have  bool
	lines []string // the end of the log, oldest first
}

func (v *monitorView) snapshot(s ipc.Snapshot) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state, v.have = s, true
	if v.redraw {
		v.draw()
		return
	}
	describe(v.out, s)
	fmt.Fprintln(v.out)
}

func (v *monitorView) log(l ipc.LogLine) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.redraw {
		fmt.Fprintln(v.out, logging.Strip(l.Text))
		return
	}
	v.lines = append(v.lines, l.Text)
	if len(v.lines) > monitorLogLines {
		v.lines = v.lines[len(v.lines)-monitorLogLines:]
	}
	if v.have {
		v.draw()
	}
}

// draw replaces the screen with the current view, in one write so that the
// terminal does not show it half done.
func (v *monitorView) draw() {
	var b strings.Builder
	b.WriteString(hideCursor + clearScreen)
	describe(&b, v.state)
	if len(v.lines) > 0 {
		b.WriteString("\nLog\n")
		for _, line := range v.lines {
			b.WriteString(line + "\x1b[0m\n")
		}
	}
	b.WriteString("\nCtrl+C closes this view; HIDI keeps running.\n")
	io.WriteString(v.out, b.String())
}
