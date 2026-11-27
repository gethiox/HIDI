//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// What lies over everything else in the window: a note about what the
// pointer rests on, and a question that has to be answered before the
// window does anything else. Both are asked for while the frame is drawn
// and drawn when it ends, on top.

// hintDelay is how long the pointer has to rest on something before the
// note about it shows.
const hintDelay = 600 * time.Millisecond

// hints is what the kit keeps for the notes.
type hints struct {
	// want is the note asked for in the frame being drawn, by what the
	// pointer is on; empty if it is on nothing that has one.
	want string
	// text is the note the pointer has been resting on, and since when.
	text  string
	since time.Time
}

// hint says what the note about r is: it shows when the pointer has
// rested on r for a moment, beside the pointer, and goes when the pointer
// moves on. A widget calls it wherever it draws itself; text may run to
// several lines.
func (k *kit) hint(r rl.Rectangle, text string) {
	if text != "" && k.hovered(r) {
		k.hints.want = text
	}
}

// Nothing in the window has a note yet: that is to come, and this keeps
// the tools that look for code nobody calls from taking hint for some.
var _ = (*kit).hint

// drawHint draws the note the frame asked for, once the pointer has
// rested long enough.
func (k *kit) drawHint(now time.Time) {
	h := &k.hints
	want := h.want
	h.want = ""
	moved := k.dragged()
	if want == "" {
		h.text = ""
		return
	}
	// Another note, or a pointer on its way: the waiting starts anew.
	if want != h.text || (now.Sub(h.since) < hintDelay && (moved.X != 0 || moved.Y != 0)) {
		h.text, h.since = want, now
	}
	if now.Sub(h.since) < hintDelay {
		k.moving = true // frames go on until it shows
		return
	}
	const pad, room, lineH = 8, 300, 18
	lines := Wrap(h.text, room, func(s string) float32 { return width(k.sans, s, sizeText-1) })
	wide := float32(0)
	for _, line := range lines {
		wide = max(wide, width(k.sans, line, sizeText-1))
	}
	wide, high := wide+2*pad, float32(len(lines))*lineH+2*pad-4
	p := k.mouse()
	x, y := Beside(p.X, p.Y, wide, high, k.wide, k.high)
	box := rl.NewRectangle(x, y, wide, high)
	rl.DrawRectangleRounded(box, 0.25, 6, col(colBar))
	rl.DrawRectangleRoundedLinesEx(box, 0.25, 6, k.hair(), col(colFaint))
	for i, line := range lines {
		k.text(k.sans, line, x+pad, y+pad-2+float32(i)*lineH, sizeText-1, col(colText))
	}
}

// question is something the window asks before it goes on.
type question struct {
	text string
	// yes is what the button says that does it, and do what it does.
	yes string
	do  func()
	// fresh says that the question was asked in the frame being drawn:
	// the click that asked it is not an answer.
	fresh bool
}

// confirm asks a question over everything else in the window. Until it is
// answered nothing else in the window takes the pointer. The button that
// says yes does do; "Cancel", a click beside the question, and Escape do
// nothing.
func (k *kit) confirm(text, yes string, do func()) {
	k.asking = &question{text: text, yes: yes, do: do, fresh: true}
}

// drawQuestion draws the question that waits for an answer, if any, and
// takes the answer.
func (k *kit) drawQuestion() {
	q := k.asking
	if q == nil {
		return
	}
	// The question itself takes the pointer, from the frame after it
	// was asked.
	k.deaf, k.blocked = q.fresh, false
	defer func() { k.deaf, k.blocked = false, k.asking != nil }()
	q.fresh = false

	all := rl.NewRectangle(0, 0, k.wide, k.high)
	rl.DrawRectangleRec(all, rl.NewColor(0, 0, 0, 150))
	const pad, lineH = 20, 20
	room := min(420, k.wide-2*pad-32)
	lines := Wrap(q.text, room, func(s string) float32 { return width(k.sans, s, sizeText) })
	wide, high := room+2*pad, float32(len(lines))*lineH+2*pad+40
	box := rl.NewRectangle((k.wide-wide)/2, (k.high-high)/2, wide, high)
	rl.DrawRectangleRounded(box, 0.08, 8, col(colPanel))
	rl.DrawRectangleRoundedLinesEx(box, 0.08, 8, k.hair(), col(colFaint))
	for i, line := range lines {
		k.text(k.sans, line, box.X+pad, box.Y+pad+float32(i)*lineH, sizeText, col(colText))
	}
	by := box.Y + box.Height - pad - 24
	cancelW := width(k.sans, "Cancel", sizeText) + 20
	yesW := width(k.sans, q.yes, sizeText) + 20
	cancel, _ := k.button("Cancel", box.X+box.Width-pad-cancelW, by)
	yes, _ := k.button(q.yes, box.X+box.Width-pad-cancelW-8-yesW, by)
	switch {
	case yes:
		k.asking = nil
		q.do()
	case cancel, rl.IsKeyPressed(rl.KeyEscape), rl.IsMouseButtonPressed(rl.MouseButtonLeft) && k.hovered(all) && !k.hovered(box):
		k.asking = nil
	}
}
