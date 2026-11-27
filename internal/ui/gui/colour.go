package gui

import "math"

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// colText is the colour of ordinary text.
var colText = RGB{0xe6, 0xe9, 0xee}

// colBar is the colour of the bar at the bottom and of what is set into
// the panels, such as the track of an axis.
var colBar = RGB{0x0c, 0x0e, 0x11}

// The colours of a key cap by what the key does.
var (
	colWhiteNote  = RGB{0x1f, 0x5a, 0x38}
	colBlackNote  = RGB{0x24, 0x33, 0x66}
	colCNote      = RGB{0x6b, 0x64, 0x1e}
	colCommandKey = RGB{0x5a, 0x2f, 0x66}
	colOtherKey   = RGB{0x2a, 0x4a, 0x52}
	colPassKey    = RGB{0x66, 0x3d, 0x2a}
	// colNothingKey is a key that does nothing, bound or not: barely red,
	// as the keyboard's own lights show it, and no more than a hint.
	colNothingKey = RGB{0x22, 0x12, 0x15}
)

// NoteColour is the colour of a key cap that plays a note: one for the
// white keys of a piano, one for the black ones, and one for every C.
func NoteColour(note int) RGB {
	switch {
	case ((note%12)+12)%12 == 0:
		return colCNote
	case IsBlack(note):
		return colBlackNote
	}
	return colWhiteNote
}

// KeyColour is the colour of a key cap in the view of a running
// instrument: by the role the instrument reports for the key, and, for a
// note, by the note as the instrument's octave and transposition make it.
// A key without a role has the colour of the keyboard itself.
func KeyColour(role string, note, octave, transpose int) RGB {
	switch role {
	case "":
		return colNothingKey
	case "note":
		return NoteColour(note + octave*12 + transpose)
	case "command":
		return colCommandKey
	case "passthrough":
		return colPassKey
	}
	return colOtherKey
}

// ParseColour reads a colour written as "#rrggbb"; anything else is
// black.
func ParseColour(text string) RGB {
	if len(text) != 7 || text[0] != '#' {
		return RGB{}
	}
	var v [3]uint8
	for i := range 3 {
		for _, ch := range []byte(text[1+2*i : 3+2*i]) {
			var d uint8
			switch {
			case ch >= '0' && ch <= '9':
				d = ch - '0'
			case ch >= 'a' && ch <= 'f':
				d = ch - 'a' + 10
			case ch >= 'A' && ch <= 'F':
				d = ch - 'A' + 10
			default:
				return RGB{}
			}
			v[i] = v[i]<<4 | d
		}
	}
	return RGB{v[0], v[1], v[2]}
}

// Glow turns the colour a light is set to into the colour it is drawn
// with. A light at a third of its strength does not look a third as
// bright as one at full strength, it looks more than half as bright; the
// drawing follows the eye.
func Glow(c RGB) RGB {
	return RGB{glowCurve[c.R], glowCurve[c.G], glowCurve[c.B]}
}

var glowCurve = func() (t [256]uint8) {
	for i := range t {
		t[i] = uint8(math.Pow(float64(i)/255, 0.55)*255 + 0.5)
	}
	return t
}()

// ansiColours are the 16 colours a log line can use, chosen to read well
// on the window's dark background.
var ansiColours = map[int]RGB{
	30: {0x5c, 0x63, 0x6e}, 31: {0xe0, 0x5a, 0x5a}, 32: {0x4c, 0xc2, 0x7a}, 33: {0xd9, 0xb3, 0x4a},
	34: {0x5a, 0x9b, 0xe0}, 35: {0xc0, 0x7a, 0xd9}, 36: {0x4a, 0xc0, 0xc9}, 37: {0xc9, 0xce, 0xd6},
	90: {0x7d, 0x86, 0x95}, 91: {0xff, 0x7b, 0x72}, 92: {0x6f, 0xe3, 0x9a}, 93: {0xf2, 0xcf, 0x6b},
	94: {0x7f, 0xb6, 0xf5}, 95: {0xdb, 0x9c, 0xf0}, 96: {0x6f, 0xdc, 0xe3}, 97: {0xf2, 0xf4, 0xf7},
}

// AnsiColour returns the colour for an ANSI foreground code; 0 and codes
// the window does not know give the colour of ordinary text.
func AnsiColour(code int) RGB {
	if c, ok := ansiColours[code]; ok {
		return c
	}
	return colText
}
