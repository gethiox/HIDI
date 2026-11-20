package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/spf13/pflag"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/buildinfo"
	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/settings"
)

// socketFlag adds the option that names the control socket.
func socketFlag(fs *pflag.FlagSet) *string {
	path, err := ipc.DefaultPath()
	if err != nil {
		path = ""
	}
	return fs.String("socket", path, "`path` of the control socket of the running HIDI")
}

// controlHost is the running HIDI as the control interface sees it.
func controlHost(manager *instrument.Manager, logs *logging.Log, changes *ipc.Notifier, seen *seenDevices) ipc.Host {
	return ipc.Host{
		Snapshot:    func() ipc.Snapshot { return withDevices(snapshot(manager), manager, seen) },
		SetActive:   manager.SetActive,
		SetLearning: manager.SetLearning,
		Changes:     changes,
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

// withDevices adds the devices to a snapshot: those connected, and those
// met before that are not, ordered by name.
func withDevices(s ipc.Snapshot, manager *instrument.Manager, seen *seenDevices) ipc.Snapshot {
	connected := map[seenDevice]bool{}
	for _, d := range manager.Devices() {
		known := seenDevice{Name: logging.Printable(d.Info.Name), Type: d.Info.Type.String()}
		if connected[known] {
			continue // two of a model show as one
		}
		connected[known] = true
		seen.note(known)
		s.Devices = append(s.Devices, ipc.Device{Name: known.Name, Type: known.Type, Connected: true, Allowed: d.Allowed})
	}
	for _, d := range seen.all() {
		if !connected[d] {
			s.Devices = append(s.Devices, ipc.Device{Name: d.Name, Type: d.Type})
		}
	}
	slices.SortFunc(s.Devices, func(a, b ipc.Device) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Type, b.Type))
	})
	return s
}

// keyRole names what a key does, for the snapshot.
func keyRole(action engine.Action) (role string, note int) {
	switch a := action.(type) {
	case engine.Note:
		return "note", int(a.Note)
	case engine.Command:
		return "command", 0
	case engine.Passthrough:
		return "passthrough", 0
	case engine.ControlChange:
		return "controller", 0
	case engine.PitchBend:
		return "bend", 0
	}
	return "program", 0
}

func snapshot(manager *instrument.Manager) ipc.Snapshot {
	s := ipc.Snapshot{Version: buildinfo.Get().String(), Active: manager.Active(), Learning: manager.Learning(), Instruments: []ipc.Instrument{}}
	for _, st := range manager.Status() {
		buttons := make([]ipc.Button, len(st.Input.Buttons))
		for i, b := range st.Input.Buttons {
			buttons[i] = ipc.Button{Name: b.Code.String(), Part: b.Part, Down: b.Down}
		}
		axes := make([]ipc.Axis, len(st.Input.Axes))
		for i, a := range st.Input.Axes {
			axes[i] = ipc.Axis{Name: a.Code.String(), Part: a.Part, Value: a.Value}
		}
		keys := make([]ipc.Key, len(st.Keys))
		for i, k := range st.Keys {
			role, note := keyRole(k.Action)
			keys[i] = ipc.Key{Name: k.Code.String(), Role: role, Note: note}
		}
		var lights []ipc.KeyLight
		rest := ""
		if st.Lit {
			rest = settings.FormatColour(st.LightRest)
			lights = make([]ipc.KeyLight, 0, len(st.Lights))
			for code, colour := range st.Lights {
				lights = append(lights, ipc.KeyLight{Name: code.String(), Colour: settings.FormatColour(colour)})
			}
			slices.SortFunc(lights, func(a, b ipc.KeyLight) int { return cmp.Compare(a.Name, b.Name) })
		}
		s.Instruments = append(s.Instruments, ipc.Instrument{
			Trail:       &ipc.Trail{Seq: st.Trail.Seq, StepMS: int(st.Trail.Step / time.Millisecond), Input: st.Trail.Input, MIDI: st.Trail.MIDI},
			Keys:        keys,
			Lit:         st.Lit,
			Lights:      lights,
			LightRest:   rest,
			Buttons:     buttons,
			Axes:        axes,
			Name:        logging.Printable(st.Device.Name),
			Type:        st.Device.Type.String(),
			Hardware:    st.Device.Hardware.String(),
			Location:    logging.Printable(st.Device.Location),
			Profile:     st.Profile,
			ProfileFile: profileFile(st.ProfileSource),
			Octave:      st.State.Octave,
			Transpose:   st.State.Transpose,
			Channel:     int(st.State.Channel) + 1,
			Velocity:    int(st.State.Velocity),
			Layout:      st.State.Layout,
			Chord:       st.State.Chord,
			Learning:    st.State.Learning,
			Sounding:    st.State.Sounding,
			Notes:       st.State.NoteNumbers(),
			InputRate:   st.InputRate,
			MIDIRate:    st.MIDIRate,
		})
	}
	// Two devices of the same model have the same name; where they are
	// plugged in keeps their order steady. One piece of hardware can be
	// two devices in one place, a joystick that is also a keyboard; their
	// kind tells those apart.
	slices.SortFunc(s.Instruments, func(a, b ipc.Instrument) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Location, b.Location),
			cmp.Compare(a.Type, b.Type), cmp.Compare(a.Hardware, b.Hardware))
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
		// Every number has a fixed width and the text of varying length
		// comes last, so that nothing moves sideways while the monitor
		// redraws.
		fmt.Fprintf(w, "  octave %+3d  transpose %+4d  channel %2d  velocity %3d  notes %2d  layout %s\n",
			in.Octave, in.Transpose, in.Channel, in.Velocity, in.Sounding, in.Layout)
		fmt.Fprintf(w, "  input %4d/s  MIDI %4d/s\n", in.InputRate, in.MIDIRate)
		if in.Chord != "" {
			fmt.Fprintf(w, "  chord %s\n", in.Chord)
		}
		if in.Learning {
			fmt.Fprintln(w, "  learning: axes stand still until they are moved far")
		}
	}
}

// controlError explains why the running HIDI could not be reached.
func controlError(stderr io.Writer, command string, err error) int {
	if errors.Is(err, ipc.ErrNotRunning) {
		fmt.Fprintf(stderr, "hidi %s: HIDI is not running; start it with \"hidi\"\n", command)
	} else {
		fmt.Fprintf(stderr, "hidi %s: %v\n", command, err)
	}
	return 1
}

// cmdControl implements status, on, off and toggle.
func cmdControl(ctx context.Context, command string, args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet(command, pflag.ContinueOnError)
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
	fs := pflag.NewFlagSet("monitor", pflag.ContinueOnError)
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

// profileFile returns the file a profile comes from, or nothing for a
// profile that is inside HIDI.
func profileFile(source string) string {
	if strings.HasPrefix(source, config.BuiltinSource) {
		return ""
	}
	return source
}
