package midi

import (
	"errors"
	"fmt"
)

// umpTypeMIDI1 is the Universal MIDI Packet message type for MIDI 1.0 channel
// voice messages, which occupy a single 32-bit word.
const umpTypeMIDI1 = 0x2

// data returns the two MIDI 1.0 data bytes of the message. Kinds with a
// single data byte return it first and leave the second zero.
func (m Message) data() (d1, d2 uint8) {
	switch m.Kind {
	case KindNoteOn:
		velocity := To7(m.Value)
		if velocity == 0 {
			velocity = 1
		}
		return m.Index & 0x7F, velocity
	case KindNoteOff, KindPolyPressure, KindControlChange:
		return m.Index & 0x7F, To7(m.Value)
	case KindProgramChange:
		return m.Index & 0x7F, 0
	case KindChannelPressure:
		return To7(m.Value), 0
	case KindPitchBend:
		v := To14(m.Value)
		return uint8(v & 0x7F), uint8(v >> 7)
	}
	return 0, 0
}

// dataLen returns how many data bytes the kind has in MIDI 1.0.
func (k Kind) dataLen() int {
	if k == KindProgramChange || k == KindChannelPressure {
		return 1
	}
	return 2
}

// AppendBytes appends the MIDI 1.0 byte encoding of the message to dst and
// returns the extended slice. The status byte is always written; running
// status is not used. An invalid message appends nothing.
func (m Message) AppendBytes(dst []byte) []byte {
	if !m.Kind.valid() {
		return dst
	}
	d1, d2 := m.data()
	dst = append(dst, m.Kind.status()<<4|m.Channel&0x0F, d1)
	if m.Kind.dataLen() == 2 {
		dst = append(dst, d2)
	}
	return dst
}

// UMP returns the message as a MIDI 1.0 channel voice Universal MIDI Packet
// addressed to the given group (0-15). An invalid message returns 0, which is
// a UMP no-op.
func (m Message) UMP(group uint8) uint32 {
	if !m.Kind.valid() {
		return 0
	}
	d1, d2 := m.data()
	return umpTypeMIDI1<<28 |
		uint32(group&0x0F)<<24 |
		uint32(m.Kind.status())<<20 |
		uint32(m.Channel&0x0F)<<16 |
		uint32(d1)<<8 |
		uint32(d2)
}

// ErrNotChannelVoice is returned by the decoders for data that is well formed
// but is not a channel voice message, such as system messages.
var ErrNotChannelVoice = errors.New("not a channel voice message")

// DecodeBytes decodes one complete MIDI 1.0 channel voice message, including
// its status byte. A note on with velocity zero is returned as a note off.
func DecodeBytes(b []byte) (Message, error) {
	if len(b) == 0 {
		return Message{}, errors.New("empty MIDI message")
	}
	status := b[0]
	if status < 0x80 {
		return Message{}, fmt.Errorf("MIDI message starts with data byte 0x%02X", status)
	}
	if status >= 0xF0 {
		return Message{}, ErrNotChannelVoice
	}
	kind := Kind(status>>4 - 0x7)
	if want := 1 + kind.dataLen(); len(b) != want {
		return Message{}, fmt.Errorf("%s message has %d bytes, want %d", kind, len(b), want)
	}
	var d1, d2 uint8 = b[1], 0
	if len(b) == 3 {
		d2 = b[2]
	}
	if d1 > 0x7F || d2 > 0x7F {
		return Message{}, fmt.Errorf("%s message has a data byte above 0x7F", kind)
	}
	return fromData(kind, status&0x0F, d1, d2), nil
}

// DecodeUMP decodes a MIDI 1.0 channel voice Universal MIDI Packet and
// returns the message and its group. Packets of any other type return
// [ErrNotChannelVoice]. A note on with velocity zero is returned as a note
// off.
func DecodeUMP(word uint32) (m Message, group uint8, err error) {
	if word>>28 != umpTypeMIDI1 {
		return Message{}, 0, ErrNotChannelVoice
	}
	status := uint8(word >> 20 & 0x0F)
	if status < 0x8 || status > 0xE {
		return Message{}, 0, fmt.Errorf("invalid channel voice status 0x%X in UMP 0x%08X", status, word)
	}
	group = uint8(word >> 24 & 0x0F)
	channel := uint8(word >> 16 & 0x0F)
	d1, d2 := uint8(word>>8&0x7F), uint8(word&0x7F)
	return fromData(Kind(status-0x7), channel, d1, d2), group, nil
}

// fromData builds a message from validated MIDI 1.0 fields.
func fromData(kind Kind, channel, d1, d2 uint8) Message {
	switch kind {
	case KindNoteOn:
		if d2 == 0 {
			return NoteOff(channel, Note(d1), 0)
		}
		return NoteOn(channel, Note(d1), From7(d2))
	case KindNoteOff:
		return NoteOff(channel, Note(d1), From7(d2))
	case KindPolyPressure:
		return PolyPressure(channel, Note(d1), From7(d2))
	case KindControlChange:
		return ControlChange(channel, d1, From7(d2))
	case KindProgramChange:
		return ProgramChange(channel, d1)
	case KindChannelPressure:
		return ChannelPressure(channel, From7(d1))
	default: // KindPitchBend
		return PitchBend(channel, From14(uint16(d2)<<7|uint16(d1)))
	}
}
