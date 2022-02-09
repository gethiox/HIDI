package midi

import "github.com/holoplot/go-evdev"

// //              EvCode
// var Actions map[int]func(value int32)
//
//
// type Config struct {
// 	//         EvCode
// 	EV_KEY map[string]func(value int32)
// 	EV_ABS map[string]func(value int32)
// }
//
//
// type RawConfig struct {
// 	// keyboard keys to midi notes map
// 	midiMap map[interface{}]interface{} `yaml:"midi_map"`
//
// 	//            EvCode Action
// 	functions map[string]string `yaml:"actions"`
// }

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

	AnalogPitchBend = "pitch_bend"
	AnalogCC        = "cc"
	AnalogKeySim    = "key_sim"
)

type Action string
type Analog struct {
	id       string
	cc       uint8
	flipAxis bool
}

type midiMapping struct {
	name    string
	mapping map[evdev.EvCode]byte
}

type Config struct {
	midiMappings    []midiMapping
	actionMapping   map[evdev.EvCode]Action
	analogMapping   map[evdev.EvCode]Analog
	analogDeadzones map[evdev.EvCode]float64 // 0.0 - 1.0
}

var KeyboardConfig = Config{
	midiMappings: []midiMapping{
		{
			name: "Piano", // Keyboard-like mapping, two rows (z,s,x,d,c... q,2,w,3,e...)
			mapping: map[evdev.EvCode]byte{
				evdev.KEY_Z: StringToNoteUnsafe("c0"),
				evdev.KEY_S: StringToNoteUnsafe("c#0"),
				evdev.KEY_X: StringToNoteUnsafe("d0"),
				evdev.KEY_D: StringToNoteUnsafe("d#0"),
				evdev.KEY_C: StringToNoteUnsafe("e0"),
				evdev.KEY_V: StringToNoteUnsafe("f0"),
				evdev.KEY_G: StringToNoteUnsafe("f#0"),
				evdev.KEY_B: StringToNoteUnsafe("g0"),
				evdev.KEY_H: StringToNoteUnsafe("g#0"),
				evdev.KEY_N: StringToNoteUnsafe("a0"),
				evdev.KEY_J: StringToNoteUnsafe("a#0"),
				evdev.KEY_M: StringToNoteUnsafe("b0"),

				evdev.KEY_COMMA:     StringToNoteUnsafe("c1"),
				evdev.KEY_L:         StringToNoteUnsafe("c#1"),
				evdev.KEY_DOT:       StringToNoteUnsafe("d1"),
				evdev.KEY_SEMICOLON: StringToNoteUnsafe("d#1"),
				evdev.KEY_SLASH:     StringToNoteUnsafe("e1"),

				evdev.KEY_Q: StringToNoteUnsafe("c1"),
				evdev.KEY_2: StringToNoteUnsafe("c#1"),
				evdev.KEY_W: StringToNoteUnsafe("d1"),
				evdev.KEY_3: StringToNoteUnsafe("d#1"),
				evdev.KEY_E: StringToNoteUnsafe("e1"),
				evdev.KEY_R: StringToNoteUnsafe("f1"),
				evdev.KEY_5: StringToNoteUnsafe("f#1"),
				evdev.KEY_T: StringToNoteUnsafe("g1"),
				evdev.KEY_6: StringToNoteUnsafe("g#1"),
				evdev.KEY_Y: StringToNoteUnsafe("a1"),
				evdev.KEY_7: StringToNoteUnsafe("a#1"),
				evdev.KEY_U: StringToNoteUnsafe("b1"),

				evdev.KEY_I:          StringToNoteUnsafe("c2"),
				evdev.KEY_9:          StringToNoteUnsafe("c#2"),
				evdev.KEY_O:          StringToNoteUnsafe("d2"),
				evdev.KEY_0:          StringToNoteUnsafe("d#2"),
				evdev.KEY_P:          StringToNoteUnsafe("e2"),
				evdev.KEY_LEFTBRACE:  StringToNoteUnsafe("f2"),
				evdev.KEY_EQUAL:      StringToNoteUnsafe("f#2"),
				evdev.KEY_RIGHTBRACE: StringToNoteUnsafe("g2"),
				evdev.KEY_BACKSPACE:  StringToNoteUnsafe("g#2"),
				evdev.KEY_BACKSLASH:  StringToNoteUnsafe("a2"),
			},
		},
		{
			name: "Accordion", // Accordion-like mapping
			mapping: map[evdev.EvCode]byte{
				evdev.KEY_1: StringToNoteUnsafe("b-1"),
				evdev.KEY_Q: StringToNoteUnsafe("c0"),
				evdev.KEY_A: StringToNoteUnsafe("c#0"),
				evdev.KEY_Z: StringToNoteUnsafe("d0"),

				evdev.KEY_2: StringToNoteUnsafe("d0"),
				evdev.KEY_W: StringToNoteUnsafe("d#0"),
				evdev.KEY_S: StringToNoteUnsafe("e0"),
				evdev.KEY_X: StringToNoteUnsafe("f0"),

				evdev.KEY_3: StringToNoteUnsafe("f0"),
				evdev.KEY_E: StringToNoteUnsafe("f#0"),
				evdev.KEY_D: StringToNoteUnsafe("g0"),
				evdev.KEY_C: StringToNoteUnsafe("g#0"),

				evdev.KEY_4: StringToNoteUnsafe("g#0"),
				evdev.KEY_R: StringToNoteUnsafe("a0"),
				evdev.KEY_F: StringToNoteUnsafe("a#0"),
				evdev.KEY_V: StringToNoteUnsafe("b0"),

				evdev.KEY_5: StringToNoteUnsafe("b0"),
				evdev.KEY_T: StringToNoteUnsafe("c1"),
				evdev.KEY_G: StringToNoteUnsafe("c#1"),
				evdev.KEY_B: StringToNoteUnsafe("d1"),

				evdev.KEY_6: StringToNoteUnsafe("d1"),
				evdev.KEY_Y: StringToNoteUnsafe("d#1"),
				evdev.KEY_H: StringToNoteUnsafe("e1"),
				evdev.KEY_N: StringToNoteUnsafe("f1"),

				evdev.KEY_7: StringToNoteUnsafe("f1"),
				evdev.KEY_U: StringToNoteUnsafe("f#1"),
				evdev.KEY_J: StringToNoteUnsafe("g1"),
				evdev.KEY_M: StringToNoteUnsafe("g#1"),

				evdev.KEY_8:     StringToNoteUnsafe("g#1"),
				evdev.KEY_I:     StringToNoteUnsafe("a1"),
				evdev.KEY_K:     StringToNoteUnsafe("a#1"),
				evdev.KEY_COMMA: StringToNoteUnsafe("b1"),

				evdev.KEY_9:   StringToNoteUnsafe("b1"),
				evdev.KEY_O:   StringToNoteUnsafe("c2"),
				evdev.KEY_L:   StringToNoteUnsafe("c#2"),
				evdev.KEY_DOT: StringToNoteUnsafe("d2"),

				evdev.KEY_0:         StringToNoteUnsafe("d2"),
				evdev.KEY_P:         StringToNoteUnsafe("d#2"),
				evdev.KEY_SEMICOLON: StringToNoteUnsafe("e2"),
				evdev.KEY_SLASH:     StringToNoteUnsafe("f2"),

				evdev.KEY_MINUS:      StringToNoteUnsafe("f2"),
				evdev.KEY_LEFTBRACE:  StringToNoteUnsafe("f#2"),
				evdev.KEY_APOSTROPHE: StringToNoteUnsafe("g2"),

				evdev.KEY_EQUAL:      StringToNoteUnsafe("g#2"),
				evdev.KEY_RIGHTBRACE: StringToNoteUnsafe("a2"),
				evdev.KEY_ENTER:      StringToNoteUnsafe("a#2"),

				evdev.KEY_BACKSPACE: StringToNoteUnsafe("b2"),
				evdev.KEY_BACKSLASH: StringToNoteUnsafe("c3"),
			},
		},
	},
	actionMapping: map[evdev.EvCode]Action{
		evdev.KEY_ESC: Panic,

		evdev.KEY_F1: OctaveDown,
		evdev.KEY_F2: OctaveUp,
		evdev.KEY_F3: SemitoneDown,
		evdev.KEY_F4: SemitoneUp,

		evdev.KEY_F5: MappingDown,
		evdev.KEY_F6: MappingUp,
		evdev.KEY_F7: ChannelDown,
		evdev.KEY_F8: ChannelUp,

		evdev.KEY_F9:  Multinote,
		evdev.KEY_F10: Channel,
		evdev.KEY_F11: Mapping,
	},
}

var JoystickConfig = Config{
	midiMappings: []midiMapping{
		{
			name: "Default",
			mapping: map[evdev.EvCode]byte{
				evdev.BTN_A: StringToNoteUnsafe("c0"),
				evdev.BTN_B: StringToNoteUnsafe("c#0"),
				evdev.BTN_X: StringToNoteUnsafe("d0"),
				evdev.BTN_Y: StringToNoteUnsafe("d#0"),

				evdev.BTN_THUMBL: StringToNoteUnsafe("e0"),  // Left-analog press
				evdev.BTN_THUMBR: StringToNoteUnsafe("f0"),  // Right-analog press
				evdev.BTN_TL:     StringToNoteUnsafe("f#0"), // Left bumper
				evdev.BTN_TR:     StringToNoteUnsafe("g0"),  // Right bumper
				// evdev.BTN_SELECT: StringToNoteUnsafe("g#0"),
				// evdev.BTN_START:  StringToNoteUnsafe("a0"),
				evdev.BTN_MODE: StringToNoteUnsafe("a#0"), // xbox-logo
			},
		},
	},
	analogMapping: map[evdev.EvCode]Analog{
		evdev.ABS_X:  {id: AnalogCC, cc: 0},
		evdev.ABS_Y:  {id: AnalogPitchBend, flipAxis: true},
		evdev.ABS_RX: {id: AnalogCC, cc: 1},
		evdev.ABS_RY: {id: AnalogCC, cc: 2, flipAxis: true},
		evdev.ABS_Z:  {id: AnalogCC, cc: 3},
		evdev.ABS_RZ: {id: AnalogCC, cc: 4},
		// TODO:
		// evdev.ABS_HAT0X: {id: AnalogKeySim, cc: 5},
		// evdev.ABS_HAT0Y: {id: AnalogKeySim, cc: 6, flipAxis: true},
	},
	actionMapping: map[evdev.EvCode]Action{
		evdev.BTN_SELECT: ChannelDown,
		evdev.BTN_START:  ChannelUp,
		// evdev.ABS_HAT0X:
	},
}
