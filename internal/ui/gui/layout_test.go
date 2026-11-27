package gui

import "testing"

func TestFlow(t *testing.T) {
	f := newFlow(10, 110, 5, 20, 6)
	if !f.empty() {
		t.Error("a new flow is not empty")
	}
	type at struct{ x, y float32 }
	var got []at
	for _, wide := range []float32{40, 40, 40, 200, 10} {
		x, y := f.put(wide)
		got = append(got, at{x, y})
	}
	// Two fit a line of a hundred; the third begins the next; one wider
	// than a line gets a line of its own, and so does what follows it.
	want := []at{{10, 5}, {56, 5}, {10, 25}, {10, 45}, {10, 65}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("thing %d is at %v, want %v", i, got[i], want[i])
		}
	}
	if f.lines != 4 || f.empty() {
		t.Errorf("%d lines", f.lines)
	}
}

func TestWrap(t *testing.T) {
	// Every letter is one wide.
	measure := func(s string) float32 { return float32(len(s)) }
	got := Wrap("one two three four\nfive incomprehensibilities six", 9, measure)
	want := []string{"one two", "three", "four", "five", "incomprehensibilities", "six"}
	if len(got) != len(want) {
		t.Fatalf("lines: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %q, want %q", i, got[i], want[i])
		}
	}
	if got := Wrap("", 9, measure); len(got) != 1 || got[0] != "" {
		t.Errorf("nothing wraps to %q", got)
	}
}

func TestBeside(t *testing.T) {
	// Room on all sides: below and to the right.
	if x, y := Beside(100, 100, 50, 20, 400, 300); x != 116 || y != 116 {
		t.Errorf("in the open: %v, %v", x, y)
	}
	// At the bottom edge it goes above the point, at the right edge it
	// is moved in, and it never leaves at the top or the left.
	if _, y := Beside(100, 290, 50, 20, 400, 300); y != 262 {
		t.Errorf("at the bottom: %v", y)
	}
	if x, _ := Beside(390, 100, 50, 20, 400, 300); x != 350 {
		t.Errorf("at the right: %v", x)
	}
	if x, y := Beside(5, 5, 500, 400, 400, 300); x != 0 || y != 0 {
		t.Errorf("larger than the area: %v, %v", x, y)
	}
}
