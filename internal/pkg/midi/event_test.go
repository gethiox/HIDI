package midi

import (
	"testing"

	"github.com/gethiox/HIDI/internal/pkg/midi/device/config"

	"github.com/stretchr/testify/assert"
)

func TestNoteToString(t *testing.T) {
	for _, tc := range []struct {
		note     byte
		expected string
	}{
		{note: 0, expected: "C -2"},
		{note: 1, expected: "C#-2"},
		{note: 2, expected: "D -2"},
		{note: 3, expected: "D#-2"},
		{note: 4, expected: "E -2"},
		{note: 5, expected: "F -2"},
		{note: 6, expected: "F#-2"},
		{note: 7, expected: "G -2"},
		{note: 8, expected: "G#-2"},
		{note: 9, expected: "A -2"},
		{note: 10, expected: "A#-2"},
		{note: 11, expected: "B -2"},

		{note: 12, expected: "C -1"},
		{note: 13, expected: "C#-1"},
		{note: 14, expected: "D -1"},
		{note: 15, expected: "D#-1"},
		{note: 16, expected: "E -1"},
		{note: 17, expected: "F -1"},
		{note: 18, expected: "F#-1"},
		{note: 19, expected: "G -1"},
		{note: 20, expected: "G#-1"},
		{note: 21, expected: "A -1"},
		{note: 22, expected: "A#-1"},
		{note: 23, expected: "B -1"},

		{note: 24, expected: "C  0"},
		{note: 25, expected: "C# 0"},
		{note: 26, expected: "D  0"},
		{note: 27, expected: "D# 0"},
		{note: 28, expected: "E  0"},
		{note: 29, expected: "F  0"},
		{note: 30, expected: "F# 0"},
		{note: 31, expected: "G  0"},
		{note: 32, expected: "G# 0"},
		{note: 33, expected: "A  0"},
		{note: 34, expected: "A# 0"},
		{note: 35, expected: "B  0"},

		{note: 36, expected: "C  1"},
		{note: 37, expected: "C# 1"},
		{note: 38, expected: "D  1"},
		{note: 39, expected: "D# 1"},
		{note: 40, expected: "E  1"},
		{note: 41, expected: "F  1"},
		{note: 42, expected: "F# 1"},
		{note: 43, expected: "G  1"},
		{note: 44, expected: "G# 1"},
		{note: 45, expected: "A  1"},
		{note: 46, expected: "A# 1"},
		{note: 47, expected: "B  1"},

		{note: 48, expected: "C  2"},
		{note: 49, expected: "C# 2"},
		{note: 50, expected: "D  2"},
		{note: 51, expected: "D# 2"},
		{note: 52, expected: "E  2"},
		{note: 53, expected: "F  2"},
		{note: 54, expected: "F# 2"},
		{note: 55, expected: "G  2"},
		{note: 56, expected: "G# 2"},
		{note: 57, expected: "A  2"},
		{note: 58, expected: "A# 2"},
		{note: 59, expected: "B  2"},

		{note: 60, expected: "C  3"},
		{note: 61, expected: "C# 3"},
		{note: 62, expected: "D  3"},
		{note: 63, expected: "D# 3"},
		{note: 64, expected: "E  3"},
		{note: 65, expected: "F  3"},
		{note: 66, expected: "F# 3"},
		{note: 67, expected: "G  3"},
		{note: 68, expected: "G# 3"},
		{note: 69, expected: "A  3"},
		{note: 70, expected: "A# 3"},
		{note: 71, expected: "B  3"},

		{note: 72, expected: "C  4"},
		{note: 73, expected: "C# 4"},
		{note: 74, expected: "D  4"},
		{note: 75, expected: "D# 4"},
		{note: 76, expected: "E  4"},
		{note: 77, expected: "F  4"},
		{note: 78, expected: "F# 4"},
		{note: 79, expected: "G  4"},
		{note: 80, expected: "G# 4"},
		{note: 81, expected: "A  4"},
		{note: 82, expected: "A# 4"},
		{note: 83, expected: "B  4"},

		{note: 84, expected: "C  5"},
		{note: 85, expected: "C# 5"},
		{note: 86, expected: "D  5"},
		{note: 87, expected: "D# 5"},
		{note: 88, expected: "E  5"},
		{note: 89, expected: "F  5"},
		{note: 90, expected: "F# 5"},
		{note: 91, expected: "G  5"},
		{note: 92, expected: "G# 5"},
		{note: 93, expected: "A  5"},
		{note: 94, expected: "A# 5"},
		{note: 95, expected: "B  5"},

		{note: 96, expected: "C  6"},
		{note: 97, expected: "C# 6"},
		{note: 98, expected: "D  6"},
		{note: 99, expected: "D# 6"},
		{note: 100, expected: "E  6"},
		{note: 101, expected: "F  6"},
		{note: 102, expected: "F# 6"},
		{note: 103, expected: "G  6"},
		{note: 104, expected: "G# 6"},
		{note: 105, expected: "A  6"},
		{note: 106, expected: "A# 6"},
		{note: 107, expected: "B  6"},

		{note: 108, expected: "C  7"},
		{note: 109, expected: "C# 7"},
		{note: 110, expected: "D  7"},
		{note: 111, expected: "D# 7"},
		{note: 112, expected: "E  7"},
		{note: 113, expected: "F  7"},
		{note: 114, expected: "F# 7"},
		{note: 115, expected: "G  7"},
		{note: 116, expected: "G# 7"},
		{note: 117, expected: "A  7"},
		{note: 118, expected: "A# 7"},
		{note: 119, expected: "B  7"},

		{note: 120, expected: "C  8"},
		{note: 121, expected: "C# 8"},
		{note: 122, expected: "D  8"},
		{note: 123, expected: "D# 8"},
		{note: 124, expected: "E  8"},
		{note: 125, expected: "F  8"},
		{note: 126, expected: "F# 8"},
		{note: 127, expected: "G  8"},
	} {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, noteToString(tc.note), tc.expected)
		})
	}
}

func TestStringToNote(t *testing.T) {
	for _, tc := range []struct {
		string   string
		expected byte
	}{
		{string: "C-2", expected: 0},
		{string: "C#-2", expected: 1},
		{string: "D#-2", expected: 3},
		{string: "E-2", expected: 4},
		{string: "F-2", expected: 5},
		{string: "F#-2", expected: 6},
		{string: "G-2", expected: 7},
		{string: "G#-2", expected: 8},
		{string: "A-2", expected: 9},
		{string: "A#-2", expected: 10},
		{string: "B-2", expected: 11},

		{string: "C-1", expected: 12},
		{string: "C#-1", expected: 13},
		{string: "D-1", expected: 14},
		{string: "D#-1", expected: 15},
		{string: "E-1", expected: 16},
		{string: "F-1", expected: 17},
		{string: "F#-1", expected: 18},
		{string: "G-1", expected: 19},
		{string: "G#-1", expected: 20},
		{string: "A-1", expected: 21},
		{string: "A#-1", expected: 22},
		{string: "B-1", expected: 23},

		{string: "C0", expected: 24},
		{string: "C#0", expected: 25},
		{string: "D0", expected: 26},
		{string: "D#0", expected: 27},
		{string: "E0", expected: 28},
		{string: "F0", expected: 29},
		{string: "F#0", expected: 30},
		{string: "G0", expected: 31},
		{string: "G#0", expected: 32},
		{string: "A0", expected: 33},
		{string: "A#0", expected: 34},
		{string: "B0", expected: 35},

		{string: "C1", expected: 36},
		{string: "C#1", expected: 37},
		{string: "D1", expected: 38},
		{string: "D#1", expected: 39},
		{string: "E1", expected: 40},
		{string: "F1", expected: 41},
		{string: "F#1", expected: 42},
		{string: "G1", expected: 43},
		{string: "G#1", expected: 44},
		{string: "A1", expected: 45},
		{string: "A#1", expected: 46},
		{string: "B1", expected: 47},

		{string: "C2", expected: 48},
		{string: "C#2", expected: 49},
		{string: "D2", expected: 50},
		{string: "D#2", expected: 51},
		{string: "E2", expected: 52},
		{string: "F2", expected: 53},
		{string: "F#2", expected: 54},
		{string: "G2", expected: 55},
		{string: "G#2", expected: 56},
		{string: "A2", expected: 57},
		{string: "A#2", expected: 58},
		{string: "B2", expected: 59},

		{string: "C3", expected: 60},
		{string: "C#3", expected: 61},
		{string: "D3", expected: 62},
		{string: "D#3", expected: 63},
		{string: "E3", expected: 64},
		{string: "F3", expected: 65},
		{string: "F#3", expected: 66},
		{string: "G3", expected: 67},
		{string: "G#3", expected: 68},
		{string: "A3", expected: 69},
		{string: "A#3", expected: 70},
		{string: "B3", expected: 71},

		{string: "C4", expected: 72},
		{string: "C#4", expected: 73},
		{string: "D4", expected: 74},
		{string: "D#4", expected: 75},
		{string: "E4", expected: 76},
		{string: "F4", expected: 77},
		{string: "F#4", expected: 78},
		{string: "G4", expected: 79},
		{string: "G#4", expected: 80},
		{string: "A4", expected: 81},
		{string: "A#4", expected: 82},
		{string: "B4", expected: 83},

		{string: "C5", expected: 84},
		{string: "C#5", expected: 85},
		{string: "D5", expected: 86},
		{string: "D#5", expected: 87},
		{string: "E5", expected: 88},
		{string: "F5", expected: 89},
		{string: "F#5", expected: 90},
		{string: "G5", expected: 91},
		{string: "G#5", expected: 92},
		{string: "A5", expected: 93},
		{string: "A#5", expected: 94},
		{string: "B5", expected: 95},

		{string: "C6", expected: 96},
		{string: "C#6", expected: 97},
		{string: "D6", expected: 98},
		{string: "D#6", expected: 99},
		{string: "E6", expected: 100},
		{string: "F6", expected: 101},
		{string: "F#6", expected: 102},
		{string: "G6", expected: 103},
		{string: "G#6", expected: 104},
		{string: "A6", expected: 105},
		{string: "A#6", expected: 106},
		{string: "B6", expected: 107},

		{string: "C7", expected: 108},
		{string: "C#7", expected: 109},
		{string: "D7", expected: 110},
		{string: "D#7", expected: 111},
		{string: "E7", expected: 112},
		{string: "F7", expected: 113},
		{string: "F#7", expected: 114},
		{string: "G7", expected: 115},
		{string: "G#7", expected: 116},
		{string: "A7", expected: 117},
		{string: "A#7", expected: 118},
		{string: "B7", expected: 119},

		{string: "C8", expected: 120},
		{string: "C#8", expected: 121},
		{string: "D8", expected: 122},
		{string: "D#8", expected: 123},
		{string: "E8", expected: 124},
		{string: "F8", expected: 125},
		{string: "F#8", expected: 126},
		{string: "G8", expected: 127},
	} {
		t.Run(tc.string, func(t *testing.T) {
			note, err := config.StringToNote(tc.string)
			assert.Equal(t, nil, err)
			assert.Equal(t, tc.expected, note)
		})
	}
}

func TestStringToNoteFail(t *testing.T) {
	for _, tc := range []string{
		"b-3", // outside of bottom range
		"g#8", // outside of upper range
		"",
		// unaligned
		" c-2",
		"c-2 ",
		"BLAH junk text c-2",
	} {
		t.Run(tc, func(t *testing.T) {
			note, err := config.StringToNote(tc)
			assert.Equal(t, byte(0), note)
			assert.NotEqual(t, nil, err)
		})
	}
}

func TestEvent_String(t *testing.T) {
	for _, tc := range []struct {
		midiEvent Event
		expected  string
	}{
		{
			midiEvent: []byte{0b10000000, 0b00000000, 0b00000000},
			expected:  "Note Off: C -2 (channel:  1, velocity:   0)",
		}, {
			midiEvent: []byte{0b10000000, 0b00000001, 0b00000000},
			expected:  "Note Off: C#-2 (channel:  1, velocity:   0)",
		}, {
			midiEvent: []byte{0b10001111, 0b00000001, 0b00000000},
			expected:  "Note Off: C#-2 (channel: 16, velocity:   0)",
		}, {
			midiEvent: []byte{0b10001111, 0b00000001, 0b01111111},
			expected:  "Note Off: C#-2 (channel: 16, velocity: 127)",
		}, {
			midiEvent: []byte{0b10001111, 0b01111111, 0b01111111},
			expected:  "Note Off: G  8 (channel: 16, velocity: 127)",
		},

		{
			midiEvent: []byte{0b10010000, 0b00000000, 0b00000000},
			expected:  "Note On : C -2 (channel:  1, velocity:   0)",
		}, {
			midiEvent: []byte{0b10010000, 0b00000001, 0b00000000},
			expected:  "Note On : C#-2 (channel:  1, velocity:   0)",
		}, {
			midiEvent: []byte{0b10011111, 0b00000001, 0b00000000},
			expected:  "Note On : C#-2 (channel: 16, velocity:   0)",
		}, {
			midiEvent: []byte{0b10011111, 0b00000001, 0b01111111},
			expected:  "Note On : C#-2 (channel: 16, velocity: 127)",
		}, {
			midiEvent: []byte{0b10011111, 0b01111111, 0b01111111},
			expected:  "Note On : G  8 (channel: 16, velocity: 127)",
		},

		{
			midiEvent: []byte{0b10100000, 0b00000000, 0b00000000},
			expected:  "Polyphonic Key Pressure: C -2 (channel:  1, pressure:   0)",
		}, {
			midiEvent: []byte{0b10100000, 0b00000001, 0b00000000},
			expected:  "Polyphonic Key Pressure: C#-2 (channel:  1, pressure:   0)",
		}, {
			midiEvent: []byte{0b10101111, 0b00000001, 0b00000000},
			expected:  "Polyphonic Key Pressure: C#-2 (channel: 16, pressure:   0)",
		}, {
			midiEvent: []byte{0b10101111, 0b00000001, 0b01111111},
			expected:  "Polyphonic Key Pressure: C#-2 (channel: 16, pressure: 127)",
		}, {
			midiEvent: []byte{0b10101111, 0b01111111, 0b01111111},
			expected:  "Polyphonic Key Pressure: G  8 (channel: 16, pressure: 127)",
		},

		{
			midiEvent: []byte{0b10110000, 0b00000000, 0b00000000},
			expected:  "Control Change:   0, value:   0 (channel:  1)",
		}, {
			midiEvent: []byte{0b10110000, 0b00000001, 0b00000000},
			expected:  "Control Change:   1, value:   0 (channel:  1)",
		}, {
			midiEvent: []byte{0b10111111, 0b00000001, 0b00000000},
			expected:  "Control Change:   1, value:   0 (channel: 16)",
		}, {
			midiEvent: []byte{0b10111111, 0b00000001, 0b01111111},
			expected:  "Control Change:   1, value: 127 (channel: 16)",
		}, {
			midiEvent: []byte{0b10111111, 0b01111111, 0b01111111},
			expected:  "Control Change: 127, value: 127 (channel: 16)",
		},

		{
			midiEvent: []byte{0b11000000, 0b00000000},
			expected:  "Program Change:   0 (channel:  1)",
		}, {
			midiEvent: []byte{0b11000000, 0b00000001},
			expected:  "Program Change:   1 (channel:  1)",
		}, {
			midiEvent: []byte{0b11001111, 0b00000001},
			expected:  "Program Change:   1 (channel: 16)",
		}, {
			midiEvent: []byte{0b11001111, 0b01111111},
			expected:  "Program Change: 127 (channel: 16)",
		},

		{
			midiEvent: []byte{0b11010000, 0b00000000},
			expected:  "Channel Pressure:   0 (channel:  1)",
		}, {
			midiEvent: []byte{0b11010000, 0b00000001},
			expected:  "Channel Pressure:   1 (channel:  1)",
		}, {
			midiEvent: []byte{0b11011111, 0b00000001},
			expected:  "Channel Pressure:   1 (channel: 16)",
		}, {
			midiEvent: []byte{0b11011111, 0b01111111},
			expected:  "Channel Pressure: 127 (channel: 16)",
		},

		{
			midiEvent: []byte{0b11100000, 0b00000000, 0b01000000},
			expected:  "Pitch Bend:    0% (channel:  1)",
		}, {
			midiEvent: []byte{0b11101111, 0b00000000, 0b01000000},
			expected:  "Pitch Bend:    0% (channel: 16)",
		}, {
			midiEvent: []byte{0b11101111, 0b00000000, 0b01100000},
			expected:  "Pitch Bend:   50% (channel: 16)",
		}, {
			midiEvent: []byte{0b11101111, 0b01111111, 0b01111111},
			expected:  "Pitch Bend:  100% (channel: 16)",
		}, {
			midiEvent: []byte{0b11101111, 0b00000000, 0b00100000},
			expected:  "Pitch Bend:  -50% (channel: 16)",
		}, {
			midiEvent: []byte{0b11101111, 0b00000000, 0b00000000},
			expected:  "Pitch Bend: -100% (channel: 16)",
		},
	} {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.midiEvent.String())
		})
	}
}
