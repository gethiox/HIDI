package gui

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
