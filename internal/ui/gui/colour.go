package gui

import (
	"math"

	"github.com/gethiox/HIDI/internal/light"
)

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// colText is the colour of ordinary text.
var colText = RGB{0xe6, 0xe9, 0xee}

// colBar is the colour of the bar at the bottom and of what is set into
// the panels, such as the track of an axis.
var colBar = RGB{0x0c, 0x0e, 0x11}

// The colours of a key cap that plays a note: the window's own.
var (
	colWhiteNote = RGB{0x1f, 0x5a, 0x38}
	colBlackNote = RGB{0x24, 0x33, 0x66}
	colCNote     = RGB{0x6b, 0x64, 0x1e}
)

// caps are the colours of key caps that do something other than play a
// note: those of the lights, as the settings have them, made into caps
// the lettering stands out on (see capsOf).
type caps struct {
	command, other, pass, nothing RGB
}

// The strength of a light's hue over the ground of a cap: a key that
// does something, and one that does nothing, which is no more than a
// hint.
const (
	capStrength     = 0.38
	nothingStrength = 0.10
)

// capsOf makes the colours of key caps of the colours of the lights. A
// light's colour is for a key lit from below: a dim grey for keys left to
// the system, a full magenta for commands. A cap takes the light's hue
// over the dark ground of a cap, half at the strength of a cap whatever
// the light, half as bright as the light is: a dim light gives a quieter
// cap, and none is too dark for its lettering. A key that does nothing is
// only a hint of its colour.
func capsOf(c light.Colours) caps {
	hue := func(l light.RGB, strength float32, dims bool) RGB {
		top := max(l.R, l.G, l.B)
		if top == 0 {
			return colBar
		}
		if dims {
			strength *= 0.5 + 0.5*float32(top)/255
		}
		full := RGB{uint8(int(l.R) * 255 / int(top)), uint8(int(l.G) * 255 / int(top)), uint8(int(l.B) * 255 / int(top))}
		return Mix(colBar, full, strength)
	}
	return caps{
		command: hue(c.Command, capStrength, true),
		other:   hue(c.Other, capStrength, true),
		pass:    hue(c.Passthrough, capStrength, true),
		nothing: hue(c.Unbound, nothingStrength, false),
	}
}

// defaultCaps are the caps of the colours as HIDI comes.
var defaultCaps = capsOf(light.DefaultColours())

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
// A key without a role does nothing.
func KeyColour(role string, note, octave, transpose int, c caps) RGB {
	switch role {
	case "":
		return c.nothing
	case "note":
		return NoteColour(note + octave*12 + transpose)
	case "command":
		return c.command
	case "passthrough":
		return c.pass
	}
	return c.other
}

// RoleColour is the colour of a key cap on the profile page, for what the
// key does.
func RoleColour(r Role, c caps) RGB {
	switch r.Kind {
	case RoleNote:
		return NoteColour(int(r.Note))
	case RoleCommand:
		return c.command
	case RoleOther, RoleController, RoleBend:
		return c.other
	case RolePass:
		return c.pass
	}
	return c.nothing
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
