package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/midi"
)

// Controls used by the tests. The numbers are arbitrary but distinct.
var (
	keyA, keyB, keyC = control.Key(30), control.Key(48), control.Key(46)
	keyShift         = control.Key(42)
	keyUp, keyDown   = control.Key(103), control.Key(108)
	keyPanic         = control.Key(1)
	stickX           = control.Axis(0)
	trigger          = control.Axis(2)
)

func key(code control.Code, action Action, modifiers ...control.Code) Binding {
	return Binding{Source: Source{Code: code, Modifiers: modifiers}, Action: action}
}

func baseProfile(bindings ...Binding) Profile {
	return Profile{
		Layouts:  []Layout{{Name: "main", Bindings: bindings}},
		Defaults: Defaults{Velocity: 100},
	}
}

// rig drives an engine and renders its output as text, which keeps the
// expectations readable.
type rig struct {
	t *testing.T
	e *Engine
}

func newRig(t *testing.T, p Profile) *rig {
	t.Helper()
	e, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &rig{t, e}
}

func render(msgs []midi.Message) string {
	parts := make([]string, len(msgs))
	for i, m := range msgs {
		parts[i] = m.String()
	}
	return strings.Join(parts, "; ")
}

// set feeds one event and checks the messages it produces.
func (r *rig) set(code control.Code, value float64, want string) {
	r.t.Helper()
	got := render(r.e.Handle(control.Event{Code: code, Value: value}, nil))
	if got != want {
		r.t.Errorf("%v = %v\n got: %s\nwant: %s", code, value, got, want)
	}
}

func (r *rig) press(code control.Code, want string)   { r.t.Helper(); r.set(code, 1, want) }
func (r *rig) release(code control.Code, want string) { r.t.Helper(); r.set(code, 0, want) }

// expect checks the messages something other than an event produced.
func (r *rig) expect(what string, msgs []midi.Message, want string) {
	r.t.Helper()
	if got := render(msgs); got != want {
		r.t.Errorf("%s\n got: %s\nwant: %s", what, got, want)
	}
}

func (r *rig) reset(want string) {
	r.t.Helper()
	if got := render(r.e.Reset(nil)); got != want {
		r.t.Errorf("Reset\n got: %s\nwant: %s", got, want)
	}
}

func TestNoteOnOff(t *testing.T) {
	r := newRig(t, baseProfile(key(keyA, Note{Note: 60})))
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyA, "") // a repeated press report changes nothing
	if got := r.e.State().Sounding; got != 1 {
		t.Errorf("Sounding = %d, want 1", got)
	}
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")
	r.release(keyA, "")
	r.press(keyB, "") // unbound
}

func TestNoteOptions(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 60, ChannelOffset: 2, Velocity: 5}))
	p.Defaults.Channel = 15
	r := newRig(t, p)
	// The channel offset wraps around after channel 16.
	r.press(keyA, "NoteOn ch=2 note=C3 velocity=5")
	r.release(keyA, "NoteOff ch=2 note=C3 velocity=0")
}

// The release must turn off the note the press turned on, whatever happened
// to the instrument state in between.
func TestStateChangesWhileHolding(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 60}))
	p.Layouts = append(p.Layouts, Layout{Name: "other", Bindings: []Binding{key(keyB, Note{Note: 10})}})
	p.Global = []Binding{
		key(keyUp, CommandOctaveUp),
		key(keyDown, CommandChannelUp),
		key(keyC, CommandTransposeUp),
		key(keyShift, CommandLayoutNext),
		key(keyPanic, CommandLayoutPrevious),
	}
	r := newRig(t, p)

	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyUp, "")
	r.press(keyDown, "")
	r.press(keyC, "")
	r.press(keyShift, "") // keyA is not bound in the new layout at all
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")

	// Back in the first layout, a new press uses the new state.
	r.press(keyPanic, "")
	r.press(keyA, "NoteOn ch=2 note=C#4 velocity=100")
	r.release(keyA, "NoteOff ch=2 note=C#4 velocity=0")
}

// Every layout has its own commands. A key that changes the layout does
// nothing in the new one when it is let go of, whatever the key does
// there; a key that plays a note turns it off when let go of, in whatever
// layout that happens.
func TestLayoutsHaveTheirOwnCommands(t *testing.T) {
	p := Profile{
		Layouts: []Layout{
			{Name: "main", Bindings: []Binding{
				key(keyA, CommandLayoutNext),
				key(keyB, Note{Note: 60}),
				key(keyC, CommandLayoutNext, keyShift),
			}},
			{Name: "other", Bindings: []Binding{
				key(keyA, Note{Note: 62}),
				key(keyB, CommandLayoutPrevious),
				key(keyC, Note{Note: 64}),
				key(keyShift, CommandOctaveUp),
			}},
		},
		Defaults: Defaults{Velocity: 100},
	}
	r := newRig(t, p)
	layout := func(want string) {
		t.Helper()
		if got := r.e.State().Layout; got != want {
			t.Errorf("layout %q, want %q", got, want)
		}
	}

	r.press(keyA, "") // to the other layout, where the key plays a note
	layout("other")
	r.release(keyA, "")
	r.press(keyA, "NoteOn ch=1 note=D3 velocity=100")
	r.release(keyA, "NoteOff ch=1 note=D3 velocity=0")

	r.press(keyB, "") // back, where the key plays a note
	layout("main")
	r.release(keyB, "")

	// A note held across a change of layout ends when its key is let go.
	r.press(keyB, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyA, "")
	layout("other")
	r.release(keyB, "NoteOff ch=1 note=C3 velocity=0")
	r.release(keyA, "")

	// With a modifier: Shift does nothing in the first layout by itself,
	// raises the octave in the other, and C changes the layout with it.
	r.press(keyB, "") // back to main
	r.release(keyB, "")
	r.press(keyShift, "")
	r.press(keyC, "")
	layout("other")
	r.release(keyShift, "") // pressed in main, where it did nothing
	r.release(keyC, "")     // no note to turn off
	if got := r.e.State().Octave; got != 0 {
		t.Errorf("octave %d: a key let go of in another layout ran that layout's command", got)
	}
	r.press(keyC, "NoteOn ch=1 note=E3 velocity=100")
	r.release(keyC, "NoteOff ch=1 note=E3 velocity=0")
}

func TestNoteOutOfRangeIsSilent(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 120}), key(keyUp, CommandOctaveUp), key(keyDown, CommandOctaveDown))
	r := newRig(t, p)
	r.press(keyUp, "")
	r.press(keyA, "")
	r.release(keyUp, "")
	r.press(keyDown, "") // back in range while the key is held
	r.release(keyA, "")  // nothing was turned on, so nothing is turned off
	r.press(keyA, "NoteOn ch=1 note=C8 velocity=100")
}

func TestCollisionModes(t *testing.T) {
	const on, off = "NoteOn ch=1 note=C3 velocity=100", "NoteOff ch=1 note=C3 velocity=0"
	tests := []struct {
		mode CollisionMode
		// Output of: press A, press B, release A, release B.
		want [4]string
	}{
		{CollisionInterrupt, [4]string{on, off + "; " + on, "", off}},
		{CollisionNoRepeat, [4]string{on, "", "", off}},
		{CollisionRetrigger, [4]string{on, on, "", off}},
		{CollisionOff, [4]string{on, on, off, off}},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String(), func(t *testing.T) {
			p := baseProfile(key(keyA, Note{Note: 60}), key(keyB, Note{Note: 60}))
			p.Collision = tt.mode
			r := newRig(t, p)
			r.press(keyA, tt.want[0])
			r.press(keyB, tt.want[1])
			if got := r.e.State().Sounding; got != 1 {
				t.Errorf("Sounding = %d, want 1", got)
			}
			r.release(keyA, tt.want[2])
			r.release(keyB, tt.want[3])
			if got := r.e.State().Sounding; got != 0 {
				t.Errorf("Sounding after release = %d, want 0", got)
			}
		})
	}
}

func TestCommands(t *testing.T) {
	p := baseProfile()
	p.Layouts = append(p.Layouts, Layout{Name: "second"})
	p.Defaults = Defaults{Octave: 1, Transpose: 2, Channel: 3, Velocity: 100, Layout: "main"}
	up, down := keyUp, keyDown

	tests := []struct {
		up, down Command
		// get reads the value the pair controls.
		get func(State) any
		// Values after: up, up-release, down, down, and the reset combination.
		afterUp, afterDownTwice, afterReset any
	}{
		{CommandOctaveUp, CommandOctaveDown, func(s State) any { return s.Octave }, 2, 0, 1},
		{CommandTransposeUp, CommandTransposeDown, func(s State) any { return s.Transpose }, 3, 1, 2},
		{CommandChannelUp, CommandChannelDown, func(s State) any { return s.Channel }, uint8(4), uint8(2), uint8(3)},
		{CommandLayoutNext, CommandLayoutPrevious, func(s State) any { return s.Layout }, "second", "main", "main"},
		{CommandVelocityUp, CommandVelocityDown, func(s State) any { return s.Velocity }, uint8(116), uint8(84), uint8(100)},
	}
	for _, tt := range tests {
		t.Run(tt.up.String(), func(t *testing.T) {
			p := p
			p.Global = []Binding{key(up, tt.up), key(down, tt.down)}
			r := newRig(t, p)
			check := func(step string, want any) {
				t.Helper()
				if got := tt.get(r.e.State()); got != want {
					t.Errorf("after %s: got %v, want %v", step, got, want)
				}
			}
			tap := func(code control.Code) { r.press(code, ""); r.release(code, "") }

			tap(up)
			check("up", tt.afterUp)
			tap(down)
			tap(down)
			check("down twice", tt.afterDownTwice)

			// Holding both commands of a pair restores the default.
			r.press(up, "")
			r.press(down, "")
			check("reset combination", tt.afterReset)
			r.release(up, "")
			r.release(down, "")
			check("releasing the combination", tt.afterReset)
		})
	}
}

// The velocity moves along a ladder from the profile's velocity: a step
// past an end stops there, and the step back goes to the value before,
// so the profile's velocity is always reached again.
func TestVelocityLadder(t *testing.T) {
	p := baseProfile(key(keyUp, CommandVelocityUp), key(keyDown, CommandVelocityDown))
	p.Defaults.Velocity = 64
	r := newRig(t, p)
	walk := func(code control.Code, want ...uint8) {
		t.Helper()
		for i, w := range want {
			r.press(code, "")
			r.release(code, "")
			if got := r.e.State().Velocity; got != w {
				t.Errorf("press %d of %v: velocity %d, want %d", i+1, code, got, w)
			}
		}
	}
	walk(keyUp, 80, 96, 112, 127, 127, 127)
	walk(keyDown, 112, 96, 80, 64, 48, 32, 16, 1, 1)
	walk(keyUp, 16, 32, 48, 64)

	// Another step: the velocity goes to the nearest place on the new
	// ladder, and the profile's velocity is on it.
	walk(keyUp, 80)
	r.e.SetVelocityStep(10)
	if got := r.e.State().Velocity; got != 84 {
		t.Errorf("velocity after the step changed: %d, want 84", got)
	}
	walk(keyDown, 74, 64, 54)
	walk(keyUp, 64)
	r.e.SetVelocityStep(100)
	walk(keyUp, 127, 127)
	walk(keyDown, 64, 1, 1)
	walk(keyUp, 64)
	r.e.SetVelocityStep(0) // the default
	walk(keyUp, 80)
	// From the top, a smaller step lands below 127 again.
	walk(keyUp, 96, 112, 127)
	r.e.SetVelocityStep(1)
	if got := r.e.State().Velocity; got != 127 {
		t.Errorf("velocity at the top with a step of 1: %d", got)
	}
	walk(keyDown, 126)
	// 126 with a step of 10 is nearest to the top, which the ladder of
	// that step reaches only by stopping there.
	r.e.SetVelocityStep(10)
	if got := r.e.State().Velocity; got != 127 {
		t.Errorf("velocity near the top with a step of 10: %d, want 127", got)
	}
}

func TestCommandLimits(t *testing.T) {
	p := baseProfile(key(keyUp, CommandChannelUp), key(keyDown, CommandVelocityDown), key(keyA, CommandOctaveUp))
	p.Defaults.Channel = 15
	p.Defaults.Velocity = 3
	p.Defaults.Octave = MaxOctave
	r := newRig(t, p)
	r.press(keyUp, "")
	r.press(keyDown, "")
	r.press(keyA, "")
	if got, want := r.e.State(), (State{Octave: MaxOctave, Channel: 15, Velocity: 1, Layout: "main"}); got != want {
		t.Errorf("State = %+v, want %+v", got, want)
	}
}

func TestModifiers(t *testing.T) {
	r := newRig(t, baseProfile(
		key(keyA, Note{Note: 60}),
		key(keyA, Note{Note: 72}, keyShift),
		key(keyA, Note{Note: 84}, keyShift, keyC),
		key(keyB, Note{Note: 62}),
	))

	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")

	r.press(keyShift, "") // a modifier with no binding of its own does nothing
	r.press(keyA, "NoteOn ch=1 note=C4 velocity=100")
	// Releasing the modifier first does not change what the release undoes.
	r.release(keyShift, "")
	r.release(keyA, "NoteOff ch=1 note=C4 velocity=0")

	// The binding with the most held modifiers wins.
	r.press(keyShift, "")
	r.press(keyC, "")
	r.press(keyA, "NoteOn ch=1 note=C5 velocity=100")
	r.release(keyA, "NoteOff ch=1 note=C5 velocity=0")

	// A binding without modifiers still applies while modifiers are held.
	r.press(keyB, "NoteOn ch=1 note=D3 velocity=100")
}

func TestLayoutBindingBeatsGlobal(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 60}))
	p.Global = []Binding{key(keyA, Note{Note: 1}), key(keyB, Note{Note: 2})}
	r := newRig(t, p)
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyB, "NoteOn ch=1 note=D-2 velocity=100")
}

func TestParts(t *testing.T) {
	p := baseProfile(
		Binding{Source: Source{Code: keyA}, Action: Note{Note: 60}},
		Binding{Source: Source{Code: keyA, Part: "Touchpad"}, Action: Note{Note: 72}},
	)
	e, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	got := render(e.Handle(control.Event{Code: keyA, Part: "Touchpad", Value: 1}, nil))
	if want := "NoteOn ch=1 note=C4 velocity=100"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := render(e.Handle(control.Event{Code: keyA, Part: "Other", Value: 1}, nil)); got != "" {
		t.Errorf("event from an unbound part produced %s", got)
	}
}

func TestProgramChange(t *testing.T) {
	r := newRig(t, baseProfile(key(keyA, ProgramChange{Program: 5, ChannelOffset: 1})))
	r.press(keyA, "ProgramChange ch=2 program=5")
	r.release(keyA, "")
}

func TestControlChangeFromKey(t *testing.T) {
	r := newRig(t, baseProfile(key(keyA, ControlChange{Controller: 64})))
	r.press(keyA, "ControlChange ch=1 controller=64 value=127")
	r.release(keyA, "ControlChange ch=1 controller=64 value=0")
}

func TestControlChangeFromTrigger(t *testing.T) {
	r := newRig(t, baseProfile(Binding{
		Source: Source{Code: trigger, Deadzone: 0.5},
		Action: ControlChange{Controller: 7},
	}))
	r.set(trigger, 0.1, "ControlChange ch=1 controller=7 value=0") // first report, inside the dead zone
	r.set(trigger, 0.15, "")                                       // no change on the wire
	r.set(trigger, 0.75, "ControlChange ch=1 controller=7 value=64")
	r.set(trigger, 1, "ControlChange ch=1 controller=7 value=127")
	r.set(trigger, 7, "") // clamped to 1
	r.set(trigger, -3, "ControlChange ch=1 controller=7 value=0")
}

func TestInvert(t *testing.T) {
	r := newRig(t, baseProfile(Binding{
		Source: Source{Code: trigger, Invert: true},
		Action: ControlChange{Controller: 7},
	}))
	r.set(trigger, 0, "ControlChange ch=1 controller=7 value=127")
	r.set(trigger, 1, "ControlChange ch=1 controller=7 value=0")
}

func TestCenteredControlChange(t *testing.T) {
	r := newRig(t, baseProfile(Binding{
		Source: Source{Code: stickX, Centered: true, Deadzone: 0.1},
		Action: ControlChange{Controller: 10},
	}))
	r.set(stickX, 0.52, "ControlChange ch=1 controller=10 value=64") // inside the dead zone
	r.set(stickX, 1, "ControlChange ch=1 controller=10 value=127")
	r.set(stickX, 0, "ControlChange ch=1 controller=10 value=0")
	r.reset("ControlChange ch=1 controller=10 value=64")
}

func TestPitchBend(t *testing.T) {
	t.Run("centered", func(t *testing.T) {
		r := newRig(t, baseProfile(Binding{
			Source: Source{Code: stickX, Centered: true, Deadzone: 0.1},
			Action: PitchBend{},
		}))
		r.set(stickX, 0.5, "PitchBend ch=1 bend=+0")
		r.set(stickX, 0.53, "") // inside the dead zone
		r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
		r.set(stickX, 0, "PitchBend ch=1 bend=-8192")
	})
	t.Run("inverted", func(t *testing.T) {
		r := newRig(t, baseProfile(Binding{
			Source: Source{Code: stickX, Centered: true, Invert: true},
			Action: PitchBend{},
		}))
		r.set(stickX, 1, "PitchBend ch=1 bend=-8192")
	})
	t.Run("one direction", func(t *testing.T) {
		r := newRig(t, baseProfile(
			key(keyA, PitchBend{}),
			key(keyB, PitchBend{Down: true, ChannelOffset: 1}),
		))
		r.press(keyA, "PitchBend ch=1 bend=+8191")
		r.release(keyA, "PitchBend ch=1 bend=+0")
		r.press(keyB, "PitchBend ch=2 bend=-8192")
		r.release(keyB, "PitchBend ch=2 bend=+0")
	})
}

// The two halves of an axis are independent sources.
func TestAxisHalves(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Range: RangeUpper}, Action: ControlChange{Controller: 1}},
		Binding{Source: Source{Code: stickX, Range: RangeLower}, Action: ControlChange{Controller: 2}},
	))
	r.set(stickX, 0.5, "ControlChange ch=1 controller=1 value=0; ControlChange ch=1 controller=2 value=0")
	r.set(stickX, 1, "ControlChange ch=1 controller=1 value=127")
	r.set(stickX, 0, "ControlChange ch=1 controller=1 value=0; ControlChange ch=1 controller=2 value=127")
	r.set(stickX, 0.5, "ControlChange ch=1 controller=2 value=0")
}

// A hat or stick can play notes; the old direction is released before the
// new one is pressed, and resting near the threshold does not chatter.
func TestNotesFromAxis(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Range: RangeUpper}, Action: Note{Note: 60}},
		Binding{Source: Source{Code: stickX, Range: RangeLower}, Action: Note{Note: 62}},
	))
	r.set(stickX, 0.75, "") // the trigger point itself: half the hysteresis short
	r.set(stickX, 0.76, "NoteOn ch=1 note=C3 velocity=100")
	r.set(stickX, 0.75, "") // back at the point: still held
	r.set(stickX, 0.76, "")
	r.set(stickX, 0, "NoteOff ch=1 note=C3 velocity=0; NoteOn ch=1 note=D3 velocity=100")
	r.set(stickX, 0.5, "NoteOff ch=1 note=D3 velocity=0")
}

func TestCommandFromAxis(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Range: RangeUpper}, Action: CommandOctaveUp},
		Binding{Source: Source{Code: stickX, Range: RangeLower}, Action: CommandOctaveDown},
	))
	r.set(stickX, 1, "")
	r.set(stickX, 0.5, "")
	r.set(stickX, 1, "")
	if got := r.e.State().Octave; got != 2 {
		t.Errorf("Octave = %d, want 2", got)
	}
	r.set(stickX, 0, "")
	if got := r.e.State().Octave; got != 1 {
		t.Errorf("Octave = %d, want 1 (no reset: the halves are never held together)", got)
	}
}

// Changing channel while a stick is deflected must not leave the old channel
// bent or modulated.
func TestRedirectReturnsSelfCenteringControlsToRest(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Centered: true}, Action: PitchBend{}},
		key(keyA, ControlChange{Controller: 64}),
		Binding{Source: Source{Code: trigger}, Action: ControlChange{Controller: 7}},
		key(keyUp, CommandChannelUp),
	))
	r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
	r.press(keyA, "ControlChange ch=1 controller=64 value=127")
	r.set(trigger, 1, "ControlChange ch=1 controller=7 value=127")

	// The stick and the key return to rest. The trigger axis is not declared
	// self-centering, so it is treated like a lever and keeps its value.
	r.press(keyUp, "PitchBend ch=1 bend=+0; ControlChange ch=1 controller=64 value=0")

	// The next movement goes to the new channel.
	r.set(stickX, 0.9, "PitchBend ch=2 bend=+6553")
	r.release(keyA, "ControlChange ch=2 controller=64 value=0")
	r.set(trigger, 0.5, "ControlChange ch=2 controller=7 value=64")
}

// With RestAxes a control that stays where it is put is returned to rest as
// well, for players who want nothing left behind on the old channel.
func TestRedirectWithRestAxes(t *testing.T) {
	p := baseProfile(
		Binding{Source: Source{Code: trigger}, Action: ControlChange{Controller: 7}},
		key(keyUp, CommandChannelUp),
	)
	p.RestAxes = true
	r := newRig(t, p)
	r.set(trigger, 1, "ControlChange ch=1 controller=7 value=127")
	r.press(keyUp, "ControlChange ch=1 controller=7 value=0")
	r.set(trigger, 0.5, "ControlChange ch=2 controller=7 value=64")
}

func TestLayoutChangeRedirects(t *testing.T) {
	p := baseProfile(Binding{Source: Source{Code: stickX, Centered: true}, Action: PitchBend{}})
	p.Layouts = append(p.Layouts, Layout{Name: "second", Bindings: []Binding{
		{Source: Source{Code: stickX, Range: RangeUpper}, Action: Note{Note: 60}},
	}})
	p.Global = []Binding{key(keyUp, CommandLayoutNext)}
	r := newRig(t, p)
	r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
	r.press(keyUp, "PitchBend ch=1 bend=+0")
	r.set(stickX, 0.5, "")
	r.set(stickX, 1, "NoteOn ch=1 note=C3 velocity=100")
}

func TestReset(t *testing.T) {
	r := newRig(t, baseProfile(
		key(keyA, Note{Note: 60}),
		key(keyB, Note{Note: 62}),
		key(keyC, Note{Note: 64}, keyShift),
		Binding{Source: Source{Code: stickX, Centered: true}, Action: PitchBend{}},
		Binding{Source: Source{Code: trigger}, Action: ControlChange{Controller: 7}},
		key(keyUp, CommandOctaveUp),
		key(keyDown, CommandOctaveDown),
	))
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyB, "NoteOn ch=1 note=D3 velocity=100")
	r.set(stickX, 0, "PitchBend ch=1 bend=-8192")
	r.set(trigger, 1, "ControlChange ch=1 controller=7 value=127")
	r.press(keyUp, "")
	r.press(keyShift, "")

	// Everything returns to rest, including the lever-like trigger.
	r.reset("NoteOff ch=1 note=C3 velocity=0; NoteOff ch=1 note=D3 velocity=0; " +
		"PitchBend ch=1 bend=+0; ControlChange ch=1 controller=7 value=0")
	r.reset("")

	if got, want := r.e.State(), (State{Octave: 1, Velocity: 100, Layout: "main"}); got != want {
		t.Errorf("State after Reset = %+v, want %+v (settings kept, nothing sounding)", got, want)
	}

	// Controls still physically held do nothing when let go, and held state
	// from before the reset is forgotten.
	r.release(keyA, "")
	r.release(keyUp, "")
	r.press(keyDown, "") // no reset combination: keyUp is no longer held
	if got := r.e.State().Octave; got != 0 {
		t.Errorf("Octave = %d, want 0", got)
	}
	r.press(keyC, "") // its modifier is no longer considered held
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
}

func TestPanic(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 60, ChannelOffset: 2}), key(keyPanic, CommandPanic))
	e, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	e.Handle(control.Event{Code: keyA, Value: 1}, nil)
	out := e.Handle(control.Event{Code: keyPanic, Value: 1}, nil)

	// The current channel and the channel the note was sent on, each with
	// 128 note offs and the two channel mode messages.
	if len(out) != 2*130 {
		t.Fatalf("panic produced %d messages, want 260", len(out))
	}
	for _, ch := range []uint8{0, 2} {
		for _, want := range []midi.Message{
			midi.NoteOff(ch, 60, 0),
			midi.ControlChange(ch, midi.ControllerAllNotesOff, 0),
			midi.ControlChange(ch, midi.ControllerAllSoundOff, 0),
		} {
			if !slices.Contains(out, want) {
				t.Errorf("panic output lacks %v", want)
			}
		}
	}
	if got := e.State().Sounding; got != 0 {
		t.Errorf("Sounding after panic = %d, want 0", got)
	}
	// The note was already silenced, so releasing its key sends nothing.
	if out := e.Handle(control.Event{Code: keyA, Value: 0}, nil); len(out) != 0 {
		t.Errorf("release after panic produced %s", render(out))
	}
	if out := e.Handle(control.Event{Code: keyA, Value: 1}, nil); len(out) != 1 {
		t.Errorf("press after panic produced %s, want one note on", render(out))
	}
}

func TestHandleAppends(t *testing.T) {
	e, err := New(baseProfile(key(keyA, Note{Note: 60})))
	if err != nil {
		t.Fatal(err)
	}
	buf := []midi.Message{midi.ProgramChange(0, 1)}
	buf = e.Handle(control.Event{Code: keyA, Value: 1}, buf)
	if len(buf) != 2 || buf[0] != midi.ProgramChange(0, 1) {
		t.Errorf("Handle did not append to the buffer: %s", render(buf))
	}
}

// The engine must be deterministic: identical input, identical output.
func TestDeterministic(t *testing.T) {
	run := func() string {
		p := baseProfile(key(keyUp, CommandChannelUp))
		for i := uint16(0); i < 20; i++ {
			p.Layouts[0].Bindings = append(p.Layouts[0].Bindings, Binding{
				Source: Source{Code: control.Axis(i), Centered: true},
				Action: ControlChange{Controller: uint8(i)},
			})
		}
		e, err := New(p)
		if err != nil {
			t.Fatal(err)
		}
		var out []midi.Message
		for i := uint16(0); i < 20; i++ {
			out = e.Handle(control.Event{Code: control.Axis(19 - i), Value: 1}, out)
		}
		out = e.Handle(control.Event{Code: keyUp, Value: 1}, out)
		return render(e.Reset(out))
	}
	first := run()
	for i := 0; i < 20; i++ {
		if got := run(); got != first {
			t.Fatalf("run %d differs from the first run", i+2)
		}
	}
}

// The state names the notes that are on, each once, whatever channel they
// are on, and forgets them when they end.
func TestStateNotes(t *testing.T) {
	r := newRig(t, baseProfile(
		key(keyA, Note{Note: 60}),
		key(keyB, Note{Note: 60, ChannelOffset: 1}),
		key(keyC, Note{Note: 127}),
	))
	notes := func(want ...int) {
		t.Helper()
		if got := r.e.State().NoteNumbers(); !slices.Equal(got, want) {
			t.Errorf("notes = %v, want %v", got, want)
		}
	}
	notes()
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyB, "NoteOn ch=2 note=C3 velocity=100")
	r.press(keyC, "NoteOn ch=1 note=G8 velocity=100")
	notes(60, 127)
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")
	notes(60, 127) // still on, on the other channel
	r.release(keyB, "NoteOff ch=2 note=C3 velocity=0")
	notes(127)
	if before, after := r.e.State(), r.e.State(); before != after {
		t.Error("two states of an unchanged instrument differ")
	}
	r.reset("NoteOff ch=1 note=G8 velocity=0")
	notes()
}

func TestChord(t *testing.T) {
	r := newRig(t, baseProfile(
		key(keyA, Note{Note: 60}),
		key(keyB, Note{Note: 64}),
		key(keyC, Note{Note: 67}),
		key(keyUp, CommandChord),
		key(keyDown, Note{Note: 125}),
	))
	chord := func(want string) {
		t.Helper()
		if got := r.e.State().Chord; got != want {
			t.Errorf("Chord = %q, want %q", got, want)
		}
	}

	// A major chord is held and remembered.
	r.press(keyB, "NoteOn ch=1 note=E3 velocity=100")
	r.press(keyC, "NoteOn ch=1 note=G3 velocity=100")
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyUp, "")
	r.release(keyUp, "")
	chord("+4 +7")
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")
	r.release(keyB, "NoteOff ch=1 note=E3 velocity=0")
	r.release(keyC, "NoteOff ch=1 note=G3 velocity=0")

	// Every note now sounds with it, and its release ends all of it.
	r.press(keyB, "NoteOn ch=1 note=E3 velocity=100; NoteOn ch=1 note=G#3 velocity=100; NoteOn ch=1 note=B3 velocity=100")
	if got := r.e.State().NoteNumbers(); !slices.Equal(got, []int{64, 68, 71}) {
		t.Errorf("notes = %v", got)
	}
	r.release(keyB, "NoteOff ch=1 note=E3 velocity=0; NoteOff ch=1 note=G#3 velocity=0; NoteOff ch=1 note=B3 velocity=0")

	// Notes of the chord above the highest MIDI note are left out.
	r.press(keyDown, "NoteOn ch=1 note=F8 velocity=100")
	r.release(keyDown, "NoteOff ch=1 note=F8 velocity=0")

	// Remembering anew counts the keys that are held, not the notes the
	// old chord adds to them.
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100; NoteOn ch=1 note=E3 velocity=100; NoteOn ch=1 note=G3 velocity=100")
	r.press(keyUp, "")
	r.release(keyUp, "")
	chord("") // one key held: forgotten
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0; NoteOff ch=1 note=E3 velocity=0; NoteOff ch=1 note=G3 velocity=0")
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.release(keyA, "NoteOff ch=1 note=C3 velocity=0")

	// The chord survives a panic and turning HIDI off and on.
	r.press(keyA, "NoteOn ch=1 note=C3 velocity=100")
	r.press(keyC, "NoteOn ch=1 note=G3 velocity=100")
	r.press(keyUp, "")
	chord("+7")
	r.reset("NoteOff ch=1 note=G3 velocity=0; NoteOff ch=1 note=C3 velocity=0")
	chord("+7")
}

// In learning mode an axis stands still where it stood when the mode
// began, until it is moved by more than the threshold; back within it, it
// stands there again. A key that drives a controller always gets through.
func TestLearn(t *testing.T) {
	r := newRig(t, baseProfile(
		key(keyUp, CommandLearn),
		key(keyA, ControlChange{Controller: 64}),
		Binding{Source: Source{Code: trigger}, Action: ControlChange{Controller: 7}},
		Binding{Source: Source{Code: stickX, Centered: true}, Action: ControlChange{Controller: 10}},
	))
	learn := func(on bool, want string) {
		t.Helper()
		r.expect("SetLearning", r.e.SetLearning(on, 0.2, nil), want)
	}
	r.set(trigger, 0.25, "ControlChange ch=1 controller=7 value=32")

	// The command only asks; the mode is the owner's to turn on.
	r.press(keyUp, "")
	r.release(keyUp, "")
	if !r.e.LearnRequested() || r.e.LearnRequested() || r.e.State().Learning {
		t.Error("the command should ask once, and change nothing by itself")
	}
	learn(true, "")
	if !r.e.State().Learning {
		t.Error("Learning is not set")
	}

	r.set(trigger, 0.3, "")  // trembling
	r.set(trigger, 0.44, "") // not a fifth of the range yet
	r.set(trigger, 0.5, "ControlChange ch=1 controller=7 value=64")
	r.set(trigger, 0.75, "ControlChange ch=1 controller=7 value=95")
	r.set(trigger, 0.4, "ControlChange ch=1 controller=7 value=32") // back: where it stood
	r.set(trigger, 0.2, "")
	r.set(trigger, 0.0, "ControlChange ch=1 controller=7 value=0") // the other way out

	// An axis that says its first word during learning stands there. A
	// stick's range is twice as long, from one end to the other.
	r.set(stickX, 0.55, "ControlChange ch=1 controller=10 value=70")
	r.set(stickX, 0.7, "")
	r.set(stickX, 0.4, "")
	r.set(stickX, 0.8, "ControlChange ch=1 controller=10 value=102")
	r.set(stickX, 0.6, "ControlChange ch=1 controller=10 value=70")

	r.press(keyA, "ControlChange ch=1 controller=64 value=127")
	r.release(keyA, "ControlChange ch=1 controller=64 value=0")

	// The mode ends: every axis is where it really is.
	r.set(trigger, 0.3, "ControlChange ch=1 controller=7 value=32")
	learn(false, "ControlChange ch=1 controller=7 value=38; ControlChange ch=1 controller=10 value=76")
	if r.e.State().Learning {
		t.Error("Learning stayed set")
	}
	r.set(trigger, 0.31, "ControlChange ch=1 controller=7 value=39")
	learn(false, "") // again changes nothing
}

func TestNewCommandNames(t *testing.T) {
	for name, want := range map[string]Command{"chord": CommandChord, "learn": CommandLearn} {
		if got, err := ParseCommand(name); err != nil || got != want {
			t.Errorf("ParseCommand(%q) = %v, %v", name, got, err)
		}
	}
	if CommandChord.opposite() != 0 || CommandLearn.opposite() != 0 {
		t.Error("the new commands are taken for halves of a pair")
	}
}

func TestKeys(t *testing.T) {
	p := Profile{
		Layouts: []Layout{
			{Name: "notes", Bindings: []Binding{
				key(keyB, Note{Note: 62}),
				key(keyA, Note{Note: 60}),
				key(keyA, Note{Note: 72}, keyC), // with a modifier: not what a lone press does
				{Source: Source{Code: stickX, Range: RangeUpper}, Action: Note{Note: 1}},
			}},
			{Name: "other", Bindings: []Binding{key(keyA, CommandPanic)}},
		},
		Global:   []Binding{key(keyUp, CommandLayoutNext), key(keyA, CommandOctaveUp)},
		Defaults: Defaults{Velocity: 100, Layout: "notes"},
	}
	r := newRig(t, p)
	keys, layout, layouts := r.e.Keys()
	if layout != 0 || layouts != 2 {
		t.Errorf("layout %d of %d", layout, layouts)
	}
	// Ordered by code; the layout's binding of a key comes before the
	// global one.
	want := []KeyRole{{keyA, Note{Note: 60}}, {keyB, Note{Note: 62}}, {keyUp, CommandLayoutNext}}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %+v, want %+v", keys, want)
	}
	r.press(keyUp, "")
	keys, layout, _ = r.e.Keys()
	want = []KeyRole{{keyA, CommandPanic}, {keyUp, CommandLayoutNext}}
	if layout != 1 || !slices.Equal(keys, want) {
		t.Errorf("after the switch: layout %d, keys %+v", layout, keys)
	}
}

// A note with touch is louder the faster its control is pressed.
func TestTouchVelocity(t *testing.T) {
	p := baseProfile(
		Binding{Source: Source{Code: trigger}, Action: Note{Note: 60, Touch: true}},
		key(keyA, Note{Note: 62, Touch: true}),
		Binding{Source: Source{Code: stickX}, Action: Note{Note: 64}}, // without touch
	)
	p.Defaults.Velocity = 80
	r := newRig(t, p)
	start := time.Unix(1000, 0)
	at := func(ms int) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	set := func(code control.Code, value float64, when time.Time, want string) {
		t.Helper()
		got := render(r.e.Handle(control.Event{Code: code, Value: value, Time: when}, nil))
		if got != want {
			t.Errorf("%v = %v\n got: %s\nwant: %s", code, value, got, want)
		}
	}

	// Slowly: a second from rest to halfway.
	set(trigger, 0.1, at(0), "")
	set(trigger, 0.3, at(500), "")
	set(trigger, 0.6, at(1000), "NoteOn ch=1 note=C3 velocity=1")
	set(trigger, 0, at(1100), "NoteOff ch=1 note=C3 velocity=0")

	// Fast: two milliseconds.
	set(trigger, 0.2, at(2000), "")
	set(trigger, 0.9, at(2002), "NoteOn ch=1 note=C3 velocity=127")
	set(trigger, 0, at(2100), "NoteOff ch=1 note=C3 velocity=0")

	// In between: 25 ms is half way from 4 to 160 by doublings.
	set(trigger, 0.2, at(3000), "")
	set(trigger, 0.9, at(3025), "NoteOn ch=1 note=C3 velocity=64")
	set(trigger, 0, at(3100), "NoteOff ch=1 note=C3 velocity=0")

	// Lingering near rest does not count: the time runs from leaving it.
	set(trigger, 0.03, at(4000), "")
	set(trigger, 0.2, at(9000), "")
	set(trigger, 0.04, at(9500), "") // back to rest
	set(trigger, 0.2, at(9990), "")
	set(trigger, 0.7, at(9993), "NoteOn ch=1 note=C3 velocity=127")
	set(trigger, 0, at(10000), "NoteOff ch=1 note=C3 velocity=0")

	// From rest past the trigger point between two reports: as fast as
	// can be told.
	set(trigger, 1, at(11000), "NoteOn ch=1 note=C3 velocity=127")
	set(trigger, 0, at(11100), "NoteOff ch=1 note=C3 velocity=0")

	// A key is only on or off, and a note without touch does not care.
	set(keyA, 1, at(12000), "NoteOn ch=1 note=D3 velocity=80")
	set(keyA, 0, at(12100), "NoteOff ch=1 note=D3 velocity=0")
	set(stickX, 0.2, at(13000), "")
	set(stickX, 0.9, at(13001), "NoteOn ch=1 note=E3 velocity=80")
}

func TestTouchVelocityScale(t *testing.T) {
	last := uint8(128)
	for ms := 1; ms <= 400; ms++ {
		v := touchVelocity(time.Duration(ms) * time.Millisecond)
		if v < 1 || v > 127 || v > last {
			t.Fatalf("%d ms gives velocity %d after %d", ms, v, last)
		}
		last = v
	}
	if touchVelocity(4*time.Millisecond) != 127 || touchVelocity(160*time.Millisecond) != 1 {
		t.Error("the ends of the scale are off")
	}
}

// The two halves of one axis may drive one target: up bends up, down
// bends down. The half that rests must not undo what the other does.
func TestHalvesShareATarget(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Range: RangeUpper}, Action: PitchBend{}},
		Binding{Source: Source{Code: stickX, Range: RangeLower}, Action: PitchBend{Down: true}},
	))
	r.set(stickX, 0.5, "PitchBend ch=1 bend=+0")
	r.set(stickX, 0.75, "PitchBend ch=1 bend=+4096")
	r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
	r.set(stickX, 0.25, "PitchBend ch=1 bend=+0; PitchBend ch=1 bend=-4096")
	r.set(stickX, 0, "PitchBend ch=1 bend=-8192")
	r.set(stickX, 0.5, "PitchBend ch=1 bend=+0")
	r.reset("")
}

// One half of a stick that stops bending the pitch, because a held key
// gives it another part, leaves the bend of the other half alone.
func TestHalfLettingGoKeepsTheOtherHalfsBend(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Range: RangeUpper}, Action: PitchBend{}},
		Binding{Source: Source{Code: stickX, Range: RangeLower}, Action: PitchBend{Down: true}},
		Binding{Source: Source{Code: stickX, Range: RangeLower, Modifiers: []control.Code{keyShift}}, Action: Note{Note: 60}},
	))
	r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
	r.press(keyShift, "")
	r.set(stickX, 1, "") // still pushed right: the bend stays
	r.set(stickX, 0.75, "PitchBend ch=1 bend=+4096")
	r.set(stickX, 0.5, "PitchBend ch=1 bend=+0")
	r.set(stickX, 0, "NoteOn ch=1 note=C3 velocity=100")
}

// A stick that was pushed out during learning and let go of ends at rest,
// and what stands still does so through turning HIDI off and on.
func TestLearnAtRestAndThroughReset(t *testing.T) {
	r := newRig(t, baseProfile(
		Binding{Source: Source{Code: stickX, Centered: true}, Action: PitchBend{}},
	))
	r.set(stickX, 0.5, "PitchBend ch=1 bend=+0")
	r.expect("on", r.e.SetLearning(true, 0.2, nil), "") // a fifth
	r.set(stickX, 0.65, "")                             // 0.3 of the range of 2: within
	r.set(stickX, 1, "PitchBend ch=1 bend=+8191")
	r.set(stickX, 0.6, "PitchBend ch=1 bend=+0") // back within: at rest
	r.set(stickX, 0.5, "")
	// Turned off and on, the stick starts from where it is then.
	r.reset("")
	if !r.e.State().Learning {
		t.Error("a reset ended learning")
	}
	r.set(stickX, 0.75, "PitchBend ch=1 bend=+4096")
	r.set(stickX, 0.8, "")
	r.expect("off", r.e.SetLearning(false, 0, nil), "PitchBend ch=1 bend=+4915")
}

// A passed key sends nothing, and the engine tells which keys are passed
// in the layout that is selected.
func TestPassthrough(t *testing.T) {
	space, a, next := control.Key(57), control.Key(30), control.Key(88)
	e, err := New(Profile{
		Defaults: Defaults{Velocity: 100},
		Global:   []Binding{{Source: Source{Code: next}, Action: CommandLayoutNext}},
		Layouts: []Layout{
			{Name: "Open", Bindings: []Binding{
				{Source: Source{Code: space}, Action: Passthrough{}},
				{Source: Source{Code: a}, Action: Note{Note: 60}},
			}},
			{Name: "Closed", Bindings: []Binding{{Source: Source{Code: a}, Action: Note{Note: 60}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Passed(); len(got) != 1 || got[0] != space {
		t.Errorf("Passed() = %v", got)
	}
	if out := e.Handle(control.Event{Code: space, Value: 1}, nil); len(out) != 0 {
		t.Errorf("a passed key sent %v", out)
	}
	if out := e.Handle(control.Event{Code: space, Value: 0}, nil); len(out) != 0 {
		t.Errorf("its release sent %v", out)
	}
	e.Handle(control.Event{Code: next, Value: 1}, nil)
	if got := e.Passed(); len(got) != 0 {
		t.Errorf("Passed() in a layout without one = %v", got)
	}
}

// A control that is told to do nothing does nothing, also where a binding
// for every layout would give it something to do; and it is not among the
// keys that have a part.
func TestNothing(t *testing.T) {
	a, b, next := control.Key(30), control.Key(48), control.Key(88)
	x := control.Axis(0)
	e, err := New(Profile{
		Defaults: Defaults{Velocity: 100},
		Global: []Binding{
			{Source: Source{Code: next}, Action: CommandLayoutNext},
			{Source: Source{Code: a}, Action: Note{Note: 60}},
			{Source: Source{Code: x}, Action: ControlChange{Controller: 7}},
		},
		Layouts: []Layout{
			{Name: "Quiet", Bindings: []Binding{
				{Source: Source{Code: a}, Action: Nothing{}},
				{Source: Source{Code: x}, Action: Nothing{}},
				{Source: Source{Code: b}, Action: Note{Note: 62}},
			}},
			{Name: "Loud"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []control.Event{{Code: a, Value: 1}, {Code: a, Value: 0}, {Code: x, Value: 0.7}, {Code: x, Value: 0.2}} {
		if out := e.Handle(ev, nil); len(out) != 0 {
			t.Errorf("%v of a control that does nothing sent %v", ev, out)
		}
	}
	keys, _, _ := e.Keys()
	for _, k := range keys {
		if k.Code == a {
			t.Errorf("a key that does nothing has a part: %+v", k)
		}
	}
	if len(keys) != 2 {
		t.Errorf("keys with a part: %+v, want the note and the command", keys)
	}
	// In the other layout the bindings for every layout count again.
	e.Handle(control.Event{Code: next, Value: 1}, nil)
	e.Handle(control.Event{Code: next, Value: 0}, nil)
	if out := e.Handle(control.Event{Code: a, Value: 1}, nil); len(out) != 1 {
		t.Errorf("the key in the layout that does not silence it sent %v", out)
	}
	if out := e.Handle(control.Event{Code: x, Value: 1}, nil); len(out) != 1 {
		t.Errorf("the axis in the layout that does not silence it sent %v", out)
	}
}

// An axis as a button triggers where its binding says, and lets go a
// little below that; a key is down or not, whatever it says.
func TestThreshold(t *testing.T) {
	x, z, a := control.Axis(0), control.Axis(2), control.Key(30)
	e, err := New(Profile{
		Defaults: Defaults{Velocity: 100},
		Layouts: []Layout{{Name: "L", Bindings: []Binding{
			{Source: Source{Code: z, Threshold: 0.2}, Action: Note{Note: 60}},
			{Source: Source{Code: x, Range: RangeUpper, Threshold: 0.8}, Action: Note{Note: 62}},
			{Source: Source{Code: x, Range: RangeLower}, Action: Note{Note: 64}},
			{Source: Source{Code: a, Threshold: 0.9}, Action: Note{Note: 65}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	notes := func(ev control.Event) string {
		var b strings.Builder
		for _, m := range e.Handle(ev, nil) {
			b.WriteString(m.String() + "; ")
		}
		return b.String()
	}
	for _, step := range []struct {
		ev   control.Event
		want string
	}{
		{control.Event{Code: z, Value: 0.2}, ""}, // at the point: half the hysteresis short
		{control.Event{Code: z, Value: 0.22}, "NoteOn"},
		{control.Event{Code: z, Value: 0.2}, ""}, // back at the point: still held
		{control.Event{Code: z, Value: 0.18}, "NoteOff"},
		// The upper half of the stick at four fifths of its way, the
		// lower at the usual half.
		{control.Event{Code: x, Value: 0.85}, ""},
		{control.Event{Code: x, Value: 0.91}, "NoteOn"},
		{control.Event{Code: x, Value: 0.5}, "NoteOff"},
		{control.Event{Code: x, Value: 0.24}, "NoteOn"},
		{control.Event{Code: x, Value: 0.5}, "NoteOff"},
		{control.Event{Code: a, Value: 1}, "NoteOn"},
		{control.Event{Code: a, Value: 0}, "NoteOff"},
	} {
		got := notes(step.ev)
		if (step.want == "") != (got == "") || !strings.Contains(got, step.want) {
			t.Errorf("%v at %v: sent %q, want %q", step.ev.Code, step.ev.Value, got, step.want)
		}
	}
	if _, err := New(Profile{Layouts: []Layout{{Name: "L", Bindings: []Binding{{Source: Source{Code: z, Threshold: 1}, Action: Note{Note: 60}}}}}}); err == nil {
		t.Error("a threshold of 1 was accepted")
	}
}

// What a layout sends by the position of controls is listed in the order
// of the controls, axes first, each target once, with what was last sent.
func TestControllers(t *testing.T) {
	x, y, z, key, next := control.Axis(0), control.Axis(1), control.Axis(2), control.Key(57), control.Key(88)
	e, err := New(Profile{
		Defaults: Defaults{Velocity: 100, Channel: 2},
		Global: []Binding{
			{Source: Source{Code: next}, Action: CommandLayoutNext},
			{Source: Source{Code: key}, Action: ControlChange{Controller: 64}},
		},
		Layouts: []Layout{
			{Name: "One", Bindings: []Binding{
				{Source: Source{Code: z}, Action: ControlChange{Controller: 7, ChannelOffset: 1}},
				{Source: Source{Code: y, Centered: true}, Action: PitchBend{}},
				{Source: Source{Code: x, Range: RangeUpper}, Action: ControlChange{Controller: 20}},
				{Source: Source{Code: x, Range: RangeLower}, Action: ControlChange{Controller: 21}},
				{Source: Source{Code: control.Key(30)}, Action: Note{Note: 60}},
			}},
			{Name: "Two", Bindings: []Binding{{Source: Source{Code: z}, Action: PitchBend{}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list := func() string {
		var b strings.Builder
		for _, s := range e.Controllers() {
			name := fmt.Sprintf("cc%d", s.Controller)
			if s.PitchBend {
				name = "bend"
			}
			value := "?"
			if s.Known && s.PitchBend {
				value = fmt.Sprint(int(midi.To14(s.Value)) - 8192)
			} else if s.Known {
				value = fmt.Sprint(midi.To7(s.Value))
			}
			fmt.Fprintf(&b, "%s ch%d=%s ", name, s.Channel+1, value)
		}
		return strings.TrimSpace(b.String())
	}
	if got, want := list(), "cc20 ch3=? cc21 ch3=? bend ch3=? cc7 ch4=? cc64 ch3=?"; got != want {
		t.Errorf("before anything moved:\n %s\nwant\n %s", got, want)
	}
	e.Handle(control.Event{Code: z, Value: 1}, nil)
	e.Handle(control.Event{Code: y, Value: 0.5}, nil)
	e.Handle(control.Event{Code: x, Value: 0.75}, nil)
	e.Handle(control.Event{Code: key, Value: 1}, nil)
	if got, want := list(), "cc20 ch3=64 cc21 ch3=0 bend ch3=0 cc7 ch4=127 cc64 ch3=127"; got != want {
		t.Errorf("after moving:\n %s\nwant\n %s", got, want)
	}
	// Another layout: what it sends, and the pitch bend once.
	e.Handle(control.Event{Code: next, Value: 1}, nil)
	if got := list(); !strings.HasPrefix(got, "bend ch3=") || strings.Count(got, "bend") != 1 || !strings.Contains(got, "cc64") || strings.Contains(got, "cc7") {
		t.Errorf("in the second layout: %s", got)
	}
}

// An axis that acts as a button has to pass its trigger point by half the
// hysteresis to count as pressed and to come back behind it by as much to
// be let go. Neither end leaves the travel of the axis, so whatever the
// two are set to, the note can be played and ended.
func TestHysteresis(t *testing.T) {
	z := control.Axis(2)
	for _, tt := range []struct {
		name           string
		threshold, h   float64
		press, release float64 // where it begins and ends
		between        float64 // a place where it stays as it is
	}{
		{"as HIDI comes", 0, DefaultHysteresis, 0.51, 0.49, 0.5},
		{"a tenth", 0, 0.1, 0.55, 0.45, 0.5},
		{"the whole travel", 0, 1, 1, 0, 0.5},
		{"more than the travel has above the point", 0.9, 0.5, 1, 0.65, 0.8},
		{"more than the travel has below the point", 0.1, 0.5, 0.35, 0, 0.2},
		{"none", 0.3, 0, 0.3, 0.3, 0.3},
	} {
		e, err := New(Profile{
			Defaults: Defaults{Velocity: 100},
			Layouts: []Layout{{Name: "L", Bindings: []Binding{
				{Source: Source{Code: z, Threshold: tt.threshold}, Action: Note{Note: 60}},
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		e.SetHysteresis(tt.h)
		sent := func(v float64) int { return len(e.Handle(control.Event{Code: z, Value: v}, nil)) }
		const hair = 0.001
		if tt.press-hair > tt.release && sent(tt.press-hair) != 0 {
			t.Errorf("%s: pressed a hair before %v", tt.name, tt.press)
		}
		if sent(tt.press) != 1 {
			t.Errorf("%s: not pressed at %v", tt.name, tt.press)
		}
		if sent(tt.between) != 0 {
			t.Errorf("%s: let go at %v, between the two", tt.name, tt.between)
		}
		if tt.release+hair < tt.press && sent(tt.release+hair) != 0 {
			t.Errorf("%s: let go a hair before %v", tt.name, tt.release)
		}
		down := tt.release
		if tt.release == tt.press {
			down -= hair // with no hysteresis the point itself is pressed
		}
		if sent(down) != 1 {
			t.Errorf("%s: not let go at %v", tt.name, down)
		}
		if tt.between > tt.release && tt.between < tt.press && sent(tt.between) != 0 {
			t.Errorf("%s: pressed at %v, between the two", tt.name, tt.between)
		}
	}
	// A key is down or not, whatever the hysteresis.
	a := control.Key(30)
	e, _ := New(Profile{Layouts: []Layout{{Name: "L", Bindings: []Binding{{Source: Source{Code: a}, Action: Note{Note: 60}}}}}, Defaults: Defaults{Velocity: 100}})
	e.SetHysteresis(1)
	if len(e.Handle(control.Event{Code: a, Value: 1}, nil)) != 1 || len(e.Handle(control.Event{Code: a, Value: 0}, nil)) != 1 {
		t.Error("a key did not play with the hysteresis at its most")
	}
}

// Turned off, an instrument ends its notes, and puts back to rest what it
// is told to: the pitch bend, the controllers, both or neither. What is
// left alone is not sent again when its axis is where it was.
func TestSilence(t *testing.T) {
	x, y, a := control.Axis(0), control.Axis(1), control.Key(30)
	profile := Profile{
		Defaults: Defaults{Velocity: 100},
		Layouts: []Layout{{Name: "L", Bindings: []Binding{
			{Source: Source{Code: x}, Action: ControlChange{Controller: 7}},
			{Source: Source{Code: y, Centered: true}, Action: PitchBend{}},
			{Source: Source{Code: a}, Action: Note{Note: 60}},
		}}},
	}
	text := func(msgs []midi.Message) string {
		var parts []string
		for _, m := range msgs {
			parts = append(parts, m.String())
		}
		return strings.Join(parts, "; ")
	}
	for _, tt := range []struct {
		rest    Rest
		off, on string
	}{
		{Rest{PitchBend: true}, "NoteOff ch=1 note=C3 velocity=0; PitchBend ch=1 bend=+0", "PitchBend ch=1 bend=+8191"},
		{Rest{Controllers: true}, "NoteOff ch=1 note=C3 velocity=0; ControlChange ch=1 controller=7 value=0", "ControlChange ch=1 controller=7 value=95"},
		{Rest{}, "NoteOff ch=1 note=C3 velocity=0", ""},
		{Rest{Controllers: true, PitchBend: true}, "NoteOff ch=1 note=C3 velocity=0; ControlChange ch=1 controller=7 value=0; PitchBend ch=1 bend=+0", "ControlChange ch=1 controller=7 value=95; PitchBend ch=1 bend=+8191"},
	} {
		e, err := New(profile)
		if err != nil {
			t.Fatal(err)
		}
		e.Handle(control.Event{Code: x, Value: 0.75}, nil)
		e.Handle(control.Event{Code: y, Value: 1}, nil)
		e.Handle(control.Event{Code: a, Value: 1}, nil)
		if got := text(e.Silence(tt.rest, nil)); got != tt.off {
			t.Errorf("%+v, turned off:\n got %s\nwant %s", tt.rest, got, tt.off)
		}
		// Turned on again, the axes say where they stand.
		on := e.Handle(control.Event{Code: x, Value: 0.75}, nil)
		on = e.Handle(control.Event{Code: y, Value: 1}, on)
		if got := text(on); got != tt.on {
			t.Errorf("%+v, turned on again:\n got %s\nwant %s", tt.rest, got, tt.on)
		}
	}
}

// What the engine holds and sends can be asked control by control, for
// whoever shows it, and where each axis rests.
func TestHeldSendsAndNeutral(t *testing.T) {
	x, y, z, rz, a := control.Axis(0), control.Axis(1), control.Axis(2), control.Axis(5), control.Key(30)
	e, err := New(Profile{
		Defaults: Defaults{Velocity: 100},
		Layouts: []Layout{{Name: "L", Bindings: []Binding{
			{Source: Source{Code: z, Threshold: 0.3}, Action: Note{Note: 60}},
			{Source: Source{Code: x, Range: RangeUpper}, Action: PitchBend{}},
			{Source: Source{Code: x, Range: RangeLower}, Action: PitchBend{Down: true}},
			{Source: Source{Code: y, Centered: true}, Action: ControlChange{Controller: 7}},
			{Source: Source{Code: rz, Invert: true}, Action: ControlChange{Controller: 8}},
			{Source: Source{Code: a}, Action: Note{Note: 62}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.SetHysteresis(0.2)
	// Held follows the hysteresis: pressed a tenth past the point, let
	// go a tenth behind it.
	for _, step := range []struct {
		v    float64
		held bool
	}{{0.35, false}, {0.41, true}, {0.3, true}, {0.21, true}, {0.19, false}, {0.35, false}} {
		e.Handle(control.Event{Code: z, Value: step.v}, nil)
		if got := e.Held(z, "", RangeWhole); got != step.held {
			t.Errorf("the axis at %v: held %v, want %v", step.v, got, step.held)
		}
	}
	e.Handle(control.Event{Code: a, Value: 1}, nil)
	if !e.Held(a, "", RangeWhole) || e.Held(a, "", RangeUpper) || e.Held(control.Key(31), "", RangeWhole) {
		t.Error("Held of a key that is down, of a half of it, and of a key nobody touched")
	}

	// Sends: nothing known before anything was sent, then the value;
	// the two halves that bend the pitch send to the same place.
	if s, ok := e.Sends(y, "", RangeWhole); !ok || s.Known || s.Controller != 7 {
		t.Errorf("before the axis moved: %+v, %v", s, ok)
	}
	e.Handle(control.Event{Code: y, Value: 1}, nil)
	if s, ok := e.Sends(y, "", RangeWhole); !ok || !s.Known || midi.To7(s.Value) != 127 {
		t.Errorf("after the axis moved: %+v, %v", s, ok)
	}
	e.Handle(control.Event{Code: x, Value: 0}, nil)
	up, _ := e.Sends(x, "", RangeUpper)
	down, _ := e.Sends(x, "", RangeLower)
	if !up.PitchBend || !down.PitchBend || !up.Known || up.Value != down.Value || int(midi.To14(up.Value))-8192 != -8192 {
		t.Errorf("the halves of the pitch bend: %+v, %+v", up, down)
	}
	if _, ok := e.Sends(z, "", RangeWhole); ok {
		t.Error("an axis that plays a note is said to send a controller")
	}

	// Where the axes rest.
	want := map[control.Code]float64{x: 0.5, y: 0.5, z: 0, rz: 1}
	got := e.Neutrals()
	if len(got) != len(want) {
		t.Fatalf("Neutrals: %+v", got)
	}
	for _, n := range got {
		if n.At != want[n.Code] {
			t.Errorf("%s rests at %v, want %v", n.Code, n.At, want[n.Code])
		}
		if at, ok := e.Neutral(n.Code, ""); !ok || at != n.At {
			t.Errorf("Neutral(%s) = %v, %v", n.Code, at, ok)
		}
	}
	if _, ok := e.Neutral(control.Axis(9), ""); ok {
		t.Error("an axis nothing reads has a place to rest")
	}
}
