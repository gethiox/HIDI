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
