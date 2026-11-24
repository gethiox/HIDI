package gui

import "strings"

// flow sets things of different widths one after the other from left to
// right, and begins a new line when the next one does not fit: the way
// words fill a paragraph.
type flow struct {
	left, right float32 // the edges of the lines
	gap, step   float32 // between two things, and from one line to the next
	x, y        float32 // where the next thing goes
	lines       int     // how many lines have something on them
}

// newFlow begins at left, y.
func newFlow(left, right, y, step, gap float32) *flow {
	return &flow{left: left, right: right, gap: gap, step: step, x: left, y: y}
}

// put returns where a thing that is wide goes, and moves on. The first
// thing of a line goes there however wide it is.
func (f *flow) put(wide float32) (x, y float32) {
	if f.x > f.left && f.x+wide > f.right {
		f.x, f.y = f.left, f.y+f.step
		f.lines++
	}
	if f.lines == 0 {
		f.lines = 1
	}
	x, y = f.x, f.y
	f.x += wide + f.gap
	return x, y
}

// empty reports whether nothing was put yet.
func (f *flow) empty() bool { return f.lines == 0 }

// Wrap breaks a text into lines no wider than room, between words. A
// word wider than room has a line of its own. measure says how wide a
// text is.
func Wrap(text string, room float32, measure func(string) float32) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case measure(line+" "+word) <= room:
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// Beside returns where a box of the given size goes so that it stands
// below and to the right of a point, and is moved back into an area of
// the given size where it would leave it: above the point when there is
// no room below, and to the left as far as needed.
func Beside(px, py, wide, high, areaW, areaH float32) (x, y float32) {
	const gap = 16
	x, y = px+gap, py+gap
	if y+high > areaH {
		y = py - gap/2 - high
	}
	if x+wide > areaW {
		x = areaW - wide
	}
	return max(x, 0), max(y, 0)
}
