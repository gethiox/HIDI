package engine

import (
	"errors"
	"fmt"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/midi"
)

// Range selects which part of a control's travel a [Source] uses.
type Range uint8

const (
	// RangeWhole uses the whole travel. It is the only meaningful range for
	// keys and buttons.
	RangeWhole Range = iota
	// RangeUpper uses the half above the middle: a stick pushed right or
	// down, or a hat pressed in its positive direction.
	RangeUpper
	// RangeLower uses the half below the middle, growing as the control
	// moves away from the middle.
	RangeLower
)

func (r Range) String() string {
	switch r {
	case RangeWhole:
		return "whole"
	case RangeUpper:
		return "upper"
	case RangeLower:
		return "lower"
	}
	return fmt.Sprintf("Range(%d)", uint8(r))
}

// Source describes where a binding takes its input from and how the raw
// control value is shaped before the action sees it.
type Source struct {
	// Code is the control.
	Code control.Code
	// Part is the part of the device the control belongs to; see
	// [control.Event]. Empty means the main part.
	Part string
	// Range is the part of the control's travel to use.
	Range Range
	// Centered declares that the control rests in the middle of its travel,
	// as a stick axis does. It applies to RangeWhole only: the dead zone is
	// then placed around the middle, and pitch bend treats the middle as no
	// bend. The upper and lower ranges are centered by definition.
	Centered bool
	// Invert reverses the direction of the control.
	Invert bool
	// Deadzone is the fraction of travel next to the rest position that is
	// ignored, from 0 up to but not including 1. The remaining travel is
	// stretched to cover the full output range.
	Deadzone float64
	// Threshold is how far the control has to be moved, as a fraction of
	// the travel that is left after shaping, for a note, a program
	// change or a command to trigger: an axis as a button. Zero means
	// the usual half. A key, which is either down or not, triggers
	// whatever it says.
	Threshold float64
	// Modifiers are keys that must all be held for the binding to apply.
	// When several bindings match a control, the one with the most
	// modifiers wins.
	Modifiers []control.Code
}

// selfCentering reports whether the source returns to rest on its own, which
// decides whether redirecting it returns its previous target to rest.
func (s *Source) selfCentering() bool {
	return s.Code.Kind == control.KindKey || s.Centered || s.Range != RangeWhole
}

// bipolar reports whether the shaped value runs from -1 to 1 rather than
// from 0 to 1.
func (s *Source) bipolar() bool { return s.Range == RangeWhole && s.Centered }

// Action is what a binding does. The implementations are [Note],
// [ControlChange], [PitchBend], [ProgramChange], [Command] and
// [Passthrough] and [Nothing].
type Action interface {
	// momentary reports whether the action reads its source as pressed or
	// released, as opposed to following its position.
	momentary() bool
}

// Note plays a note while the source is pressed. The note is shifted by the
// instrument's current octave and transposition.
type Note struct {
	// Note is the note played at octave 0 with no transposition.
	Note midi.Note
	// ChannelOffset is added to the instrument's current channel, wrapping
	// around after channel 16.
	ChannelOffset uint8
	// Velocity is a fixed velocity for this note, 1-127. Zero uses the
	// instrument's current velocity.
	Velocity uint8
	// Touch takes the velocity from how fast the control was pressed: the
	// time it took from leaving rest to the point where the note
	// triggers. That is for controls that report how far they are
	// pressed, or how hard: a trigger, an analog key, a pad. A control
	// that is only on or off has no such time to measure, and plays with
	// the fixed velocity or the instrument's.
	Touch bool
}

// ControlChange sends the position of the source as a controller value. A
// key sends the maximum while held and zero when released.
type ControlChange struct {
	// Controller is the controller number, 0-127.
	Controller uint8
	// ChannelOffset is added to the instrument's current channel, wrapping
	// around after channel 16.
	ChannelOffset uint8
}

// PitchBend bends the pitch with the position of the source. A centered
// source bends both ways; any other source bends in one direction, from no
// bend at rest.
type PitchBend struct {
	// ChannelOffset is added to the instrument's current channel, wrapping
	// around after channel 16.
	ChannelOffset uint8
	// Down makes a one-directional source bend down instead of up. It has
	// no effect on a centered source; use [Source.Invert] there.
	Down bool
}

// ProgramChange sends a program change when the source is pressed.
type ProgramChange struct {
	// Program is the program number, 0-127.
	Program uint8
	// ChannelOffset is added to the instrument's current channel, wrapping
	// around after channel 16.
	ChannelOffset uint8
}

// Passthrough leaves a key to the rest of the system: the key plays
// nothing and types what it always types, also while HIDI is on and the
// other keys of the device do not. That is for the keys a program needs
// while it is played into, the space bar that starts and stops a
// recording for one. The engine does nothing for it; whoever holds the
// device passes the key on (see [Engine.Passed]).
type Passthrough struct{}

// PassthroughName is how a passed key is written in a profile.
const PassthroughName = "passthrough"

// Nothing is the action of a control that is to do nothing. A control
// without a binding does nothing either; Nothing says so where something
// else would otherwise give it a part: a binding that applies in every
// layout, or the rule by which a profile binds the controls it does not
// name.
type Nothing struct{}

// NothingName is how a control that does nothing is written in a profile.
const NothingName = "nothing"

// Command changes the state of the instrument when the source is pressed.
//
// The Up and Down commands of a pair reset the value to the profile's
// default when both are held at once.
type Command uint8

const (
	// CommandOctaveUp and CommandOctaveDown shift all notes by an octave.
	CommandOctaveUp Command = iota + 1
	CommandOctaveDown
	// CommandTransposeUp and CommandTransposeDown shift all notes by a
	// semitone.
	CommandTransposeUp
	CommandTransposeDown
	// CommandChannelUp and CommandChannelDown change the MIDI channel.
	CommandChannelUp
	CommandChannelDown
	// CommandLayoutNext and CommandLayoutPrevious switch between layouts.
	CommandLayoutNext
	CommandLayoutPrevious
	// CommandVelocityUp and CommandVelocityDown change the velocity of
	// notes that do not set their own, a step at a time: see
	// [Engine.SetVelocityStep].
	CommandVelocityUp
	CommandVelocityDown
	// CommandPanic silences the instrument: it releases every held note and
	// asks receivers to stop all sound on the channels the instrument used.
	CommandPanic
	// CommandChord remembers a chord. Pressed while two or more notes are
	// held, it takes their intervals above the lowest one; from then on
	// every note played sounds with those intervals above it. Pressed
	// while fewer notes are held, it forgets the chord.
	CommandChord
	// CommandLearn asks for learning mode to be turned on, or off if it
	// is on: see [Engine.SetLearning]. The mode is one for all
	// instruments, so the engine only notes the wish
	// ([Engine.LearnRequested]).
	CommandLearn
	// CommandToggle turns HIDI off and on, for all instruments. The
	// engine does nothing for it: it does not run while HIDI is off, and
	// the key has to work then. Whoever holds the device watches the key
	// or the keys of the binding itself.
	CommandToggle

	commandCount
)

// ToggleName is how the command that turns HIDI off and on is written in
// a profile.
const ToggleName = "hidi_toggle"

var commandNames = [commandCount]string{
	CommandOctaveUp: "octave_up", CommandOctaveDown: "octave_down",
	CommandTransposeUp: "transpose_up", CommandTransposeDown: "transpose_down",
	CommandChannelUp: "channel_up", CommandChannelDown: "channel_down",
	CommandLayoutNext: "layout_next", CommandLayoutPrevious: "layout_previous",
	CommandVelocityUp: "velocity_up", CommandVelocityDown: "velocity_down",
	CommandPanic: "panic", CommandChord: "chord", CommandLearn: "learn",
	CommandToggle: ToggleName,
}

// String returns the name used for the command in configuration files, such
// as "octave_up".
func (c Command) String() string {
	if c > 0 && c < commandCount {
		return commandNames[c]
	}
	return fmt.Sprintf("Command(%d)", uint8(c))
}

// ParseCommand converts a configuration name such as "octave_up" to a
// [Command].
func ParseCommand(name string) (Command, error) {
	for c := Command(1); c < commandCount; c++ {
		if commandNames[c] == name {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown command %q", name)
}

// opposite returns the other command of an Up/Down pair, or 0 for commands
// that are not part of a pair.
func (c Command) opposite() Command {
	if c == 0 || c >= CommandPanic {
		return 0
	}
	if c%2 == 1 {
		return c + 1
	}
	return c - 1
}

func (Note) momentary() bool          { return true }
func (ProgramChange) momentary() bool { return true }
func (Command) momentary() bool       { return true }
func (Passthrough) momentary() bool   { return true }
func (Nothing) momentary() bool       { return true }
func (ControlChange) momentary() bool { return false }
func (PitchBend) momentary() bool     { return false }

// Binding connects a source to an action.
type Binding struct {
	Source Source
	Action Action
}

// Layout is a named set of bindings. The player switches between layouts
// with [CommandLayoutNext] and [CommandLayoutPrevious].
type Layout struct {
	// Name identifies the layout. It must be unique within a profile.
	Name string
	// Bindings apply while the layout is selected.
	Bindings []Binding
}

// CollisionMode decides what happens when a note that is already sounding is
// played again, which occurs when two sources are bound to the same note.
type CollisionMode uint8

const (
	// CollisionInterrupt ends the sounding note and starts it again. The
	// note ends when the last source holding it is released.
	CollisionInterrupt CollisionMode = iota
	// CollisionNoRepeat ignores the second press. The note ends when the
	// last source holding it is released.
	CollisionNoRepeat
	// CollisionRetrigger sends another note on without ending the first.
	// The note ends when the last source holding it is released.
	CollisionRetrigger
	// CollisionOff does not track collisions: every press sends a note on
	// and every release a note off, so the first release ends the note for
	// all sources.
	CollisionOff
)

var collisionNames = [...]string{
	CollisionInterrupt: "interrupt", CollisionNoRepeat: "no_repeat",
	CollisionRetrigger: "retrigger", CollisionOff: "off",
}

// String returns the name used for the mode in configuration files.
func (m CollisionMode) String() string {
	if int(m) < len(collisionNames) {
		return collisionNames[m]
	}
	return fmt.Sprintf("CollisionMode(%d)", uint8(m))
}

// ParseCollisionMode converts a configuration name such as "interrupt" to a
// [CollisionMode].
func ParseCollisionMode(name string) (CollisionMode, error) {
	for m, n := range collisionNames {
		if n == name {
			return CollisionMode(m), nil
		}
	}
	return 0, fmt.Errorf("unknown collision mode %q", name)
}

// Limits of the instrument state.
const (
	// MaxOctave bounds the octave shift in both directions.
	MaxOctave = 10
	// MaxTranspose bounds the transposition in both directions.
	MaxTranspose = 127
)

// Defaults is the state an instrument starts in and the state the reset
// combinations return to.
type Defaults struct {
	// Octave is the octave shift, from -MaxOctave to MaxOctave.
	Octave int
	// Transpose is the shift in semitones, from -MaxTranspose to
	// MaxTranspose.
	Transpose int
	// Channel is the MIDI channel, 0-15.
	Channel uint8
	// Velocity is the note velocity, 1-127.
	Velocity uint8
	// Layout is the name of the layout selected at start. Empty selects the
	// first layout.
	Layout string
}

// Profile is the complete description of an instrument.
type Profile struct {
	// Layouts are the switchable binding sets. There must be at least one.
	Layouts []Layout
	// Global bindings apply in every layout. When a layout binding and a
	// global binding match equally well, the layout binding wins. The
	// profiles HIDI reads put the binding of [CommandToggle] here and
	// nothing else; every layout holds the rest of its bindings itself.
	Global []Binding
	// Defaults is the initial state.
	Defaults Defaults
	// Collision decides how a repeated note is handled.
	Collision CollisionMode
	// RestAxes decides what happens to a controller when a change of
	// channel or layout takes its control away. Pitch bend, and a
	// controller driven by a control that centres itself, always return to
	// rest. A controller driven by a control that stays where it is put,
	// such as a throttle lever, keeps its value, so that a volume does not
	// jump to zero; with RestAxes it returns to rest as well.
	RestAxes bool
}

// Validate checks the profile and returns an error describing every problem
// found, or nil. [New] calls it; it is exported so that a profile can be
// checked without building an engine.
func (p *Profile) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	d := p.Defaults
	if d.Octave < -MaxOctave || d.Octave > MaxOctave {
		fail("default octave %d is outside %d to %d", d.Octave, -MaxOctave, MaxOctave)
	}
	if d.Transpose < -MaxTranspose || d.Transpose > MaxTranspose {
		fail("default transpose %d is outside %d to %d", d.Transpose, -MaxTranspose, MaxTranspose)
	}
	if d.Channel > 15 {
		fail("default channel %d is outside 0-15", d.Channel)
	}
	if d.Velocity < 1 || d.Velocity > 127 {
		fail("default velocity %d is outside 1-127", d.Velocity)
	}
	if int(p.Collision) >= len(collisionNames) {
		fail("invalid collision mode %d", p.Collision)
	}

	if len(p.Layouts) == 0 {
		fail("profile has no layouts")
	}
	seen := map[string]bool{}
	foundDefault := d.Layout == ""
	for i := range p.Layouts {
		l := &p.Layouts[i]
		switch {
		case l.Name == "":
			fail("layout %d has no name", i+1)
		case seen[l.Name]:
			fail("layout name %q is used more than once", l.Name)
		}
		seen[l.Name] = true
		foundDefault = foundDefault || l.Name == d.Layout
		for j := range l.Bindings {
			if err := l.Bindings[j].validate(); err != nil {
				fail("layout %q, binding %d: %w", l.Name, j+1, err)
			}
		}
	}
	if !foundDefault {
		fail("default layout %q does not exist", d.Layout)
	}
	for j := range p.Global {
		if err := p.Global[j].validate(); err != nil {
			fail("global binding %d: %w", j+1, err)
		}
	}
	return errors.Join(errs...)
}

func (b *Binding) validate() error {
	s := &b.Source
	switch {
	case s.Code.IsZero():
		return errors.New("source has no control")
	case s.Range > RangeLower:
		return fmt.Errorf("%v: invalid range %d", s.Code, s.Range)
	case !(s.Deadzone >= 0 && s.Deadzone < 1):
		return fmt.Errorf("%v: dead zone %v is outside 0 to 1", s.Code, s.Deadzone)
	case s.Centered && s.Range != RangeWhole:
		return fmt.Errorf("%v: centered applies to the whole range only", s.Code)
	case !(s.Threshold >= 0 && s.Threshold < 1):
		return fmt.Errorf("%v: threshold %v is outside 0 to 1", s.Code, s.Threshold)
	}
	for _, m := range s.Modifiers {
		if m.Kind != control.KindKey {
			return fmt.Errorf("%v: modifier %v is not a key", s.Code, m)
		}
		if m == s.Code {
			return fmt.Errorf("%v: a control cannot be its own modifier", s.Code)
		}
	}

	switch a := b.Action.(type) {
	case nil:
		return fmt.Errorf("%v: binding has no action", s.Code)
	case Note:
		if a.Note > midi.MaxNote || a.ChannelOffset > 15 || a.Velocity > 127 {
			return fmt.Errorf("%v: note action %+v is out of range", s.Code, a)
		}
	case ControlChange:
		if a.Controller > 127 || a.ChannelOffset > 15 {
			return fmt.Errorf("%v: control change action %+v is out of range", s.Code, a)
		}
	case PitchBend:
		if a.ChannelOffset > 15 {
			return fmt.Errorf("%v: pitch bend action %+v is out of range", s.Code, a)
		}
	case ProgramChange:
		if a.Program > 127 || a.ChannelOffset > 15 {
			return fmt.Errorf("%v: program change action %+v is out of range", s.Code, a)
		}
	case Command:
		if a == 0 || a >= commandCount {
			return fmt.Errorf("%v: invalid command %d", s.Code, a)
		}
	case Passthrough:
		if s.Code.Kind != control.KindKey || s.Part != "" || len(s.Modifiers) != 0 {
			return fmt.Errorf("%v: only a key of the main part, pressed alone, can be passed through", s.Code)
		}
	case Nothing:
	default:
		return fmt.Errorf("%v: unsupported action type %T", s.Code, a)
	}
	return nil
}

// Commands returns every command, in the order they are declared.
func Commands() []Command {
	out := make([]Command, 0, commandCount-1)
	for c := Command(1); c < commandCount; c++ {
		out = append(out, c)
	}
	return out
}
