// Package engine turns the control events of one input device into MIDI
// messages. One [Engine] is one instrument.
//
// The engine is pure: it performs no I/O, starts no goroutines and reads no
// clock. Feeding it the same events always produces the same messages, which
// is what makes its behaviour testable. It is not safe for concurrent use;
// the caller serializes access.
//
// # Bindings
//
// A [Profile] describes the instrument as a list of bindings. A [Binding]
// connects a [Source], which is a control or part of an axis's travel, to an
// [Action] such as playing a note or sending a controller. Bindings are
// grouped into layouts the player switches between, plus a global group that
// applies in every layout. A binding may require other keys to be held
// ([Source.Modifiers]), which is how key combinations are expressed.
//
// Keys and axes go through the same path. A source reduces whatever its
// control reports to one number, and each action reads that number either as
// a position (controllers, pitch bend) or as pressed-or-not (notes,
// commands), with hysteresis so that an axis resting near the threshold does
// not chatter.
//
// # Press-time resolution
//
// What a press does is decided when it happens and remembered until the
// release. Changing octave, transposition, channel or layout while keys are
// held therefore never leaves a note sounding: the release turns off exactly
// the notes the press turned on.
//
// Positions follow the same idea. The engine remembers every controller and
// pitch bend value it sent. When a command redirects a self-centering control
// (a stick, a key) to another channel or layout, what it was driving is
// first returned to rest, and [Engine.Reset] returns everything to rest when
// the instrument is disabled or its device disappears.
package engine
