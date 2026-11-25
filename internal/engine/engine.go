package engine

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/midi"
)

// thresholds returns where a momentary action of the source triggers and
// where it ends: the usual ones, or those of the source's own threshold,
// with the same gap in proportion.
func (s *Source) thresholds() (press, release float64) {
	if s.Threshold > 0 && s.Code.Kind != control.KindKey {
		return s.Threshold, s.Threshold * releaseThreshold / pressThreshold
	}
	return pressThreshold, releaseThreshold
}

// Shape reduces a raw position of the control, from 0 to 1, to what the
// action of the source sees: from -1 to 1 where [Source.Bipolar] says so,
// from 0 to 1 otherwise. It is what the engine does, for whoever wants to
// show what a position would send.
func (s Source) Shape(v float64) float64 { return shape(&s, min(max(v, 0), 1)) }

// Bipolar reports whether the shaped position runs from -1 to 1: the
// whole of a control that rests in the middle.
func (s Source) Bipolar() bool { return s.bipolar() }

// Triggers reports whether a shaped position is past the point where a
// note or a command of the source triggers.
func (s Source) Triggers(x float64) bool {
	press, _ := s.thresholds()
	return x >= press
}

// A momentary action triggers when its source rises to pressThreshold and
// ends when the source falls below releaseThreshold. The gap keeps an axis
// that rests near the threshold from triggering repeatedly.
const (
	pressThreshold   = 0.5
	releaseThreshold = 0.45
)

// controlKey identifies a control on the device.
type controlKey struct {
	part string
	code control.Code
}

// sourceKey identifies one range of a control. Each has its own state, so the
// two halves of a stick axis can be bound independently.
type sourceKey struct {
	control controlKey
	rng     Range
}

// target is something a position-following action drives: one controller on
// one channel, or the pitch bend of one channel.
type target struct {
	pitchBend  bool
	channel    uint8
	controller uint8
}

// targetState is the last value sent to a target and the value it rests at.
type targetState struct {
	value, rest uint32
	// owner is the source that last moved the target away from rest, nil
	// while it rests. Two sources may drive one target, the two halves
	// of an axis that bend the pitch up and down for one; the one that
	// rests must not put the target back while the other holds it.
	owner *sourceState
}

// noteRef is a note the engine turned on.
type noteRef struct {
	channel uint8
	note    midi.Note
}

// sourceState remembers what a source is currently doing, so that it can be
// undone no matter how the instrument state changed in the meantime.
type sourceState struct {
	// pressed and latched describe a momentary action in progress: latched
	// is the binding that was resolved when the source was pressed.
	pressed bool
	latched *Binding
	// notes are the notes the press turned on.
	notes []noteRef
	// leftRest is when the source last left its rest position, for
	// measuring how fast it is pressed; zero while it rests.
	leftRest time.Time
	// travel is how long the press took from there to the trigger point,
	// zero if that was not seen.
	travel time.Duration

	// driving reports that the source last drove target with a
	// position-following action. centering records whether the source
	// returns to rest on its own.
	driving   bool
	target    target
	centering bool

	// last is the binding the source last drove with and lastX the
	// position it had then, as the device reported it: what is sent may
	// differ while the instrument is learning.
	last  *Binding
	lastX float64
	// base is where the source stood when learning began, or when it
	// first reported after that; hasBase says that there is one.
	base    float64
	hasBase bool
}

// State is a snapshot of an instrument, for display. It is comparable, so a
// caller can detect changes by comparing snapshots.
type State struct {
	// Octave and Transpose are the current note shifts.
	Octave, Transpose int
	// Channel is the current MIDI channel, 0-15.
	Channel uint8
	// Velocity is the current note velocity, 1-127.
	Velocity uint8
	// Layout is the name of the selected layout.
	Layout string
	// Sounding is the number of notes currently on.
	Sounding int
	// Chord is the remembered chord as its intervals in semitones above
	// the note played, such as "+4 +7", or empty when there is none.
	Chord string
	// Learning reports that the instrument is in learning mode: axes
	// stand still until they are moved far.
	Learning bool
	// Notes has a bit set for every note number that is on, on whatever
	// channel: bit n%64 of element n/64 for note n.
	Notes [2]uint64
}

// NoteNumbers lists the notes that are on, lowest first.
func (s State) NoteNumbers() []int {
	notes := []int{}
	for n := range 128 {
		if s.Notes[n/64]&(1<<(n%64)) != 0 {
			notes = append(notes, n)
		}
	}
	return notes
}

// Engine is one instrument. Create it with [New].
type Engine struct {
	profile Profile
	// index holds, for each layout, the bindings that apply to each control:
	// the layout's own bindings first, then the global ones.
	index []map[controlKey][]*Binding

	octave, transpose int
	channel, velocity uint8
	layout            int
	defaultLayout     int

	// states and stateOrder hold the per-source state. The slice gives
	// iteration a stable order so that output is deterministic.
	states     map[sourceKey]*sourceState
	stateOrder []*sourceState
	// targets and targetOrder do the same for everything that was driven.
	targets     map[target]*targetState
	targetOrder []target

	// down is the set of keys currently held, for modifier matching.
	down map[control.Code]bool
	// held counts the sources currently pressing each command.
	held [commandCount]uint8
	// sounding counts the sources holding each note.
	sounding      [16][128]uint8
	soundingNotes int
	// pitches counts, for each note number, the channels it is on.
	pitches  [128]uint8
	noteBits [2]uint64
	// chord holds the intervals of the remembered chord, lowest first, and
	// chordText the same for display.
	chord     []int
	chordText string
	// now is the time of the event being handled.
	now time.Time
	// usedChannels has a bit set for every channel a note was sent on.
	usedChannels uint16
	// learning says that the instrument is in learning mode, and
	// learnThreshold how far an axis has to move to get through then, as
	// a part of its whole range. learnAsked says that the learn command
	// was pressed and nobody has asked about it yet.
	learning       bool
	learnThreshold float64
	learnAsked     bool
}

// DefaultLearnThreshold is how far an axis has to be moved to get through
// in learning mode, unless it is set: half of its range.
const DefaultLearnThreshold = 0.5

// SetLearning turns learning mode on or off and sets how far an axis has
// to move in it, as a part of its whole range from 0 to 1.
//
// Learning mode is for teaching a program which controller to listen to.
// Such a program takes the first controller it hears, and an axis that
// trembles by one step, or a stick that is brushed, must not be that one.
// So when the mode begins, every axis is taken to stand where it stands,
// and what it sends stays exactly there until the axis has moved by more
// than the threshold. From then on it sends where it is. When it comes
// back within the threshold it sends the place it stood at again, and
// stands still there. Keys are not affected.
//
// When the mode ends, every axis sends where it really is.
func (e *Engine) SetLearning(on bool, threshold float64, out []midi.Message) []midi.Message {
	if !(threshold > 0 && threshold <= 1) {
		threshold = DefaultLearnThreshold
	}
	e.learnThreshold = threshold
	if on == e.learning {
		return out
	}
	e.learning = on
	for _, st := range e.stateOrder {
		switch {
		case on:
			st.base, st.hasBase = st.lastX, st.driving && st.last != nil
		case st.driving && st.last != nil:
			st.hasBase = false
			out = e.drive(st, st.last, st.lastX, out)
		}
	}
	return out
}

// LearnRequested reports whether the learn command was pressed since the
// last call. The command asks for learning mode to be turned on or off;
// whoever owns the engine does that, for all instruments at once, with
// [Engine.SetLearning].
func (e *Engine) LearnRequested() bool {
	asked := e.learnAsked
	e.learnAsked = false
	return asked
}

// New returns an engine for the profile, in the profile's default state. It
// returns an error describing every problem if the profile is invalid.
//
// The engine keeps references into the profile; the caller must not modify
// its slices afterwards.
func New(p Profile) (*Engine, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid profile: %w", err)
	}

	e := &Engine{
		profile: p,
		index:   make([]map[controlKey][]*Binding, len(p.Layouts)),
		states:  map[sourceKey]*sourceState{},
		targets: map[target]*targetState{},
		down:    map[control.Code]bool{},
	}
	for i := range p.Layouts {
		idx := map[controlKey][]*Binding{}
		add := func(bindings []Binding) {
			for j := range bindings {
				b := &bindings[j]
				key := controlKey{b.Source.Part, b.Source.Code}
				idx[key] = append(idx[key], b)
			}
		}
		add(p.Layouts[i].Bindings)
		add(p.Global)
		e.index[i] = idx
		if p.Layouts[i].Name == p.Defaults.Layout {
			e.defaultLayout = i
		}
	}

	e.octave = p.Defaults.Octave
	e.transpose = p.Defaults.Transpose
	e.channel = p.Defaults.Channel
	e.velocity = p.Defaults.Velocity
	e.layout = e.defaultLayout
	return e, nil
}

// State returns a snapshot of the instrument.
func (e *Engine) State() State {
	return State{
		Octave:    e.octave,
		Transpose: e.transpose,
		Channel:   e.channel,
		Velocity:  e.velocity,
		Layout:    e.profile.Layouts[e.layout].Name,
		Sounding:  e.soundingNotes,
		Notes:     e.noteBits,
		Chord:     e.chordText,
		Learning:  e.learning,
	}
}

// Handle processes one control event. It appends the resulting MIDI messages
// to out and returns the extended slice, so a caller can reuse one buffer.
func (e *Engine) Handle(ev control.Event, out []midi.Message) []midi.Message {
	e.now = ev.Time
	v := ev.Value
	switch {
	case !(v > 0): // also catches NaN
		v = 0
	case v > 1:
		v = 1
	}
	if ev.Code.Kind == control.KindKey {
		if v >= pressThreshold {
			e.down[ev.Code] = true
		} else {
			delete(e.down, ev.Code)
		}
	}

	// One control feeds up to three sources. Sources that are busy go first,
	// so that an axis crossing from one half to the other releases the old
	// half before it presses the new one.
	key := sourceKey{control: controlKey{ev.Part, ev.Code}}
	for _, busy := range [...]bool{true, false} {
		for key.rng = RangeWhole; key.rng <= RangeLower; key.rng++ {
			st := e.states[key]
			if (st != nil && (st.pressed || st.driving)) == busy {
				out = e.handleSource(key, st, v, out)
			}
		}
	}
	return out
}

func (e *Engine) handleSource(key sourceKey, st *sourceState, v float64, out []midi.Message) []midi.Message {
	var b *Binding
	if st != nil && st.pressed {
		b = st.latched
	} else {
		b = e.resolve(key)
	}
	if b == nil {
		if st != nil && st.driving {
			out = e.abandon(st, out)
		}
		return out
	}
	if st == nil {
		st = &sourceState{}
		e.states[key] = st
		e.stateOrder = append(e.stateOrder, st)
	}

	x := shape(&b.Source, v)
	if !b.Action.momentary() {
		return e.drive(st, b, x, out)
	}

	if st.driving { // the source followed a position under another binding
		out = e.abandon(st, out)
	}
	// Follow the way from rest to the trigger point, for notes that take
	// their velocity from how fast it is covered.
	press, release := b.Source.thresholds()
	if !st.pressed {
		switch {
		case x <= restLevel:
			st.leftRest = time.Time{}
		case st.leftRest.IsZero() && x < press:
			st.leftRest = e.now // on its way, and not there yet
		}
	}
	switch {
	case !st.pressed && x >= press:
		st.travel = 0
		switch {
		case !st.leftRest.IsZero() && !e.now.IsZero():
			st.travel = max(e.now.Sub(st.leftRest), 1)
		case st.leftRest.IsZero() && b.Source.Code.Kind == control.KindAxis:
			// From rest to the trigger point between two reports: as
			// fast as can be told. A key, which is only on or off, says
			// nothing of the kind.
			st.travel = 1
		}
		st.leftRest = time.Time{}
		st.pressed, st.latched = true, b
		out = e.press(st, out)
	case st.pressed && x < release:
		out = e.release(st, out)
	}
	return out
}

// resolve returns the binding that applies to a source right now: among the
// bindings whose modifiers are all held, the one with the most modifiers.
// Ties go to the layout's own bindings over global ones, then to the binding
// declared first.
func (e *Engine) resolve(key sourceKey) *Binding {
	var best *Binding
candidates:
	for _, b := range e.index[e.layout][key.control] {
		if b.Source.Range != key.rng {
			continue
		}
		for _, m := range b.Source.Modifiers {
			if !e.down[m] {
				continue candidates
			}
		}
		if best == nil || len(b.Source.Modifiers) > len(best.Source.Modifiers) {
			best = b
		}
	}
	return best
}

// shape reduces a raw control value (0 to 1) to what the action sees: -1 to
// 1 for a centered whole-range source, 0 to 1 for every other source.
func shape(s *Source, v float64) float64 {
	switch s.Range {
	case RangeWhole:
		if !s.Centered {
			if s.Invert {
				v = 1 - v
			}
			return deadzone(v, s.Deadzone)
		}
		x := 2*v - 1
		if s.Invert {
			x = -x
		}
		if x < 0 {
			return -deadzone(-x, s.Deadzone)
		}
		return deadzone(x, s.Deadzone)
	case RangeLower:
		v = 1 - v
	}
	if s.Invert {
		v = 1 - v
	}
	return deadzone(2*v-1, s.Deadzone)
}

// deadzone maps x from the range zone-1 onto 0-1 and everything below the
// zone to 0.
func deadzone(x, zone float64) float64 {
	if x <= zone {
		return 0
	}
	return (x - zone) / (1 - zone)
}

func (e *Engine) channelFor(offset uint8) uint8 { return (e.channel + offset) % 16 }

// press starts the momentary action latched in st.
func (e *Engine) press(st *sourceState, out []midi.Message) []midi.Message {
	switch a := st.latched.Action.(type) {
	case Note:
		return e.noteOn(st, a, out)
	case ProgramChange:
		return append(out, midi.ProgramChange(e.channelFor(a.ChannelOffset), a.Program))
	case Command:
		return e.commandPress(a, out)
	}
	return out
}

// release ends the momentary action latched in st.
func (e *Engine) release(st *sourceState, out []midi.Message) []midi.Message {
	switch a := st.latched.Action.(type) {
	case Note:
		for _, ref := range st.notes {
			out = e.noteOff(ref, out)
		}
		st.notes = st.notes[:0]
	case Command:
		if e.held[a] > 0 {
			e.held[a]--
		}
	}
	st.pressed, st.latched = false, nil
	return out
}

func (e *Engine) noteOn(st *sourceState, a Note, out []midi.Message) []midi.Message {
	n := int(a.Note) + e.octave*12 + e.transpose
	if n < 0 || n > int(midi.MaxNote) {
		return out // the press stays latched, so its release is a no-op
	}
	channel := e.channelFor(a.ChannelOffset)
	velocity := e.velocity
	if a.Velocity != 0 {
		velocity = a.Velocity
	}
	if a.Touch && st.travel > 0 {
		velocity = touchVelocity(st.travel)
	}
	// The note played comes first in st.notes; rememberChord relies on it.
	out = e.sound(st, noteRef{channel, midi.Note(n)}, velocity, out)
	for _, interval := range e.chord {
		if m := n + interval; m <= int(midi.MaxNote) {
			out = e.sound(st, noteRef{channel, midi.Note(m)}, velocity, out)
		}
	}
	return out
}

// sound turns a note on for a source, which remembers it for its release.
func (e *Engine) sound(st *sourceState, ref noteRef, velocity uint8, out []midi.Message) []midi.Message {
	count := &e.sounding[ref.channel][ref.note]
	if *count == 255 {
		return out
	}

	on := midi.NoteOn(ref.channel, ref.note, midi.From7(velocity))
	switch {
	case *count == 0 || e.profile.Collision == CollisionOff || e.profile.Collision == CollisionRetrigger:
		out = append(out, on)
	case e.profile.Collision == CollisionInterrupt:
		out = append(out, midi.NoteOff(ref.channel, ref.note, 0), on)
	}
	// CollisionNoRepeat sends nothing for a note that is already sounding.

	if *count == 0 {
		e.soundingNotes++
		e.pitches[ref.note]++
		e.noteBits[ref.note/64] |= 1 << (ref.note % 64)
	}
	*count++
	e.usedChannels |= 1 << ref.channel
	st.notes = append(st.notes, ref)
	return out
}

func (e *Engine) noteOff(ref noteRef, out []midi.Message) []midi.Message {
	count := &e.sounding[ref.channel][ref.note]
	if *count == 0 {
		return out // already silenced by a panic
	}
	*count--
	if *count == 0 {
		e.soundingNotes--
		if e.pitches[ref.note]--; e.pitches[ref.note] == 0 {
			e.noteBits[ref.note/64] &^= 1 << (ref.note % 64)
		}
	}
	if *count == 0 || e.profile.Collision == CollisionOff {
		out = append(out, midi.NoteOff(ref.channel, ref.note, 0))
	}
	return out
}

// drive sends the position x of a source to the target of its
// position-following action.
func (e *Engine) drive(st *sourceState, b *Binding, x float64, out []midi.Message) []midi.Message {
	st.last, st.lastX = b, x
	// A key that drives a controller is deliberate and always gets
	// through; so does its release.
	if e.learning && b.Source.Code.Kind == control.KindAxis {
		if !st.hasBase {
			st.base, st.hasBase = x, true // its first word since learning began
		}
		// The range of a centered source runs from -1 to 1.
		span := 1.0
		if b.Source.bipolar() {
			span = 2
		}
		if math.Abs(x-st.base) <= e.learnThreshold*span {
			x = st.base
		}
	}
	var t target
	var value, rest uint32
	switch a := b.Action.(type) {
	case ControlChange:
		t = target{channel: e.channelFor(a.ChannelOffset), controller: a.Controller}
		if b.Source.bipolar() {
			value, rest = midi.Unit((x+1)/2), midi.Unit(0.5)
		} else {
			value = midi.Unit(x)
		}
	case PitchBend:
		t = target{pitchBend: true, channel: e.channelFor(a.ChannelOffset)}
		if a.Down && !b.Source.bipolar() {
			x = -x
		}
		value, rest = midi.Bipolar(x), midi.ValueCenter
	}

	if st.driving && st.target != t {
		out = e.abandon(st, out)
	}
	st.driving, st.target, st.centering = true, t, b.Source.selfCentering()
	atRest := t.sameOnWire(value, rest)
	if ts := e.targets[t]; ts != nil && atRest {
		if o := ts.owner; o != nil && o != st && o.driving && o.target == t {
			return out // another source holds the target away from rest
		}
	}
	out = e.send(t, value, rest, out)
	switch ts := e.targets[t]; {
	case !atRest:
		ts.owner = st
	case ts.owner == st:
		ts.owner = nil
	}
	return out
}

// send emits a new value for a target unless it would encode the same as the
// last one sent.
func (e *Engine) send(t target, value, rest uint32, out []midi.Message) []midi.Message {
	ts := e.targets[t]
	if ts == nil {
		ts = &targetState{}
		e.targets[t] = ts
		e.targetOrder = append(e.targetOrder, t)
	} else if t.sameOnWire(ts.value, value) {
		ts.rest = rest
		return out
	}
	ts.value, ts.rest = value, rest
	if t.pitchBend {
		return append(out, midi.PitchBend(t.channel, value))
	}
	return append(out, midi.ControlChange(t.channel, t.controller, value))
}

// sameOnWire reports whether two values are indistinguishable at the
// resolution MIDI 1.0 carries for the target.
func (t target) sameOnWire(a, b uint32) bool {
	if t.pitchBend {
		return midi.To14(a) == midi.To14(b)
	}
	return midi.To7(a) == midi.To7(b)
}

// abandon detaches a source from the target it was driving. Pitch bend, and
// controllers driven by a self-centering source, are returned to rest; a
// controller driven by a control that stays where it is put, such as a
// throttle lever, keeps its value unless the profile asks for rest.
func (e *Engine) abandon(st *sourceState, out []midi.Message) []midi.Message {
	st.driving = false
	if st.centering || st.target.pitchBend || e.profile.RestAxes {
		out = e.rest(st.target, out)
	}
	return out
}

// rest returns a target to its rest value.
func (e *Engine) rest(t target, out []midi.Message) []midi.Message {
	if ts := e.targets[t]; ts != nil {
		out = e.send(t, ts.rest, ts.rest, out)
	}
	return out
}

func (e *Engine) commandPress(c Command, out []midi.Message) []midi.Message {
	if e.held[c] < 255 {
		e.held[c]++
	}
	if opposite := c.opposite(); opposite != 0 && e.held[opposite] > 0 {
		return e.resetPair(c, out)
	}

	switch c {
	case CommandOctaveUp:
		e.octave = min(e.octave+1, MaxOctave)
	case CommandOctaveDown:
		e.octave = max(e.octave-1, -MaxOctave)
	case CommandTransposeUp:
		e.transpose = min(e.transpose+1, MaxTranspose)
	case CommandTransposeDown:
		e.transpose = max(e.transpose-1, -MaxTranspose)
	case CommandChannelUp:
		out = e.setChannel(min(e.channel+1, 15), out)
	case CommandChannelDown:
		out = e.setChannel(max(e.channel, 1)-1, out)
	case CommandLayoutNext:
		out = e.setLayout(min(e.layout+1, len(e.profile.Layouts)-1), out)
	case CommandLayoutPrevious:
		out = e.setLayout(max(e.layout-1, 0), out)
	case CommandVelocityUp:
		e.velocity = min(e.velocity+velocityStep, 127)
	case CommandVelocityDown:
		e.velocity = max(e.velocity, velocityStep+1) - velocityStep
	case CommandPanic:
		out = e.panic(out)
	case CommandChord:
		e.rememberChord()
	case CommandLearn:
		e.learnAsked = true
	}
	return out
}

// restLevel is how far a control may be from rest and still count as
// resting, when the time of a press is measured.
const restLevel = 0.05

// A press that takes fastTouch or less plays at full velocity, one that
// takes slowTouch or more at the lowest. In between the velocity falls
// evenly with each doubling of the time, which is how loudness is heard.
const (
	fastTouch = 4 * time.Millisecond
	slowTouch = 160 * time.Millisecond
)

// touchVelocity turns the time a press took into a velocity.
func touchVelocity(travel time.Duration) uint8 {
	switch {
	case travel <= fastTouch:
		return 127
	case travel >= slowTouch:
		return 1
	}
	fraction := math.Log(float64(travel)/float64(fastTouch)) / math.Log(float64(slowTouch)/float64(fastTouch))
	return uint8(127 - fraction*126 + 0.5)
}

// rememberChord takes the chord from the notes that are held: the
// intervals of the notes played above the lowest of them. Notes that sound
// because of the chord remembered before do not count. With fewer than two
// notes held, the chord is forgotten.
func (e *Engine) rememberChord() {
	var played []int
	for _, st := range e.stateOrder {
		if !st.pressed || len(st.notes) == 0 {
			continue
		}
		if _, isNote := st.latched.Action.(Note); isNote {
			played = append(played, int(st.notes[0].note))
		}
	}
	slices.Sort(played)
	played = slices.Compact(played)
	e.chord, e.chordText = nil, ""
	if len(played) < 2 {
		return
	}
	var text []string
	for _, n := range played[1:] {
		e.chord = append(e.chord, n-played[0])
		text = append(text, fmt.Sprintf("+%d", n-played[0]))
	}
	e.chordText = strings.Join(text, " ")
}

// resetPair restores the default of the value an Up/Down pair controls.
func (e *Engine) resetPair(c Command, out []midi.Message) []midi.Message {
	switch c {
	case CommandOctaveUp, CommandOctaveDown:
		e.octave = e.profile.Defaults.Octave
	case CommandTransposeUp, CommandTransposeDown:
		e.transpose = e.profile.Defaults.Transpose
	case CommandChannelUp, CommandChannelDown:
		out = e.setChannel(e.profile.Defaults.Channel, out)
	case CommandLayoutNext, CommandLayoutPrevious:
		out = e.setLayout(e.defaultLayout, out)
	case CommandVelocityUp, CommandVelocityDown:
		e.velocity = e.profile.Defaults.Velocity
	}
	return out
}

func (e *Engine) setChannel(channel uint8, out []midi.Message) []midi.Message {
	if channel == e.channel {
		return out
	}
	out = e.redirect(out)
	e.channel = channel
	return out
}

func (e *Engine) setLayout(layout int, out []midi.Message) []midi.Message {
	if layout == e.layout {
		return out
	}
	out = e.redirect(out)
	e.layout = layout
	return out
}

// redirect detaches every source from what it is driving, ahead of a change
// that may point it somewhere else. The next event from each source attaches
// it to its new target.
func (e *Engine) redirect(out []midi.Message) []midi.Message {
	for _, st := range e.stateOrder {
		if st.driving {
			out = e.abandon(st, out)
		}
	}
	return out
}

// panic silences the instrument on every channel it has used.
func (e *Engine) panic(out []midi.Message) []midi.Message {
	// Forget held notes; their releases become no-ops.
	for _, st := range e.stateOrder {
		st.notes = st.notes[:0]
	}
	e.sounding = [16][128]uint8{}
	e.soundingNotes = 0
	e.pitches = [128]uint8{}
	e.noteBits = [2]uint64{}

	channels := e.usedChannels | 1<<e.channel
	for ch := uint8(0); ch < 16; ch++ {
		if channels&(1<<ch) == 0 {
			continue
		}
		// Some receivers ignore All Notes Off, so every note is also turned
		// off individually.
		for n := midi.Note(0); n <= midi.MaxNote; n++ {
			out = append(out, midi.NoteOff(ch, n, 0))
		}
		out = append(out,
			midi.ControlChange(ch, midi.ControllerAllNotesOff, 0),
			midi.ControlChange(ch, midi.ControllerAllSoundOff, 0),
		)
	}
	return out
}

// Reset returns the instrument to silence: it releases every held note and
// returns every controller and pitch bend it has moved to rest. Call it when
// the instrument is disabled or its device disconnects. Octave,
// transposition, channel, velocity and layout are kept.
//
// Controls that are still physically held do nothing when they are released
// afterwards.
func (e *Engine) Reset(out []midi.Message) []midi.Message {
	for _, st := range e.stateOrder {
		if st.pressed {
			out = e.release(st, out)
		}
		st.driving, st.hasBase = false, false
	}
	for _, t := range e.targetOrder {
		out = e.rest(t, out)
		e.targets[t].owner = nil
	}
	clear(e.down)
	e.held = [commandCount]uint8{}
	return out
}
