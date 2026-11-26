package light

import (
	"math"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// Scene is everything the colours of a keyboard depend on.
type Scene struct {
	// Active says whether HIDI is on. While it is off the keyboard types,
	// and is lit evenly, except for the keys that are down.
	Active bool
	// Pressed are the keys of the device that are down. They show while
	// HIDI is off.
	Pressed []control.Code
	// Keys is what every bound key does in the selected layout.
	Keys []engine.KeyRole
	// Layout is the selected layout and Layouts how many there are.
	Layout, Layouts int
	// State is the state of the instrument.
	State engine.State
	// External holds the notes other programs have sent to the
	// instrument and not ended yet, by channel: bit n%64 of element n/64
	// for note n.
	External [16][2]uint64
}

// Colours are the colours that can be chosen.
type Colours struct {
	// White, Black and C colour the keys that play white keys of a
	// piano, black ones, and every C, which gives the eye a place to
	// start from.
	White, Black, C RGB
	// Unbound colours keys that do nothing.
	Unbound RGB
	// Playing colours a key whose note sounds because of this
	// instrument, and External one whose note another program sent on
	// the instrument's channel.
	Playing, External RGB
	// Idle colours every key while HIDI is off, and Pressed a key that
	// is down then.
	Idle, Pressed RGB
	// Command colours the keys that change the instrument. They show how
	// far a value has gone, in three steps of brightness: this colour is
	// the brightest.
	Command RGB
	// Panic colours the key that silences the instrument.
	Panic RGB
	// Learn colours the key of learning mode, Chord the key that
	// remembers a chord: dim while the mode is off or no chord is
	// remembered, at full brightness otherwise. Velocity colours the
	// keys that move the velocity, the brighter the higher it is.
	Learn, Chord, Velocity RGB
	// Other colours a key that sends something else, a controller for
	// one.
	Other RGB
	// Passthrough colours a key that is left to the system.
	Passthrough RGB
}

// DefaultColours are the colours of HIDI 1.x.
func DefaultColours() Colours {
	return Colours{
		White: RGB{0x00, 0x55, 0x00}, Black: RGB{0x00, 0x00, 0x55}, C: RGB{0x55, 0x55, 0x00},
		Unbound: RGB{0x44, 0x00, 0x00},
		Playing: RGB{0xff, 0xff, 0xff}, External: RGB{0xff, 0xff, 0xff},
		Idle: RGB{0x18, 0x18, 0x18}, Pressed: RGB{0xff, 0xff, 0xff},
		Command: RGB{0xff, 0xff, 0xff}, Panic: RGB{0xff, 0x00, 0x00},
		Learn: RGB{0xff, 0xd0, 0x00}, Chord: RGB{0xa0, 0x30, 0xff}, Velocity: RGB{0x00, 0xe0, 0xff},
		Other: RGB{0x00, 0x30, 0x30}, Passthrough: RGB{0x40, 0x00, 0x40},
	}
}

// Named gives the colours by the names they have in the settings, in the
// order they are shown.
func (c *Colours) Named() []struct {
	Name, About string
	Colour      *RGB
} {
	return []struct {
		Name, About string
		Colour      *RGB
	}{
		{"white", "keys that play white keys", &c.White},
		{"black", "keys that play black keys", &c.Black},
		{"c", "keys that play a C", &c.C},
		{"unbound", "keys that do nothing", &c.Unbound},
		{"playing", "a key whose note sounds", &c.Playing},
		{"external", "a note another program sent", &c.External},
		{"command", "command keys, at their brightest", &c.Command},
		{"panic", "the panic key", &c.Panic},
		{"learn", "the learning key, at its brightest", &c.Learn},
		{"chord", "the chord key, at its brightest", &c.Chord},
		{"velocity", "the velocity keys, at their brightest", &c.Velocity},
		{"other", "keys that send something else", &c.Other},
		{"passthrough", "keys left to the system", &c.Passthrough},
		{"idle", "every key while HIDI is off", &c.Idle},
		{"pressed", "a key held while HIDI is off", &c.Pressed},
	}
}

// scale returns a colour at a part of its brightness, in 255ths.
func scale(c RGB, by int) RGB {
	return RGB{uint8(int(c.R) * by / 255), uint8(int(c.G) * by / 255), uint8(int(c.B) * by / 255)}
}

// The three steps of the command keys: there, used once, used more.
const (
	dim    = 27
	medium = 100
	bright = 255
)

// ChannelColour gives each of the sixteen MIDI channels a colour of its
// own. From one channel to the next the hue moves almost half way round
// the colour wheel, so that neighbouring channels differ clearly. (1.x went
// round twice in sixteen steps, which gave channel 9 the colour of
// channel 1.)
func ChannelColour(channel uint8) RGB {
	hue := math.Mod(360.0/16*7*float64(channel%16)+30, 360)
	return hsv(hue)
}

// hsv returns the full, bright colour of a hue in degrees.
func hsv(hue float64) RGB {
	x := 1 - math.Abs(math.Mod(hue/60, 2)-1)
	var r, g, b float64
	switch int(hue / 60) {
	case 0:
		r, g = 1, x
	case 1:
		r, g = x, 1
	case 2:
		g, b = 1, x
	case 3:
		g, b = x, 1
	case 4:
		r, b = x, 1
	default:
		r, b = 1, x
	}
	return RGB{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5)}
}

func third(c RGB) RGB { return RGB{c.R / 3, c.G / 3, c.B / 3} }

func has(set [2]uint64, note int) bool { return set[note/64]&(1<<(note%64)) != 0 }

// steps picks a white for one of a pair of keys that move a value up and
// down: brighter the further the value has gone in the key's direction.
func steps(value int) int {
	switch {
	case value >= 2:
		return bright
	case value == 1:
		return medium
	}
	return dim
}

// Paint returns the colour of every key that has one of its own. Keys it
// does not name are unbound; while HIDI is off, they are idle.
func Paint(s Scene, c Colours) map[control.Code]RGB {
	out := make(map[control.Code]RGB, len(s.Keys))
	if !s.Active {
		for _, key := range s.Pressed {
			out[key] = c.Pressed
		}
		return out
	}
	channelColour := ChannelColour(s.State.Channel)
	for _, key := range s.Keys {
		switch action := key.Action.(type) {
		case engine.Note:
			out[key.Code] = paintNote(s, c, action)
		case engine.Command:
			out[key.Code] = paintCommand(s, c, action, channelColour)
		case engine.Passthrough:
			out[key.Code] = c.Passthrough
		default:
			out[key.Code] = c.Other
		}
	}
	return out
}

func paintNote(s Scene, c Colours, a engine.Note) RGB {
	note := int(a.Note) + s.State.Octave*12 + s.State.Transpose
	if note < 0 || note > 127 {
		return c.Unbound // out of range: the key plays nothing now
	}
	colour := c.White
	switch {
	case note%12 == 0:
		colour = c.C
	case isBlack(note):
		colour = c.Black
	}
	// Notes from outside show in the colour of their channel; those on
	// the channel this key plays on stand out.
	own := (s.State.Channel + a.ChannelOffset) % 16
	for channel := 15; channel >= 0; channel-- {
		if has(s.External[channel], note) {
			colour = ChannelColour(uint8(channel))
		}
	}
	if has(s.External[own], note) {
		colour = c.External
	}
	if has(s.State.Notes, note) {
		colour = c.Playing
	}
	return colour
}

func isBlack(note int) bool {
	switch note % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

func paintCommand(s Scene, c Colours, command engine.Command, channelColour RGB) RGB {
	level := medium
	switch command {
	case engine.CommandPanic:
		return c.Panic
	case engine.CommandOctaveUp:
		level = steps(s.State.Octave)
	case engine.CommandOctaveDown:
		level = steps(-s.State.Octave)
	case engine.CommandTransposeUp:
		level = steps(s.State.Transpose)
	case engine.CommandTransposeDown:
		level = steps(-s.State.Transpose)
	case engine.CommandLayoutNext:
		level = bright
		if s.Layout >= s.Layouts-1 {
			level = dim // nowhere further to go
		}
	case engine.CommandLayoutPrevious:
		level = bright
		if s.Layout <= 0 {
			level = dim
		}
	case engine.CommandChannelUp:
		if s.State.Channel >= 15 {
			return third(channelColour)
		}
		return channelColour
	case engine.CommandChannelDown:
		if s.State.Channel == 0 {
			return third(channelColour)
		}
		return channelColour
	case engine.CommandChord:
		if s.State.Chord != "" {
			return c.Chord
		}
		return scale(c.Chord, dim)
	case engine.CommandLearn:
		if s.State.Learning {
			return c.Learn
		}
		return scale(c.Learn, dim)
	case engine.CommandVelocityUp, engine.CommandVelocityDown:
		// Both keys show the velocity: dim at 1, full at 127.
		v := max(int(s.State.Velocity), 1)
		return scale(c.Velocity, dim+(bright-dim)*(v-1)/126)
	}
	return scale(c.Command, level)
}
