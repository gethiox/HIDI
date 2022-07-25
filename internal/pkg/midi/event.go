package midi

import (
	"fmt"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
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

	// System real-time
	TimingClock    uint8 = 0b11111000
	TimingStart    uint8 = 0b11111010
	TimingContinue uint8 = 0b11111011
	TimingStop     uint8 = 0b11111100
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

func noteToString(note byte) string {
	return fmt.Sprintf("%-2s%2d", config.NoteToPitch(note), config.NoteToOctave(note))
}

type Event []byte

func (e Event) Type() uint8 {
	if len(e) == 0 {
		return 0
	}
	if e[0]&0b11110000 != 0b11110000 && e[0]&0b10000000 != 0b00000000 {
		return e[0] & 0b11110000
	}
	return e[0]
}

// Note - make sure event type is NoteOn/NoteOff before call
func (e Event) Note() uint8 {
	if len(e) == 0 {
		return 0
	}
	return e[1]
}

func (e Event) Channel() uint8 {
	if len(e) == 0 {
		return 0
	}
	return e[0] & 0b1111
}

func (e Event) String() string {
	if len(e) == 0 {
		return fmt.Sprintf("Warning: empty Midi event, it should be not emitted")
	}

	if e[0]&0b11110000 != 0b11110000 { // classic channel-related events
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
		}
	} else {
		switch e[0] {
		case TimingClock:
			return fmt.Sprintf("Sync Clock")
		case TimingStart:
			return fmt.Sprintf("Sync Start")
		case TimingContinue:
			return fmt.Sprintf("Sync Continue")
		case TimingStop:
			return fmt.Sprintf("Sync Stop")
		}
	}
	msg := "Oof, unexpected event format: "
	for _, v := range e {
		msg += fmt.Sprintf("0x%02x ", v)
	}
	return msg

}

func NoteEvent(messageType, channel, note, velocity uint8) Event {
	return Event{messageType | channel, note, velocity}
}

func ControlChangeEvent(channel, function, value uint8) Event {
	return Event{ControlChange | channel, function, value}
}

// PitchBendEvent accepts a value in range -1.0 to 1.0
func PitchBendEvent(channel uint8, val float64) Event {
	target := int(float64((1<<14)-1) * ((val + 1.0) / 2.0)) // valid 14-bit pitch-bend range
	msb := uint8((target >> 7) & 0b01111111)                // filtering bit that is beyond valid pitch-bend range when val>1.0, just in case
	lsb := uint8(target & 0b01111111)                       // filtering out one bit of msb, feels good man
	return Event{PitchWheelChange | channel, lsb, msb}
}

// todo:
//   implement system exclusive - System Exclusive (data dump) 2nd byte= Vendor ID followed by more data bytes and ending with EOX
func ExtractEvents(d []byte) ([]Event, []byte) {
	if len(d) == 0 {
		return []Event{}, []byte{}
	}
	var data = make([]byte, len(d))
	var leftover = make([]byte, 0)
	copy(data, d)

	events := make([]Event, 0)

	start := 0
	max := len(data) - 1

	var length = 0

	for {
		if start > max {
			break
		}
		b := data[start]
		switch { // 0b10010000
		case b >= 0b10000000 && b <= 0b10111111: // note on/off, poly aftertouch, mode change,
			length = 3
		case b >= 0b11000000 && b <= 0b11011111: // program change, channel aftertouch
			length = 2
		case b >= 0b11100000 && b <= 0b11101111: // pitch-bend
			length = 3
		case b >= 0b11110000 && b <= 0b11110010: // sys exclusive, midi time frame, song position
			length = 2
		case b == 0b11110011: // song select
			length = 2
		case b >= 0b11110100: // 2x undefined, system messages
			length = 1
		default:
			panic(fmt.Sprintf("oUuUu: %v", data[start:]))
		}

		if start+length-1 > max {
			log.Info(fmt.Sprintf("end index greater than max range: %d, max: %d", start+length-1, max), logger.Warning)
			break
		}
		events = append(events, data[start:start+length])
		start += length
	}
	leftover = append(leftover, data[start:]...)
	return events, leftover
}
