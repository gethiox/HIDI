package config

import (
	"fmt"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/holoplot/go-evdev"
	"github.com/realbucksavage/openrgb-go"
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
	Learning     Action = "cc_learning"
	GyroX        Action = "gyro_x"
	GyroY        Action = "gyro_y"
	GyroZ        Action = "gyro_z"
	Exit         Action = "exit"

	AnalogPitchBend MappingType = "pitch_bend"
	AnalogCC        MappingType = "cc"
	AnalogKeySim    MappingType = "key"
	AnalogActionSim MappingType = "action"

	CollisionOff       CollisionMode = "off"       // always emit note_on/off events
	CollisionNoRepeat  CollisionMode = "no_repeat" // emit note_on on first occurrence, note_off on last release
	CollisionInterrupt CollisionMode = "interrupt" // interrupt previous occurrence with note_off event first, note_off on last release
	CollisionRetrigger CollisionMode = "retrigger" // always emit note_on, note_off on last release

	GyroHold   GyroMode = "hold"
	GyroToggle GyroMode = "toggle"
)

var SupportedActions = map[Action]bool{
	MappingUp:    true,
	MappingDown:  true,
	Mapping:      true,
	OctaveUp:     true,
	OctaveDown:   true,
	SemitoneUp:   true,
	SemitoneDown: true,
	ChannelUp:    true,
	ChannelDown:  true,
	Channel:      true,
	Multinote:    true,
	Panic:        true,
	Learning:     true,
	Exit:         true,
}

var SupportedMappingTypes = map[MappingType]bool{
	AnalogPitchBend: true,
	AnalogCC:        true,
	AnalogKeySim:    true,
	AnalogActionSim: true,
}

var SupportedGyroMappingTypes = map[MappingType]bool{
	AnalogPitchBend: true,
	AnalogCC:        true,
}

var SupportedGyroActivationTypes = map[GyroMode]bool{
	GyroHold:   true,
	GyroToggle: true,
}

var SupportedCollisionModes = map[CollisionMode]bool{
	CollisionOff:       true,
	CollisionNoRepeat:  true,
	CollisionInterrupt: true,
	CollisionRetrigger: true,
}

type Action string
type MappingType string
type CollisionMode string
type GyroMode string

type AnalogMappingCC struct {
	CC, CCNeg     byte
	FlipAxis      bool
	Bidirectional bool
}

type AnalogMappingNote struct {
	Note, NoteNeg byte
	FlipAxis      bool
	Bidirectional bool
}
type AnalogMappingAction struct {
	Action, ActionNeg Action
	FlipAxis          bool
	Bidirectional     bool
}

type Analog struct {
	MappingType       MappingType
	CC, CCNeg         byte
	Note, NoteNeg     byte
	Action, ActionNeg Action
	FlipAxis          bool
	Bidirectional     bool
}

type KeyMapping struct {
	Name   string
	Midi   map[evdev.EvCode]byte
	Analog map[evdev.EvCode]Analog
}

type Defaults struct {
	Octave, Semitone, Channel, Mapping int
}

type Colors struct {
	White, Black, C openrgb.Color
	Unavailable     openrgb.Color
	Other           openrgb.Color
	Active          openrgb.Color
	ActiveExternal  openrgb.Color
}

type OpenRGB struct {
	Colors Colors
}

type Deadzone struct {
	Deadzones map[evdev.EvCode]float64
	Default   float64
}

var axisToName = map[int]string{0: "X", 1: "Y", 2: "Z"}

type GyroDesc struct {
	Axis                int // 0-2 (x, y, z)
	Type                MappingType
	CC                  int
	ActivationMode      GyroMode
	ResetOnDeactivation bool
	FlipAxis            bool
	ValueMultiplier     float64
}

func (g GyroDesc) String() string {
	switch g.Type {
	case AnalogPitchBend:
		return fmt.Sprintf("%s (axis: %s)", g.Type, axisToName[g.Axis])
	case AnalogCC:
		return fmt.Sprintf("%s (CC: %d) (axis: %s)", g.Type, g.CC, axisToName[g.Axis])
	default:
		return "unknown"
	}
}

type Config struct {
	ID            input.InputID
	Uniq          string
	KeyMappings   []KeyMapping
	ActionMapping map[evdev.EvCode]Action
	ExitSequence  []evdev.EvCode
	Deadzone      Deadzone
	Gyro          map[evdev.EvCode][]GyroDesc
	CollisionMode CollisionMode
	Defaults      Defaults
	OpenRGB       OpenRGB
}
