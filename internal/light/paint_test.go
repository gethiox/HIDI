package light

import (
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

var (
	keyC3, keyCs3, keyD3 = control.Key(30), control.Key(17), control.Key(31)
	keyUp, keyDown       = control.Key(60), control.Key(59)
)

func scene() Scene {
	return Scene{
		Active: true, Layout: 0, Layouts: 2,
		Keys: []engine.KeyRole{
			{Code: keyC3, Action: engine.Note{Note: 60}},
			{Code: keyCs3, Action: engine.Note{Note: 61}},
			{Code: keyD3, Action: engine.Note{Note: 62}},
			{Code: keyUp, Action: engine.CommandOctaveUp},
			{Code: keyDown, Action: engine.CommandOctaveDown},
			{Code: control.Key(1), Action: engine.CommandPanic},
			{Code: control.Key(87), Action: engine.CommandLayoutPrevious},
			{Code: control.Key(88), Action: engine.CommandLayoutNext},
			{Code: control.Key(63), Action: engine.CommandChannelDown},
			{Code: control.Key(64), Action: engine.CommandChannelUp},
			{Code: control.Key(67), Action: engine.CommandChord},
			{Code: control.Key(68), Action: engine.CommandLearn},
			{Code: control.Key(65), Action: engine.CommandVelocityDown},
			{Code: control.Key(66), Action: engine.CommandVelocityUp},
			{Code: control.Key(57), Action: engine.ControlChange{Controller: 64}},
		},
	}
}

func setNote(set *[2]uint64, note int) { set[note/64] |= 1 << (note % 64) }

func TestPaintNotes(t *testing.T) {
	c := DefaultColours()
	s := scene()
	got := Paint(s, c)
	if got[keyC3] != c.C || got[keyCs3] != c.Black || got[keyD3] != c.White {
		t.Errorf("at rest: C %v, C sharp %v, D %v", got[keyC3], got[keyCs3], got[keyD3])
	}

	// Transposed by a semitone, the same keys play other notes.
	s.State.Transpose = 1
	got = Paint(s, c)
	if got[keyC3] != c.Black || got[keyCs3] != c.White || got[keyD3] != c.Black {
		t.Errorf("a semitone up: %v %v %v", got[keyC3], got[keyCs3], got[keyD3])
	}
	// Far enough up, a key plays nothing.
	s.State.Transpose, s.State.Octave = 0, 6
	if got = Paint(s, c); got[keyC3] != c.Unbound {
		t.Errorf("out of range: %v", got[keyC3])
	}

	// A note this instrument plays, one from outside on its channel, and
	// one from outside on another channel.
	s = scene()
	setNote(&s.State.Notes, 60)
	setNote(&s.External[0], 61)
	setNote(&s.External[5], 62)
	got = Paint(s, c)
	if got[keyC3] != c.Playing || got[keyCs3] != c.External || got[keyD3] != ChannelColour(5) {
		t.Errorf("sounding: %v %v %v", got[keyC3], got[keyCs3], got[keyD3])
	}
	// Its own note wins over one from outside.
	setNote(&s.External[0], 60)
	if got = Paint(s, c); got[keyC3] != c.Playing {
		t.Errorf("own note over an outside one: %v", got[keyC3])
	}
}

func TestPaintCommands(t *testing.T) {
	c := DefaultColours()
	dim, medium, bright := scale(c.Command, dim), scale(c.Command, medium), scale(c.Command, bright)
	red, other := c.Panic, c.Other
	s := scene()
	got := Paint(s, c)
	if got[keyUp] != dim || got[keyDown] != dim {
		t.Errorf("octave keys at rest: %v %v", got[keyUp], got[keyDown])
	}
	if got[control.Key(1)] != red {
		t.Errorf("panic: %v", got[control.Key(1)])
	}
	if got[control.Key(87)] != dim || got[control.Key(88)] != bright {
		t.Errorf("layout keys on the first layout: %v %v", got[control.Key(87)], got[control.Key(88)])
	}
	if got[control.Key(63)] != third(ChannelColour(0)) || got[control.Key(64)] != ChannelColour(0) {
		t.Errorf("channel keys on the first channel: %v %v", got[control.Key(63)], got[control.Key(64)])
	}
	if got[control.Key(67)] != scale(c.Chord, 27) || got[control.Key(68)] != scale(c.Learn, 27) || got[control.Key(57)] != other {
		t.Errorf("chord %v, learn %v, controller %v", got[control.Key(67)], got[control.Key(68)], got[control.Key(57)])
	}
	// The velocity keys show the velocity, both alike: dim at 1, at full
	// brightness at 127, between in proportion.
	for _, v := range []struct {
		velocity uint8
		level    int
	}{{1, 27}, {127, 255}, {64, 141}} {
		s.State.Velocity = v.velocity
		got = Paint(s, c)
		if want := scale(c.Velocity, v.level); got[control.Key(65)] != want || got[control.Key(66)] != want {
			t.Errorf("velocity %d: keys %v %v, want %v", v.velocity, got[control.Key(65)], got[control.Key(66)], want)
		}
	}

	s.State.Octave, s.Layout, s.State.Channel, s.State.Chord, s.State.Learning = 1, 1, 15, "+4 +7", true
	got = Paint(s, c)
	if got[keyUp] != medium || got[keyDown] != dim {
		t.Errorf("one octave up: %v %v", got[keyUp], got[keyDown])
	}
	if got[control.Key(87)] != bright || got[control.Key(88)] != dim {
		t.Errorf("layout keys on the last layout: %v %v", got[control.Key(87)], got[control.Key(88)])
	}
	if got[control.Key(64)] != third(ChannelColour(15)) || got[control.Key(67)] != c.Chord || got[control.Key(68)] != c.Learn {
		t.Errorf("last channel %v, chord %v, learn %v", got[control.Key(64)], got[control.Key(67)], got[control.Key(68)])
	}
	s.State.Octave = -3
	if got = Paint(s, c); got[keyUp] != dim || got[keyDown] != bright {
		t.Errorf("three octaves down: %v %v", got[keyUp], got[keyDown])
	}
}

func TestPaintWhileOff(t *testing.T) {
	c := DefaultColours()
	s := scene()
	s.Active = false
	setNote(&s.State.Notes, 60)
	if got := Paint(s, c); len(got) != 0 {
		t.Errorf("keys with a colour of their own while HIDI is off: %v", got)
	}
	// A key that is down shows, bound or not.
	s.Pressed = []control.Code{keyUp, control.Key(200)}
	got := Paint(s, c)
	if len(got) != 2 || got[keyUp] != c.Pressed || got[control.Key(200)] != c.Pressed {
		t.Errorf("pressed keys while HIDI is off: %v", got)
	}
}

func TestPaintPassthroughAndCustomColours(t *testing.T) {
	c := DefaultColours()
	c.Command, c.Passthrough = RGB{0, 0, 255}, RGB{1, 2, 3}
	s := scene()
	s.Keys = append(s.Keys, engine.KeyRole{Code: control.Key(57), Action: engine.Passthrough{}})
	got := Paint(s, c)
	if got[control.Key(57)] != (RGB{1, 2, 3}) {
		t.Errorf("a passed key is %v", got[control.Key(57)])
	}
	if got[keyUp] != (RGB{0, 0, 27}) {
		t.Errorf("a command key at rest is %v, want the command colour dimmed", got[keyUp])
	}
	names := map[string]bool{}
	for _, n := range c.Named() {
		names[n.Name] = true
	}
	if len(names) != 15 {
		t.Errorf("%d named colours, want 15", len(names))
	}
}

func TestChannelColours(t *testing.T) {
	seen := map[RGB]bool{}
	for channel := range uint8(16) {
		colour := ChannelColour(channel)
		if seen[colour] {
			t.Errorf("channel %d shares its colour %v", channel+1, colour)
		}
		seen[colour] = true
		if max(colour.R, colour.G, colour.B) != 255 {
			t.Errorf("channel %d is not bright: %v", channel+1, colour)
		}
	}
}

func TestKeyOfLED(t *testing.T) {
	for name, want := range map[string]string{
		"Key: A": "KEY_A", "Key: 7": "KEY_7", "Key: F1": "KEY_F1", "Key: F12": "KEY_F12",
		"Key: Escape": "KEY_ESC", "Key: Left Windows": "KEY_LEFTMETA", "Key: \\ (ANSI)": "KEY_BACKSLASH",
		"Key: Number Pad 5": "KEY_KP5", "Key: Number Pad Enter": "KEY_KPENTER", "Key: Up Arrow": "KEY_UP",
		"Key: ;": "KEY_SEMICOLON", "Key: Media Mute": "KEY_MUTE",
	} {
		code, ok := KeyOfLED(name)
		if !ok || code.String() != want {
			t.Errorf("KeyOfLED(%q) = %v, %v; want %s", name, code, ok, want)
		}
	}
	for _, name := range []string{"Logo", "RGB Strip 3", "Key: Fn", "Key: ", "Key: Number Pad 12", "A"} {
		if code, ok := KeyOfLED(name); ok {
			t.Errorf("KeyOfLED(%q) = %v, want none", name, code)
		}
	}
	// Every name in the table must be a key HIDI knows.
	for led, key := range ledKeys {
		if _, err := control.ParseCode(key); err != nil {
			t.Errorf("the light %q is mapped to %q: %v", led, key, err)
		}
	}
}
