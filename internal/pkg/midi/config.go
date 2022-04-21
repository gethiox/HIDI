package midi

import (
	"github.com/holoplot/go-evdev"
)

const (
	MappingUp    Action = "mapping_up"
	MappingDown  Action = "mapping_down"
	Mapping      Action = "mapping" // given with mapping number
	OctaveUp     Action = "octave_up"
	OctaveDown   Action = "octave_down"
	SemitoneUp   Action = "semitone_up"
	SemitoneDown Action = "semitone_down"
	ChannelUp    Action = "channel_up"
	ChannelDown  Action = "channel_down"
	Channel      Action = "channel"   // given with number parameter 1-16
	Multinote    Action = "multinote" // holding this button and pressing midi keys sets multinote mode
	Panic        Action = "panic"
)

const (
	AnalogPitchBend AnalogType = "pitch_bend"
	AnalogCC        AnalogType = "cc"
	AnalogKeySim    AnalogType = "key"
)

var NameToAction = map[string]Action{
	string(MappingUp):    MappingUp,
	string(MappingDown):  MappingDown,
	string(Mapping):      Mapping,
	string(OctaveUp):     OctaveUp,
	string(OctaveDown):   OctaveDown,
	string(SemitoneUp):   SemitoneUp,
	string(SemitoneDown): SemitoneDown,
	string(ChannelUp):    ChannelUp,
	string(ChannelDown):  ChannelDown,
	string(Channel):      Channel,
	string(Multinote):    Multinote,
	string(Panic):        Panic,
}

var NameToAnalogID = map[string]AnalogType{
	string(AnalogPitchBend): AnalogPitchBend,
	string(AnalogCC):        AnalogCC,
	string(AnalogKeySim):    AnalogKeySim,
}

type Action string
type AnalogType string

type Analog struct {
	id            AnalogType
	cc, ccNeg     byte
	note, noteNeg byte
	flipAxis      bool
	bidirectional bool
}

type KeyMapping struct {
	Name   string
	Midi   map[evdev.EvCode]byte
	Analog map[evdev.EvCode]Analog
}

type Config struct {
	KeyMappings     []KeyMapping
	ActionMapping   map[evdev.EvCode]Action
	AnalogDeadzones map[evdev.EvCode]float64 // 0.0 - 1.0
}
