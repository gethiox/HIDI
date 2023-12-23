package config

import (
	"os"
	"testing"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/holoplot/go-evdev"
	"github.com/realbucksavage/openrgb-go"
	"github.com/stretchr/testify/assert"
)

func TestParseDefaultKeyboard(t *testing.T) {
	data, err := os.ReadFile("../../../../../cmd/hidi/hidi-config/factory/keyboard/0_default.toml")
	assert.Equal(t, nil, err)

	c, err := ParseData(data)
	assert.Equal(t, nil, err)

	expectedConfig := Config{
		ID: input.InputID{
			Bus:     0,
			Vendor:  0,
			Product: 0,
			Version: 0,
		},
		KeyMappings: []KeyMapping{
			{
				Name: "Piano",
				Midi: map[evdev.EvCode]byte{
					evdev.KEY_CAPSLOCK:  StringToNoteUnsafe("a#-1"),
					evdev.KEY_LEFTSHIFT: StringToNoteUnsafe("b-1"),
					evdev.KEY_Z:         StringToNoteUnsafe("c0"),
					evdev.KEY_S:         StringToNoteUnsafe("c#0"),
					evdev.KEY_X:         StringToNoteUnsafe("d0"),
					evdev.KEY_D:         StringToNoteUnsafe("d#0"),
					evdev.KEY_C:         StringToNoteUnsafe("e0"),
					evdev.KEY_V:         StringToNoteUnsafe("f0"),
					evdev.KEY_G:         StringToNoteUnsafe("f#0"),
					evdev.KEY_B:         StringToNoteUnsafe("g0"),
					evdev.KEY_H:         StringToNoteUnsafe("g#0"),
					evdev.KEY_N:         StringToNoteUnsafe("a0"),
					evdev.KEY_J:         StringToNoteUnsafe("a#0"),
					evdev.KEY_M:         StringToNoteUnsafe("b0"),

					evdev.KEY_COMMA:      StringToNoteUnsafe("c1"),
					evdev.KEY_L:          StringToNoteUnsafe("c#1"),
					evdev.KEY_DOT:        StringToNoteUnsafe("d1"),
					evdev.KEY_SEMICOLON:  StringToNoteUnsafe("d#1"),
					evdev.KEY_SLASH:      StringToNoteUnsafe("e1"),
					evdev.KEY_RIGHTSHIFT: StringToNoteUnsafe("f1"),
					evdev.KEY_ENTER:      StringToNoteUnsafe("f#1"),

					evdev.KEY_GRAVE: StringToNoteUnsafe("a#0"),
					evdev.KEY_TAB:   StringToNoteUnsafe("b0"),
					evdev.KEY_Q:     StringToNoteUnsafe("c1"),
					evdev.KEY_2:     StringToNoteUnsafe("c#1"),
					evdev.KEY_W:     StringToNoteUnsafe("d1"),
					evdev.KEY_3:     StringToNoteUnsafe("d#1"),
					evdev.KEY_E:     StringToNoteUnsafe("e1"),
					evdev.KEY_R:     StringToNoteUnsafe("f1"),
					evdev.KEY_5:     StringToNoteUnsafe("f#1"),
					evdev.KEY_T:     StringToNoteUnsafe("g1"),
					evdev.KEY_6:     StringToNoteUnsafe("g#1"),
					evdev.KEY_Y:     StringToNoteUnsafe("a1"),
					evdev.KEY_7:     StringToNoteUnsafe("a#1"),
					evdev.KEY_U:     StringToNoteUnsafe("b1"),

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

					evdev.KEY_KP1:        StringToNoteUnsafe("c0"),
					evdev.KEY_KP2:        StringToNoteUnsafe("c#0"),
					evdev.KEY_KP3:        StringToNoteUnsafe("d0"),
					evdev.KEY_KP0:        StringToNoteUnsafe("d#0"),
					evdev.KEY_KP4:        StringToNoteUnsafe("e0"),
					evdev.KEY_KP5:        StringToNoteUnsafe("f0"),
					evdev.KEY_KP6:        StringToNoteUnsafe("f#0"),
					evdev.KEY_KPENTER:    StringToNoteUnsafe("g0"),
					evdev.KEY_KP7:        StringToNoteUnsafe("g#0"),
					evdev.KEY_KP8:        StringToNoteUnsafe("a0"),
					evdev.KEY_KP9:        StringToNoteUnsafe("a#0"),
					evdev.KEY_KPPLUS:     StringToNoteUnsafe("b0"),
					evdev.KEY_NUMLOCK:    StringToNoteUnsafe("c1"),
					evdev.KEY_KPSLASH:    StringToNoteUnsafe("c#1"),
					evdev.KEY_KPASTERISK: StringToNoteUnsafe("d1"),
					evdev.KEY_KPMINUS:    StringToNoteUnsafe("d#1"),
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name: "Chromatic",
				Midi: map[evdev.EvCode]byte{
					evdev.KEY_GRAVE:     StringToNoteUnsafe("g#-1"),
					evdev.KEY_TAB:       StringToNoteUnsafe("a-1"),
					evdev.KEY_CAPSLOCK:  StringToNoteUnsafe("a#-1"),
					evdev.KEY_LEFTSHIFT: StringToNoteUnsafe("b-1"),

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
					evdev.KEY_RIGHTSHIFT: StringToNoteUnsafe("g#2"),

					evdev.KEY_EQUAL:      StringToNoteUnsafe("g#2"),
					evdev.KEY_RIGHTBRACE: StringToNoteUnsafe("a2"),
					evdev.KEY_ENTER:      StringToNoteUnsafe("a#2"),

					evdev.KEY_BACKSPACE: StringToNoteUnsafe("b2"),
					evdev.KEY_BACKSLASH: StringToNoteUnsafe("c3"),

					evdev.KEY_KP1:        StringToNoteUnsafe("c0"),
					evdev.KEY_KP2:        StringToNoteUnsafe("c#0"),
					evdev.KEY_KP3:        StringToNoteUnsafe("d0"),
					evdev.KEY_KP0:        StringToNoteUnsafe("d#0"),
					evdev.KEY_KP4:        StringToNoteUnsafe("e0"),
					evdev.KEY_KP5:        StringToNoteUnsafe("f0"),
					evdev.KEY_KP6:        StringToNoteUnsafe("f#0"),
					evdev.KEY_KPENTER:    StringToNoteUnsafe("g0"),
					evdev.KEY_KP7:        StringToNoteUnsafe("g#0"),
					evdev.KEY_KP8:        StringToNoteUnsafe("a0"),
					evdev.KEY_KP9:        StringToNoteUnsafe("a#0"),
					evdev.KEY_KPPLUS:     StringToNoteUnsafe("b0"),
					evdev.KEY_NUMLOCK:    StringToNoteUnsafe("c1"),
					evdev.KEY_KPSLASH:    StringToNoteUnsafe("c#1"),
					evdev.KEY_KPASTERISK: StringToNoteUnsafe("d1"),
					evdev.KEY_KPMINUS:    StringToNoteUnsafe("d#1"),
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name: "Control",
				Midi: map[evdev.EvCode]byte{
					evdev.KEY_1:          0,
					evdev.KEY_Q:          1,
					evdev.KEY_A:          2,
					evdev.KEY_Z:          3,
					evdev.KEY_2:          4,
					evdev.KEY_W:          5,
					evdev.KEY_S:          6,
					evdev.KEY_X:          7,
					evdev.KEY_3:          8,
					evdev.KEY_E:          9,
					evdev.KEY_D:          10,
					evdev.KEY_C:          11,
					evdev.KEY_4:          12,
					evdev.KEY_R:          13,
					evdev.KEY_F:          14,
					evdev.KEY_V:          15,
					evdev.KEY_5:          16,
					evdev.KEY_T:          17,
					evdev.KEY_G:          18,
					evdev.KEY_B:          19,
					evdev.KEY_6:          20,
					evdev.KEY_Y:          21,
					evdev.KEY_H:          22,
					evdev.KEY_N:          23,
					evdev.KEY_7:          24,
					evdev.KEY_U:          25,
					evdev.KEY_J:          26,
					evdev.KEY_M:          27,
					evdev.KEY_8:          28,
					evdev.KEY_I:          29,
					evdev.KEY_K:          30,
					evdev.KEY_COMMA:      31,
					evdev.KEY_9:          32,
					evdev.KEY_O:          33,
					evdev.KEY_L:          34,
					evdev.KEY_DOT:        35,
					evdev.KEY_0:          36,
					evdev.KEY_P:          37,
					evdev.KEY_SEMICOLON:  38,
					evdev.KEY_SLASH:      39,
					evdev.KEY_MINUS:      40,
					evdev.KEY_LEFTBRACE:  41,
					evdev.KEY_APOSTROPHE: 42,
					evdev.KEY_EQUAL:      43,
					evdev.KEY_RIGHTBRACE: 44,
					evdev.KEY_ENTER:      45,
					evdev.KEY_BACKSPACE:  46,
					evdev.KEY_BACKSLASH:  47,
					evdev.KEY_GRAVE:      48,
					evdev.KEY_TAB:        49,
					evdev.KEY_CAPSLOCK:   50,
					evdev.KEY_LEFTSHIFT:  51,
					evdev.KEY_LEFTCTRL:   52,
					evdev.KEY_LEFTMETA:   53,
					evdev.KEY_LEFTALT:    54,
					evdev.KEY_SPACE:      55,
					evdev.KEY_RIGHTALT:   96,
					evdev.KEY_RIGHTMETA:  56,
					evdev.KEY_COMPOSE:    57,
					evdev.KEY_RIGHTCTRL:  58,
					evdev.KEY_RIGHTSHIFT: 59,

					evdev.KEY_UP:    60,
					evdev.KEY_DOWN:  61,
					evdev.KEY_LEFT:  62,
					evdev.KEY_RIGHT: 63,

					evdev.KEY_INSERT:   64,
					evdev.KEY_DELETE:   65,
					evdev.KEY_HOME:     66,
					evdev.KEY_END:      67,
					evdev.KEY_PAGEUP:   68,
					evdev.KEY_PAGEDOWN: 69,

					evdev.KEY_SYSRQ:      70,
					evdev.KEY_SCROLLLOCK: 71,
					evdev.KEY_PAUSE:      72,

					evdev.KEY_PREVIOUSSONG: 73,
					evdev.KEY_PLAYPAUSE:    74,
					evdev.KEY_NEXTSONG:     75,
					evdev.KEY_MUTE:         76,
					evdev.KEY_VOLUMEUP:     77,
					evdev.KEY_VOLUMEDOWN:   78,

					evdev.KEY_NUMLOCK:    79,
					evdev.KEY_KPSLASH:    80,
					evdev.KEY_KPASTERISK: 81,
					evdev.KEY_KPMINUS:    82,
					evdev.KEY_KP7:        83,
					evdev.KEY_KP8:        84,
					evdev.KEY_KP9:        85,
					evdev.KEY_KPPLUS:     86,
					evdev.KEY_KP4:        87,
					evdev.KEY_KP5:        88,
					evdev.KEY_KP6:        89,
					evdev.KEY_KP1:        90,
					evdev.KEY_KP2:        91,
					evdev.KEY_KP3:        92,
					evdev.KEY_KPENTER:    93,
					evdev.KEY_KP0:        94,
					evdev.KEY_KPDOT:      95,
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name:   "Debug",
				Midi:   map[evdev.EvCode]byte{},
				Analog: map[evdev.EvCode]Analog{},
			},
		},
		ActionMapping: map[evdev.EvCode]Action{
			evdev.KEY_ESC: "panic",

			evdev.KEY_F1: "octave_down",
			evdev.KEY_F2: "octave_up",
			evdev.KEY_F3: "semitone_down",
			evdev.KEY_F4: "semitone_up",
			evdev.KEY_F5: "channel_down",
			evdev.KEY_F6: "channel_up",

			evdev.KEY_F11: "mapping_down",
			evdev.KEY_F12: "mapping_up",
		},
		ExitSequence:  []evdev.EvCode{evdev.KEY_LEFTALT, evdev.KEY_ESC},
		Deadzone:      Deadzone{Deadzones: map[evdev.EvCode]float64{}},
		CollisionMode: CollisionInterrupt,
		Defaults: Defaults{
			Octave:   0,
			Semitone: 0,
			Channel:  1,
			Mapping:  0,
		},
		OpenRGB: OpenRGB{
			Colors: Colors{
				White:          openrgb.Color{Red: 0x00, Green: 0x55, Blue: 0x00},
				Black:          openrgb.Color{Red: 0x00, Green: 0x00, Blue: 0x55},
				C:              openrgb.Color{Red: 0x55, Green: 0x55, Blue: 0x00},
				Unavailable:    openrgb.Color{Red: 0x44, Green: 0x00, Blue: 0x00},
				Other:          openrgb.Color{Red: 0x44, Green: 0x00, Blue: 0x00},
				Active:         openrgb.Color{Red: 0xff, Green: 0xff, Blue: 0xff},
				ActiveExternal: openrgb.Color{Red: 0xff, Green: 0xff, Blue: 0xff},
			},
		},
		Gyro: map[evdev.EvCode][]GyroDesc{
			evdev.KEY_LEFTALT: {
				{
					Type:                "cc",
					CC:                  1,
					Axis:                1,
					ActivationMode:      "toggle",
					ResetOnDeactivation: false,
					FlipAxis:            true,
					ValueMultiplier:     0.2,
				},
			},
			evdev.KEY_SPACE: {
				{
					Type:                "pitch_bend",
					Axis:                2,
					ActivationMode:      "hold",
					ResetOnDeactivation: true,
					FlipAxis:            false,
					ValueMultiplier:     0.2,
				},
			},
		},
	}

	assert.Equal(t, expectedConfig, c)
}

func TestParseDefaultGamepad(t *testing.T) {
	data, err := os.ReadFile("../../../../../cmd/hidi/hidi-config/factory/gamepad/0_default.toml")
	assert.Equal(t, nil, err)

	c, err := ParseData(data)
	assert.Equal(t, nil, err)

	expectedConfig := Config{
		ID: input.InputID{
			Bus:     0,
			Vendor:  0,
			Product: 0,
			Version: 0,
		},
		KeyMappings: []KeyMapping{
			{
				Name: "Default",
				Midi: map[evdev.EvCode]byte{
					evdev.BTN_A:      0,
					evdev.BTN_B:      1,
					evdev.BTN_X:      2,
					evdev.BTN_Y:      3,
					evdev.BTN_C:      4,
					evdev.BTN_Z:      6,
					evdev.BTN_TL2:    7,
					evdev.BTN_TR2:    8,
					evdev.BTN_THUMBL: 9,
					evdev.BTN_THUMBR: 10,
					evdev.BTN_TL:     11,
					evdev.BTN_TR:     12,
				},
				Analog: map[evdev.EvCode]Analog{
					evdev.ABS_X:     {MappingType: AnalogCC, CC: 0},
					evdev.ABS_Y:     {MappingType: AnalogPitchBend, FlipAxis: true},
					evdev.ABS_RX:    {MappingType: AnalogCC, CC: 1, CCNeg: 2, Bidirectional: true},
					evdev.ABS_RY:    {MappingType: AnalogCC, CC: 3, CCNeg: 4, FlipAxis: true, Bidirectional: true},
					evdev.ABS_Z:     {MappingType: AnalogCC, CC: 5},
					evdev.ABS_RZ:    {MappingType: AnalogCC, CC: 6},
					evdev.ABS_HAT0X: {MappingType: AnalogActionSim, Action: OctaveUp, ActionNeg: OctaveDown, Bidirectional: true},
					evdev.ABS_HAT0Y: {MappingType: AnalogActionSim, Action: MappingUp, ActionNeg: MappingDown, FlipAxis: true, Bidirectional: true},
				},
			},
		},
		ActionMapping: map[evdev.EvCode]Action{
			evdev.BTN_SELECT: ChannelDown,
			evdev.BTN_START:  ChannelUp,
			evdev.BTN_MODE:   Panic,
			evdev.BTN_TL:     Learning,
		},
		ExitSequence: nil,
		Deadzone: Deadzone{
			Default: 0.1,
			Deadzones: map[evdev.EvCode]float64{
				evdev.ABS_Z:     0.0,
				evdev.ABS_RZ:    0.0,
				evdev.ABS_HAT0X: 0.0,
				evdev.ABS_HAT0Y: 0.0,
			},
		},
		CollisionMode: CollisionInterrupt,
		Defaults: Defaults{
			Octave:   0,
			Semitone: 0,
			Channel:  1,
			Mapping:  0,
		},
		OpenRGB: OpenRGB{
			Colors: Colors{
				White:          openrgb.Color{},
				Black:          openrgb.Color{},
				C:              openrgb.Color{},
				Unavailable:    openrgb.Color{},
				Other:          openrgb.Color{},
				Active:         openrgb.Color{},
				ActiveExternal: openrgb.Color{},
			},
		},
		Gyro: map[evdev.EvCode][]GyroDesc{},
	}

	assert.Equal(t, expectedConfig, c)
}
