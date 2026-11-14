package control

import (
	"strings"
	"testing"
)

func TestCodeString(t *testing.T) {
	tests := []struct {
		code Code
		want string
	}{
		{Key(30), "KEY_A"},
		{Key(0x130), "BTN_SOUTH"}, // not the range marker BTN_GAMEPAD
		{Key(0x100), "BTN_0"},     // not the range marker BTN_MISC
		{Key(0x110), "BTN_LEFT"},  // not the range marker BTN_MOUSE
		{Key(113), "KEY_MUTE"},    // not the alias KEY_MIN_INTERESTING
		{Key(0x2f5), "KEY_0x2f5"}, // unnamed
		{Axis(0), "ABS_X"},
		{Axis(0x10), "ABS_HAT0X"},
		{Axis(0x3e), "ABS_0x3e"},
		{Relative(8), "REL_WHEEL"},
		{Code{}, "Code(0, 0x0)"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("%#v.String() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestParseCode(t *testing.T) {
	tests := []struct {
		in   string
		want Code
	}{
		{"KEY_A", Key(30)},
		{"key_a", Key(30)},
		{"BTN_SOUTH", Key(0x130)},
		{"BTN_A", Key(0x130)},       // alias
		{"BTN_GAMEPAD", Key(0x130)}, // range marker
		{"KEY_0x2f5", Key(0x2f5)},
		{"key_0X2F5", Key(0x2f5)},
		{"BTN_0x130", Key(0x130)},
		{"ABS_X", Axis(0)},
		{"ABS_0x3e", Axis(0x3e)},
		{"REL_WHEEL", Relative(8)},
	}
	for _, tt := range tests {
		got, err := ParseCode(tt.in)
		if err != nil {
			t.Errorf("ParseCode(%q) returned error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseCode(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestParseCodeErrors(t *testing.T) {
	for _, in := range []string{"", "A", "KEY_", "KEY_NOPE", "ABS_A", "KEY_X ", "KEY_0x", "KEY_0xZZ", "KEY_0x10000", "EV_KEY", "KEY_30"} {
		if got, err := ParseCode(in); err == nil {
			t.Errorf("ParseCode(%q) = %v, want an error", in, got)
		}
	}
}

// Every canonical name must parse back to the code it names, and every
// alias must resolve to a code that has a canonical name.
func TestNameTablesAreConsistent(t *testing.T) {
	for _, kind := range []Kind{KindKey, KindAxis, KindRelative} {
		if len(kind.names()) == 0 {
			t.Errorf("%s has no names", kind)
		}
		for num, name := range kind.names() {
			got, err := ParseCode(name)
			if want := (Code{kind, num}); err != nil || got != want {
				t.Errorf("ParseCode(%q) = %#v, %v; want %#v", name, got, err, want)
			}
		}
		for name, num := range kind.codes() {
			if _, ok := kind.names()[num]; !ok {
				t.Errorf("%s resolves to 0x%x, which has no canonical name", name, num)
			}
			if name != strings.ToUpper(name) {
				t.Errorf("%s is not upper case, so ParseCode cannot find it", name)
			}
		}
	}
}

func TestCodeTextRoundTrip(t *testing.T) {
	for _, code := range []Code{Key(30), Key(0x2f5), Axis(1), Relative(0)} {
		text, err := code.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", code, err)
		}
		var got Code
		if err := got.UnmarshalText(text); err != nil || got != code {
			t.Errorf("round trip of %v through %q = %v, %v", code, text, got, err)
		}
	}

	if _, err := (Code{}).MarshalText(); err == nil {
		t.Error("MarshalText of the zero Code succeeded, want an error")
	}
	code := Key(30)
	if err := code.UnmarshalText([]byte("nonsense")); err == nil || code != Key(30) {
		t.Errorf("UnmarshalText of bad input: err = %v, code = %v; want an error and an unchanged code", err, code)
	}
}

func TestCodeIsZero(t *testing.T) {
	if !(Code{}).IsZero() || Key(0).IsZero() {
		t.Error("IsZero is wrong for the zero Code or for Key(0)")
	}
}
