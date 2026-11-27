package gui

import "testing"

func TestNumberSpec(t *testing.T) {
	percent := numberSpec{lo: 0, hi: 95, unit: "%"}
	for _, tt := range []struct {
		text string
		v    float64
		ok   bool
	}{{"5", 5, true}, {"2,5", 2.5, true}, {"2.5", 2.5, true}, {"95", 95, true}, {"95.5", 0, false}, {"-1", 0, false}, {"", 0, false}, {".", 0, false}} {
		if v, ok := percent.parse(tt.text); ok != tt.ok || v != tt.v {
			t.Errorf("parse(%q) = %v, %v; want %v, %v", tt.text, v, ok, tt.v, tt.ok)
		}
	}
	octave := numberSpec{lo: -10, hi: 10, whole: true, plus: true}
	if got := octave.format(2); got != "+2" {
		t.Errorf("an octave: %q", got)
	}
	if v, ok := octave.parse("+2"); !ok || v != 2 {
		t.Errorf("an octave typed with its sign: %v %v", v, ok)
	}
	if _, ok := octave.parse("1.5"); ok {
		t.Error("a whole number took a fraction")
	}
	if got := percent.format(2.5); got != "2.5" {
		t.Errorf("a fraction: %q", got)
	}
	if got := percent.format(0.1 * 100); got != "10" {
		t.Errorf("ten: %q", got)
	}
	if r := octave.rules(); r.kind != wholeNumber || !r.signed {
		t.Errorf("the rules of an octave: %+v", r)
	}
}

func TestNoteName(t *testing.T) {
	for text, want := range map[string]int{"C3": 60, "c3": 60, "F#-1": 18, "f#-1": 18, "Bb2": 58, "G8": 127} {
		if n, ok := noteName(text); !ok || int(n) != want {
			t.Errorf("%q: %v, %v; want %d", text, n, ok, want)
		}
	}
	for _, text := range []string{"60", "H3", "C9", "C", "#3", ""} {
		if _, ok := noteName(text); ok {
			t.Errorf("%q was taken for a note", text)
		}
	}
}

func TestStepZone(t *testing.T) {
	for _, tt := range []struct {
		from float64
		by   int
		want float64
	}{{0, 1, 5}, {0, -1, 0}, {2.5, 1, 5}, {2.5, -1, 0}, {5, -1, 0}, {5, 1, 10}, {95, 1, 95}, {93, 1, 95}, {12.5, -1, 10}} {
		if got := stepZone(tt.from, tt.by); got != tt.want {
			t.Errorf("stepZone(%v, %d) = %v, want %v", tt.from, tt.by, got, tt.want)
		}
	}
}

// The point an axis triggers at is neither of its ends, and a dead zone
// leaves some of the travel.
func TestSpecRanges(t *testing.T) {
	for _, text := range []string{"0", "100"} {
		if _, ok := thresholdSpec.parse(text); ok {
			t.Errorf("a trigger point of %s%% was taken", text)
		}
	}
	if _, ok := thresholdSpec.parse("50"); !ok {
		t.Error("the middle was not taken for a trigger point")
	}
	if _, ok := zoneSpec.parse("100"); ok {
		t.Error("a dead zone of the whole travel was taken")
	}
}
