package midi

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// message types
	NoteOff               uint8 = 0b1000 << 4
	NoteOn                uint8 = 0b1001 << 4
	PolyphonicKeyPressure uint8 = 0b1010 << 4 // After-touch
	ControlChange         uint8 = 0b1011 << 4
	ProgramChange         uint8 = 0b1100 << 4
	ChannelPressure       uint8 = 0b1101 << 4 // After-touch
	PitchWheelChange      uint8 = 0b1110 << 4

	// ControlChange
	AllNotesOff         uint8 = 0b01111011
	AllSoundOff         uint8 = 0b01111000
	ResetAllControllers uint8 = 0b01111001
)

var intervalToString = map[int]string{
	0:  "Perfect unison",
	1:  "Minor second",
	2:  "Major second",
	3:  "Minor third",
	4:  "Major third",
	5:  "Perfect fourth",
	6:  "Tritone",
	7:  "Perfect fifth",
	8:  "Minor sixth",
	9:  "Major sixth",
	10: "Minor seventh",
	11: "Major seventh",
	12: "Perfect octave",
}

var stringToNoteRegex = regexp.MustCompile("(?P<pitch>[a-zA-Z]#?)(?P<octave>-?[0-9])")

func StringToNote(note string) (byte, error) {
	match := stringToNoteRegex.FindStringSubmatch(note)
	if match[0] == "" {
		return 0, errors.New("unsupported format, bruh")
	}

	pitch := strings.ToUpper(match[1])
	octave, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, fmt.Errorf("parsing octave failed: %w", err)
	}

	return (uint8(octave)+2)*12 + pitchToVal[pitch], nil
}

func StringToNoteUnsafe(note string) byte {
	b, _ := StringToNote(note)
	return b
}

var valToPitch = map[uint8]string{
	0: "C", 1: "C#", 2: "D", 3: "D#",
	4: "E", 5: "F", 6: "F#", 7: "G",
	8: "G#", 9: "A", 10: "A#", 11: "B",
}

var pitchToVal = map[string]uint8{
	"C": 0, "C#": 1, "D": 2, "D#": 3,
	"E": 4, "F": 5, "F#": 6, "G": 7,
	"G#": 8, "A": 9, "A#": 10, "B": 11,
}

func noteToPitch(note byte) string {
	return valToPitch[note%12]
}

func noteToOctave(note byte) int {
	return int(note/12) - 2
}

func noteToString(note byte) string {
	return fmt.Sprintf("%-2s%2d", noteToPitch(note), noteToOctave(note))
}

type Event []byte

func (e Event) String() string {
	if len(e) == 0 {
		return fmt.Sprintf("Warning: empty Midi event, it should be not emitted")
	}
	channel := e[0]&0b1111 + 1
	switch x := e[0] & 0b11110000; x {
	case NoteOff:
		return fmt.Sprintf("Note Off: %s (channel: %2d, velocity: %3d)", noteToString(e[1]), channel, e[2])
	case NoteOn:
		return fmt.Sprintf("Note On : %s (channel: %2d, velocity: %3d)", noteToString(e[1]), channel, e[2])
	case PolyphonicKeyPressure:
		return fmt.Sprintf("Polyphonic Key Pressure: %s (channel: %2d, pressure: %3d)", noteToString(e[1]), channel, e[2])
	case ControlChange:
		var value string
		if len(e) == 3 {
			value = fmt.Sprintf("%3d", e[2])
		} else {
			value = "---"
		}
		return fmt.Sprintf("Control Change: %3d, value: %s (channel: %2d)", e[1], value, channel)
	case ProgramChange:
		return fmt.Sprintf("Program Change: %3d (channel: %2d)", e[1], channel)
	case ChannelPressure:
		return fmt.Sprintf("Channel Pressure: %3d (channel: %2d)", e[1], channel)
	case PitchWheelChange:
		val := float64((int(e[2])<<7)+int(e[1])-8192) / 8192 // max value: 16383, middle value (no pitch change): 8192
		return fmt.Sprintf("Pitch Bend: %4.0f%% (channel: %2d)", val*100, channel)
	// TODO: cover the rest of possible midi events
	default:
		msg := "Oof, unexpected event format: "
		for _, v := range e {
			msg += fmt.Sprintf("0x%02x ", v)
		}
		return msg
	}
}

func NoteEvent(messageType, channel, note, velocity uint8) Event {
	return Event{messageType | channel, note, velocity}
}

func ControlChangeEvent(channel, function, value uint8) Event {
	return Event{ControlChange | channel, function, value}
}

// PitchBendEvent accepts a value in range -1.0 to 1.0
func PitchBendEvent(channel uint8, val float64) Event {
	target := int(float64((1<<14)-1) * val)  // valid 14-bit pitch-bend range
	msb := uint8((target >> 7) & 0b01111111) // filtering bit that is beyond valid pitch-bend range when val>1.0, just in case
	lsb := uint8(target & 0b01111111)        // filtering out one bit of msb, feels good man
	return Event{PitchWheelChange | channel, lsb, msb}
}
