package midi

import (
	"bytes"
	"errors"
	"testing"
)

// wire lists each message kind with its expected MIDI 1.0 encodings.
var wire = []struct {
	name  string
	msg   Message
	bytes []byte
	ump   uint32 // on group 0
	str   string
}{
	{"note on", NoteOn(0, 60, From7(100)), []byte{0x90, 60, 100}, 0x20903C64, "NoteOn ch=1 note=C3 velocity=100"},
	{"note off", NoteOff(15, 127, From7(64)), []byte{0x8F, 127, 64}, 0x208F7F40, "NoteOff ch=16 note=G8 velocity=64"},
	{"poly pressure", PolyPressure(2, 61, From7(1)), []byte{0xA2, 61, 1}, 0x20A23D01, "PolyPressure ch=3 note=C#3 pressure=1"},
	{"control change", ControlChange(1, 7, From7(127)), []byte{0xB1, 7, 127}, 0x20B1077F, "ControlChange ch=2 controller=7 value=127"},
	{"program change", ProgramChange(9, 42), []byte{0xC9, 42}, 0x20C92A00, "ProgramChange ch=10 program=42"},
	{"channel pressure", ChannelPressure(3, From7(99)), []byte{0xD3, 99}, 0x20D36300, "ChannelPressure ch=4 pressure=99"},
	{"pitch bend center", PitchBend(0, ValueCenter), []byte{0xE0, 0x00, 0x40}, 0x20E00040, "PitchBend ch=1 bend=+0"},
	{"pitch bend max", PitchBend(4, ValueMax), []byte{0xE4, 0x7F, 0x7F}, 0x20E47F7F, "PitchBend ch=5 bend=+8191"},
	{"pitch bend min", PitchBend(4, 0), []byte{0xE4, 0x00, 0x00}, 0x20E40000, "PitchBend ch=5 bend=-8192"},
}

func TestEncode(t *testing.T) {
	for _, tt := range wire {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.AppendBytes(nil); !bytes.Equal(got, tt.bytes) {
				t.Errorf("AppendBytes = % X, want % X", got, tt.bytes)
			}
			if got := tt.msg.UMP(0); got != tt.ump {
				t.Errorf("UMP(0) = 0x%08X, want 0x%08X", got, tt.ump)
			}
			if got := tt.msg.String(); got != tt.str {
				t.Errorf("String() = %q, want %q", got, tt.str)
			}
		})
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	for _, tt := range wire {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeBytes(tt.bytes)
			if err != nil || got != tt.msg {
				t.Errorf("DecodeBytes(% X) = %+v, %v; want %+v", tt.bytes, got, err, tt.msg)
			}
			got, group, err := DecodeUMP(tt.ump | 0x5<<24)
			if err != nil || got != tt.msg || group != 5 {
				t.Errorf("DecodeUMP = %+v, group %d, %v; want %+v, group 5", got, group, err, tt.msg)
			}
		})
	}
}

func TestAppendBytesExtends(t *testing.T) {
	buf := []byte{0xAA}
	buf = NoteOn(0, 60, From7(1)).AppendBytes(buf)
	buf = ProgramChange(0, 5).AppendBytes(buf)
	if want := []byte{0xAA, 0x90, 60, 1, 0xC0, 5}; !bytes.Equal(buf, want) {
		t.Errorf("buffer = % X, want % X", buf, want)
	}
}

func TestUMPGroup(t *testing.T) {
	if got := NoteOn(0, 60, From7(100)).UMP(0xF); got != 0x2F903C64 {
		t.Errorf("UMP(15) = 0x%08X, want 0x2F903C64", got)
	}
}

// A note on must never encode with velocity zero, which MIDI 1.0 reads as a
// note off.
func TestNoteOnVelocityNeverZero(t *testing.T) {
	for _, velocity := range []uint32{0, 1, 1<<25 - 1} {
		m := NoteOn(0, 60, velocity)
		if got := m.AppendBytes(nil); got[2] != 1 {
			t.Errorf("velocity %d encoded as %d, want 1", velocity, got[2])
		}
		if got := m.UMP(0) & 0x7F; got != 1 {
			t.Errorf("velocity %d encoded as %d in UMP, want 1", velocity, got)
		}
	}
}

func TestDecodeNoteOnZeroVelocityIsNoteOff(t *testing.T) {
	want := NoteOff(3, 60, 0)
	if got, err := DecodeBytes([]byte{0x93, 60, 0}); err != nil || got != want {
		t.Errorf("DecodeBytes = %+v, %v; want %+v", got, err, want)
	}
	if got, _, err := DecodeUMP(0x20933C00); err != nil || got != want {
		t.Errorf("DecodeUMP = %+v, %v; want %+v", got, err, want)
	}
}

func TestOutOfRangeFieldsAreMasked(t *testing.T) {
	m := Message{Kind: KindNoteOn, Channel: 0x13, Index: 0x80 | 60, Value: From7(100)}
	if got, want := m.AppendBytes(nil), []byte{0x93, 60, 100}; !bytes.Equal(got, want) {
		t.Errorf("AppendBytes = % X, want % X", got, want)
	}
}

func TestInvalidMessage(t *testing.T) {
	for _, m := range []Message{{}, {Kind: KindPitchBend + 1}} {
		if got := m.AppendBytes([]byte{1}); !bytes.Equal(got, []byte{1}) {
			t.Errorf("AppendBytes of %+v appended data: % X", m, got)
		}
		if got := m.UMP(0); got != 0 {
			t.Errorf("UMP of %+v = 0x%08X, want 0", m, got)
		}
		_ = m.String() // must not panic
	}
}

func TestDecodeBytesErrors(t *testing.T) {
	tests := []struct {
		name            string
		in              []byte
		notChannelVoice bool
	}{
		{"empty", nil, false},
		{"starts with data byte", []byte{0x40, 1, 2}, false},
		{"too short", []byte{0x90, 60}, false},
		{"too long", []byte{0xC0, 1, 2}, false},
		{"data byte too large", []byte{0x90, 60, 0x80}, false},
		{"system exclusive", []byte{0xF0, 1, 0xF7}, true},
		{"timing clock", []byte{0xF8}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeBytes(tt.in)
			if err == nil {
				t.Fatal("want an error")
			}
			if got := errors.Is(err, ErrNotChannelVoice); got != tt.notChannelVoice {
				t.Errorf("errors.Is(err, ErrNotChannelVoice) = %v, want %v (err: %v)", got, tt.notChannelVoice, err)
			}
		})
	}
}

func TestDecodeUMPErrors(t *testing.T) {
	if _, _, err := DecodeUMP(0x40903C64); !errors.Is(err, ErrNotChannelVoice) {
		t.Errorf("MIDI 2.0 packet: err = %v, want ErrNotChannelVoice", err)
	}
	if _, _, err := DecodeUMP(0x20103C64); err == nil || errors.Is(err, ErrNotChannelVoice) {
		t.Errorf("invalid status: err = %v, want a format error", err)
	}
}

func FuzzDecodeBytes(f *testing.F) {
	for _, tt := range wire {
		f.Add(tt.bytes)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		m, err := DecodeBytes(in)
		if err != nil {
			return
		}
		// Anything that decodes must encode to something that decodes to
		// the same message.
		again, err := DecodeBytes(m.AppendBytes(nil))
		if err != nil || again != m {
			t.Errorf("% X decoded to %+v, which re-decodes to %+v, %v", in, m, again, err)
		}
	})
}

func TestParseNoteRejectsAbsurdOctaves(t *testing.T) {
	for _, s := range []string{"C9223372036854775807", "C-9223372036854775808", "C9", "C-3"} {
		if n, err := ParseNote(s); err == nil {
			t.Errorf("ParseNote(%q) = %v, want an error", s, n)
		}
	}
}
