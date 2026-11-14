package midi

import "fmt"

// Kind identifies the type of a channel voice message.
type Kind uint8

// The channel voice message kinds. Their order matches the MIDI status
// nibbles 0x8-0xE.
const (
	KindNoteOff Kind = iota + 1
	KindNoteOn
	KindPolyPressure
	KindControlChange
	KindProgramChange
	KindChannelPressure
	KindPitchBend
)

func (k Kind) String() string {
	switch k {
	case KindNoteOff:
		return "NoteOff"
	case KindNoteOn:
		return "NoteOn"
	case KindPolyPressure:
		return "PolyPressure"
	case KindControlChange:
		return "ControlChange"
	case KindProgramChange:
		return "ProgramChange"
	case KindChannelPressure:
		return "ChannelPressure"
	case KindPitchBend:
		return "PitchBend"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// status returns the MIDI status nibble for the kind, 0x8-0xE.
func (k Kind) status() uint8 { return uint8(k) + 0x7 }

// valid reports whether k is one of the defined kinds.
func (k Kind) valid() bool { return k >= KindNoteOff && k <= KindPitchBend }

// Controller numbers for the channel mode messages HIDI sends.
const (
	ControllerAllSoundOff         uint8 = 120
	ControllerResetAllControllers uint8 = 121
	ControllerAllNotesOff         uint8 = 123
)

// Message is a MIDI channel voice message. The zero value is not a valid
// message; use the constructor functions.
//
// Message is a small comparable value and is passed by value.
type Message struct {
	// Kind is the message type.
	Kind Kind
	// Channel is the MIDI channel, 0-15. Channel 0 is what users call
	// channel 1.
	Channel uint8
	// Index is the note number for note and poly pressure messages, the
	// controller number for control change, and the program number for
	// program change. It is 0-127 and unused by other kinds.
	Index uint8
	// Value is the velocity, pressure, controller value or pitch bend over
	// the full 32-bit range. Pitch bend is centered on ValueCenter. It is
	// unused by program change.
	Value uint32
}

// NoteOn returns a note on message. A velocity that would encode as zero is
// sent as the lowest audible velocity instead, because MIDI 1.0 defines a
// note on with velocity zero as a note off.
func NoteOn(channel uint8, note Note, velocity uint32) Message {
	return Message{Kind: KindNoteOn, Channel: channel, Index: uint8(note), Value: velocity}
}

// NoteOff returns a note off message with the given release velocity.
func NoteOff(channel uint8, note Note, velocity uint32) Message {
	return Message{Kind: KindNoteOff, Channel: channel, Index: uint8(note), Value: velocity}
}

// PolyPressure returns a polyphonic key pressure (per-note aftertouch) message.
func PolyPressure(channel uint8, note Note, pressure uint32) Message {
	return Message{Kind: KindPolyPressure, Channel: channel, Index: uint8(note), Value: pressure}
}

// ControlChange returns a control change message.
func ControlChange(channel, controller uint8, value uint32) Message {
	return Message{Kind: KindControlChange, Channel: channel, Index: controller, Value: value}
}

// ProgramChange returns a program change message.
func ProgramChange(channel, program uint8) Message {
	return Message{Kind: KindProgramChange, Channel: channel, Index: program}
}

// ChannelPressure returns a channel pressure (aftertouch) message.
func ChannelPressure(channel uint8, pressure uint32) Message {
	return Message{Kind: KindChannelPressure, Channel: channel, Value: pressure}
}

// PitchBend returns a pitch bend message. Use [Bipolar] to build the value
// from a -1 to 1 range; [ValueCenter] means no bend.
func PitchBend(channel uint8, value uint32) Message {
	return Message{Kind: KindPitchBend, Channel: channel, Value: value}
}

// Note returns the note of a note or poly pressure message.
func (m Message) Note() Note { return Note(m.Index & 0x7F) }

// String describes the message with the values MIDI 1.0 carries, for example
// "NoteOn ch=1 note=C3 velocity=64". Channels are shown as 1-16.
func (m Message) String() string {
	ch := m.Channel&0x0F + 1
	d1, d2 := m.data()
	switch m.Kind {
	case KindNoteOn, KindNoteOff:
		return fmt.Sprintf("%s ch=%d note=%s velocity=%d", m.Kind, ch, m.Note(), d2)
	case KindPolyPressure:
		return fmt.Sprintf("%s ch=%d note=%s pressure=%d", m.Kind, ch, m.Note(), d2)
	case KindControlChange:
		return fmt.Sprintf("%s ch=%d controller=%d value=%d", m.Kind, ch, d1, d2)
	case KindProgramChange:
		return fmt.Sprintf("%s ch=%d program=%d", m.Kind, ch, d1)
	case KindChannelPressure:
		return fmt.Sprintf("%s ch=%d pressure=%d", m.Kind, ch, d1)
	case KindPitchBend:
		return fmt.Sprintf("%s ch=%d bend=%+d", m.Kind, ch, int(To14(m.Value))-8192)
	}
	return fmt.Sprintf("%s ch=%d", m.Kind, ch)
}
