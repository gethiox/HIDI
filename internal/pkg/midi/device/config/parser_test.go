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
				Midi: map[evdev.EvCode]Key{
					evdev.KEY_CAPSLOCK:  {StringToNoteUnsafe("a#-1"), 0},
					evdev.KEY_LEFTSHIFT: {StringToNoteUnsafe("b-1"), 0},
					evdev.KEY_Z:         {StringToNoteUnsafe("c0"), 0},
					evdev.KEY_S:         {StringToNoteUnsafe("c#0"), 0},
					evdev.KEY_X:         {StringToNoteUnsafe("d0"), 0},
					evdev.KEY_D:         {StringToNoteUnsafe("d#0"), 0},
					evdev.KEY_C:         {StringToNoteUnsafe("e0"), 0},
					evdev.KEY_V:         {StringToNoteUnsafe("f0"), 0},
					evdev.KEY_G:         {StringToNoteUnsafe("f#0"), 0},
					evdev.KEY_B:         {StringToNoteUnsafe("g0"), 0},
					evdev.KEY_H:         {StringToNoteUnsafe("g#0"), 0},
					evdev.KEY_N:         {StringToNoteUnsafe("a0"), 0},
					evdev.KEY_J:         {StringToNoteUnsafe("a#0"), 0},
					evdev.KEY_M:         {StringToNoteUnsafe("b0"), 0},

					evdev.KEY_COMMA:      {StringToNoteUnsafe("c1"), 0},
					evdev.KEY_L:          {StringToNoteUnsafe("c#1"), 0},
					evdev.KEY_DOT:        {StringToNoteUnsafe("d1"), 0},
					evdev.KEY_SEMICOLON:  {StringToNoteUnsafe("d#1"), 0},
					evdev.KEY_SLASH:      {StringToNoteUnsafe("e1"), 0},
					evdev.KEY_RIGHTSHIFT: {StringToNoteUnsafe("f1"), 0},
					evdev.KEY_ENTER:      {StringToNoteUnsafe("f#1"), 0},

					evdev.KEY_GRAVE: {StringToNoteUnsafe("a#0"), 0},
					evdev.KEY_TAB:   {StringToNoteUnsafe("b0"), 0},
					evdev.KEY_Q:     {StringToNoteUnsafe("c1"), 0},
					evdev.KEY_2:     {StringToNoteUnsafe("c#1"), 0},
					evdev.KEY_W:     {StringToNoteUnsafe("d1"), 0},
					evdev.KEY_3:     {StringToNoteUnsafe("d#1"), 0},
					evdev.KEY_E:     {StringToNoteUnsafe("e1"), 0},
					evdev.KEY_R:     {StringToNoteUnsafe("f1"), 0},
					evdev.KEY_5:     {StringToNoteUnsafe("f#1"), 0},
					evdev.KEY_T:     {StringToNoteUnsafe("g1"), 0},
					evdev.KEY_6:     {StringToNoteUnsafe("g#1"), 0},
					evdev.KEY_Y:     {StringToNoteUnsafe("a1"), 0},
					evdev.KEY_7:     {StringToNoteUnsafe("a#1"), 0},
					evdev.KEY_U:     {StringToNoteUnsafe("b1"), 0},

					evdev.KEY_I:          {StringToNoteUnsafe("c2"), 0},
					evdev.KEY_9:          {StringToNoteUnsafe("c#2"), 0},
					evdev.KEY_O:          {StringToNoteUnsafe("d2"), 0},
					evdev.KEY_0:          {StringToNoteUnsafe("d#2"), 0},
					evdev.KEY_P:          {StringToNoteUnsafe("e2"), 0},
					evdev.KEY_LEFTBRACE:  {StringToNoteUnsafe("f2"), 0},
					evdev.KEY_EQUAL:      {StringToNoteUnsafe("f#2"), 0},
					evdev.KEY_RIGHTBRACE: {StringToNoteUnsafe("g2"), 0},
					evdev.KEY_BACKSPACE:  {StringToNoteUnsafe("g#2"), 0},
					evdev.KEY_BACKSLASH:  {StringToNoteUnsafe("a2"), 0},

					evdev.KEY_KP1:        {StringToNoteUnsafe("c0"), 0},
					evdev.KEY_KP2:        {StringToNoteUnsafe("c#0"), 0},
					evdev.KEY_KP3:        {StringToNoteUnsafe("d0"), 0},
					evdev.KEY_KP0:        {StringToNoteUnsafe("d#0"), 0},
					evdev.KEY_KP4:        {StringToNoteUnsafe("e0"), 0},
					evdev.KEY_KP5:        {StringToNoteUnsafe("f0"), 0},
					evdev.KEY_KP6:        {StringToNoteUnsafe("f#0"), 0},
					evdev.KEY_KPENTER:    {StringToNoteUnsafe("g0"), 0},
					evdev.KEY_KP7:        {StringToNoteUnsafe("g#0"), 0},
					evdev.KEY_KP8:        {StringToNoteUnsafe("a0"), 0},
					evdev.KEY_KP9:        {StringToNoteUnsafe("a#0"), 0},
					evdev.KEY_KPPLUS:     {StringToNoteUnsafe("b0"), 0},
					evdev.KEY_NUMLOCK:    {StringToNoteUnsafe("c1"), 0},
					evdev.KEY_KPSLASH:    {StringToNoteUnsafe("c#1"), 0},
					evdev.KEY_KPASTERISK: {StringToNoteUnsafe("d1"), 0},
					evdev.KEY_KPMINUS:    {StringToNoteUnsafe("d#1"), 0},
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name: "Chromatic",
				Midi: map[evdev.EvCode]Key{
					evdev.KEY_GRAVE:     {StringToNoteUnsafe("g#-1"), 0},
					evdev.KEY_TAB:       {StringToNoteUnsafe("a-1"), 0},
					evdev.KEY_CAPSLOCK:  {StringToNoteUnsafe("a#-1"), 0},
					evdev.KEY_LEFTSHIFT: {StringToNoteUnsafe("b-1"), 0},

					evdev.KEY_1: {StringToNoteUnsafe("b-1"), 0},
					evdev.KEY_Q: {StringToNoteUnsafe("c0"), 0},
					evdev.KEY_A: {StringToNoteUnsafe("c#0"), 0},
					evdev.KEY_Z: {StringToNoteUnsafe("d0"), 0},

					evdev.KEY_2: {StringToNoteUnsafe("d0"), 0},
					evdev.KEY_W: {StringToNoteUnsafe("d#0"), 0},
					evdev.KEY_S: {StringToNoteUnsafe("e0"), 0},
					evdev.KEY_X: {StringToNoteUnsafe("f0"), 0},

					evdev.KEY_3: {StringToNoteUnsafe("f0"), 0},
					evdev.KEY_E: {StringToNoteUnsafe("f#0"), 0},
					evdev.KEY_D: {StringToNoteUnsafe("g0"), 0},
					evdev.KEY_C: {StringToNoteUnsafe("g#0"), 0},

					evdev.KEY_4: {StringToNoteUnsafe("g#0"), 0},
					evdev.KEY_R: {StringToNoteUnsafe("a0"), 0},
					evdev.KEY_F: {StringToNoteUnsafe("a#0"), 0},
					evdev.KEY_V: {StringToNoteUnsafe("b0"), 0},

					evdev.KEY_5: {StringToNoteUnsafe("b0"), 0},
					evdev.KEY_T: {StringToNoteUnsafe("c1"), 0},
					evdev.KEY_G: {StringToNoteUnsafe("c#1"), 0},
					evdev.KEY_B: {StringToNoteUnsafe("d1"), 0},

					evdev.KEY_6: {StringToNoteUnsafe("d1"), 0},
					evdev.KEY_Y: {StringToNoteUnsafe("d#1"), 0},
					evdev.KEY_H: {StringToNoteUnsafe("e1"), 0},
					evdev.KEY_N: {StringToNoteUnsafe("f1"), 0},

					evdev.KEY_7: {StringToNoteUnsafe("f1"), 0},
					evdev.KEY_U: {StringToNoteUnsafe("f#1"), 0},
					evdev.KEY_J: {StringToNoteUnsafe("g1"), 0},
					evdev.KEY_M: {StringToNoteUnsafe("g#1"), 0},

					evdev.KEY_8:     {StringToNoteUnsafe("g#1"), 0},
					evdev.KEY_I:     {StringToNoteUnsafe("a1"), 0},
					evdev.KEY_K:     {StringToNoteUnsafe("a#1"), 0},
					evdev.KEY_COMMA: {StringToNoteUnsafe("b1"), 0},

					evdev.KEY_9:   {StringToNoteUnsafe("b1"), 0},
					evdev.KEY_O:   {StringToNoteUnsafe("c2"), 0},
					evdev.KEY_L:   {StringToNoteUnsafe("c#2"), 0},
					evdev.KEY_DOT: {StringToNoteUnsafe("d2"), 0},

					evdev.KEY_0:         {StringToNoteUnsafe("d2"), 0},
					evdev.KEY_P:         {StringToNoteUnsafe("d#2"), 0},
					evdev.KEY_SEMICOLON: {StringToNoteUnsafe("e2"), 0},
					evdev.KEY_SLASH:     {StringToNoteUnsafe("f2"), 0},

					evdev.KEY_MINUS:      {StringToNoteUnsafe("f2"), 0},
					evdev.KEY_LEFTBRACE:  {StringToNoteUnsafe("f#2"), 0},
					evdev.KEY_APOSTROPHE: {StringToNoteUnsafe("g2"), 0},
					evdev.KEY_RIGHTSHIFT: {StringToNoteUnsafe("g#2"), 0},

					evdev.KEY_EQUAL:      {StringToNoteUnsafe("g#2"), 0},
					evdev.KEY_RIGHTBRACE: {StringToNoteUnsafe("a2"), 0},
					evdev.KEY_ENTER:      {StringToNoteUnsafe("a#2"), 0},

					evdev.KEY_BACKSPACE: {StringToNoteUnsafe("b2"), 0},
					evdev.KEY_BACKSLASH: {StringToNoteUnsafe("c3"), 0},

					evdev.KEY_KP1:        {StringToNoteUnsafe("c0"), 0},
					evdev.KEY_KP2:        {StringToNoteUnsafe("c#0"), 0},
					evdev.KEY_KP3:        {StringToNoteUnsafe("d0"), 0},
					evdev.KEY_KP0:        {StringToNoteUnsafe("d#0"), 0},
					evdev.KEY_KP4:        {StringToNoteUnsafe("e0"), 0},
					evdev.KEY_KP5:        {StringToNoteUnsafe("f0"), 0},
					evdev.KEY_KP6:        {StringToNoteUnsafe("f#0"), 0},
					evdev.KEY_KPENTER:    {StringToNoteUnsafe("g0"), 0},
					evdev.KEY_KP7:        {StringToNoteUnsafe("g#0"), 0},
					evdev.KEY_KP8:        {StringToNoteUnsafe("a0"), 0},
					evdev.KEY_KP9:        {StringToNoteUnsafe("a#0"), 0},
					evdev.KEY_KPPLUS:     {StringToNoteUnsafe("b0"), 0},
					evdev.KEY_NUMLOCK:    {StringToNoteUnsafe("c1"), 0},
					evdev.KEY_KPSLASH:    {StringToNoteUnsafe("c#1"), 0},
					evdev.KEY_KPASTERISK: {StringToNoteUnsafe("d1"), 0},
					evdev.KEY_KPMINUS:    {StringToNoteUnsafe("d#1"), 0},
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name: "Control",
				Midi: map[evdev.EvCode]Key{
					evdev.KEY_1:          {0, 0},
					evdev.KEY_Q:          {1, 0},
					evdev.KEY_A:          {2, 0},
					evdev.KEY_Z:          {3, 0},
					evdev.KEY_2:          {4, 0},
					evdev.KEY_W:          {5, 0},
					evdev.KEY_S:          {6, 0},
					evdev.KEY_X:          {7, 0},
					evdev.KEY_3:          {8, 0},
					evdev.KEY_E:          {9, 0},
					evdev.KEY_D:          {10, 0},
					evdev.KEY_C:          {11, 0},
					evdev.KEY_4:          {12, 0},
					evdev.KEY_R:          {13, 0},
					evdev.KEY_F:          {14, 0},
					evdev.KEY_V:          {15, 0},
					evdev.KEY_5:          {16, 0},
					evdev.KEY_T:          {17, 0},
					evdev.KEY_G:          {18, 0},
					evdev.KEY_B:          {19, 0},
					evdev.KEY_6:          {20, 0},
					evdev.KEY_Y:          {21, 0},
					evdev.KEY_H:          {22, 0},
					evdev.KEY_N:          {23, 0},
					evdev.KEY_7:          {24, 0},
					evdev.KEY_U:          {25, 0},
					evdev.KEY_J:          {26, 0},
					evdev.KEY_M:          {27, 0},
					evdev.KEY_8:          {28, 0},
					evdev.KEY_I:          {29, 0},
					evdev.KEY_K:          {30, 0},
					evdev.KEY_COMMA:      {31, 0},
					evdev.KEY_9:          {32, 0},
					evdev.KEY_O:          {33, 0},
					evdev.KEY_L:          {34, 0},
					evdev.KEY_DOT:        {35, 0},
					evdev.KEY_0:          {36, 0},
					evdev.KEY_P:          {37, 0},
					evdev.KEY_SEMICOLON:  {38, 0},
					evdev.KEY_SLASH:      {39, 0},
					evdev.KEY_MINUS:      {40, 0},
					evdev.KEY_LEFTBRACE:  {41, 0},
					evdev.KEY_APOSTROPHE: {42, 0},
					evdev.KEY_EQUAL:      {43, 0},
					evdev.KEY_RIGHTBRACE: {44, 0},
					evdev.KEY_ENTER:      {45, 0},
					evdev.KEY_BACKSPACE:  {46, 0},
					evdev.KEY_BACKSLASH:  {47, 0},
					evdev.KEY_GRAVE:      {48, 0},
					evdev.KEY_TAB:        {49, 0},
					evdev.KEY_CAPSLOCK:   {50, 0},
					evdev.KEY_LEFTSHIFT:  {51, 0},
					evdev.KEY_LEFTCTRL:   {52, 0},
					evdev.KEY_LEFTMETA:   {53, 0},
					evdev.KEY_LEFTALT:    {54, 0},
					evdev.KEY_SPACE:      {55, 0},
					evdev.KEY_RIGHTALT:   {96, 0},
					evdev.KEY_RIGHTMETA:  {56, 0},
					evdev.KEY_COMPOSE:    {57, 0},
					evdev.KEY_RIGHTCTRL:  {58, 0},
					evdev.KEY_RIGHTSHIFT: {59, 0},

					evdev.KEY_UP:    {60, 0},
					evdev.KEY_DOWN:  {61, 0},
					evdev.KEY_LEFT:  {62, 0},
					evdev.KEY_RIGHT: {63, 0},

					evdev.KEY_INSERT:   {64, 0},
					evdev.KEY_DELETE:   {65, 0},
					evdev.KEY_HOME:     {66, 0},
					evdev.KEY_END:      {67, 0},
					evdev.KEY_PAGEUP:   {68, 0},
					evdev.KEY_PAGEDOWN: {69, 0},

					evdev.KEY_SYSRQ:      {70, 0},
					evdev.KEY_SCROLLLOCK: {71, 0},
					evdev.KEY_PAUSE:      {72, 0},

					evdev.KEY_PREVIOUSSONG: {73, 0},
					evdev.KEY_PLAYPAUSE:    {74, 0},
					evdev.KEY_NEXTSONG:     {75, 0},
					evdev.KEY_MUTE:         {76, 0},
					evdev.KEY_VOLUMEUP:     {77, 0},
					evdev.KEY_VOLUMEDOWN:   {78, 0},

					evdev.KEY_NUMLOCK:    {79, 0},
					evdev.KEY_KPSLASH:    {80, 0},
					evdev.KEY_KPASTERISK: {81, 0},
					evdev.KEY_KPMINUS:    {82, 0},
					evdev.KEY_KP7:        {83, 0},
					evdev.KEY_KP8:        {84, 0},
					evdev.KEY_KP9:        {85, 0},
					evdev.KEY_KPPLUS:     {86, 0},
					evdev.KEY_KP4:        {87, 0},
					evdev.KEY_KP5:        {88, 0},
					evdev.KEY_KP6:        {89, 0},
					evdev.KEY_KP1:        {90, 0},
					evdev.KEY_KP2:        {91, 0},
					evdev.KEY_KP3:        {92, 0},
					evdev.KEY_KPENTER:    {93, 0},
					evdev.KEY_KP0:        {94, 0},
					evdev.KEY_KPDOT:      {95, 0},
				},
				Analog: map[evdev.EvCode]Analog{},
			},
			{
				Name:   "Debug",
				Midi:   map[evdev.EvCode]Key{},
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
				Midi: map[evdev.EvCode]Key{
					evdev.BTN_A:      {0, 0},
					evdev.BTN_B:      {1, 0},
					evdev.BTN_X:      {2, 0},
					evdev.BTN_Y:      {3, 0},
					evdev.BTN_C:      {4, 0},
					evdev.BTN_Z:      {6, 0},
					evdev.BTN_TL2:    {7, 0},
					evdev.BTN_TR2:    {8, 0},
					evdev.BTN_THUMBL: {9, 0},
					evdev.BTN_THUMBR: {10, 0},
					evdev.BTN_TL:     {11, 0},
					evdev.BTN_TR:     {12, 0},
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
	}

	assert.Equal(t, expectedConfig, c)
}

// TODO: test for deadzone_at_center
