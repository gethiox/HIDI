//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"runtime"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// The widgets: what the window is put together from. Each draws itself
// at the place it is given and reports what the pointer did to it in the
// frame being drawn. Everything that takes a click lights up while the
// pointer is on it.

// pressID tells a button from every other one drawn: by its label and its
// place, which stay the same from frame to frame.
type pressID struct {
	label string
	x, y  float32
}

// colPushed is a button while it is held down: the ground of a button
// under the pointer, tinted towards the accent.
var colPushed = Mix(colPanelEdge, colAccent, 0.4)

// button draws a button and reports a click on it. It shows as pressed
// while the mouse button is held down on it.
func (k *kit) button(label string, x, y float32) (clickedNow bool, right float32) {
	return k.pushButton(label, x, y, false)
}

// repeatButton is a button that goes on clicking while it is held down,
// for one that steps a number: see [repeatDelay].
func (k *kit) repeatButton(label string, x, y float32) (clickedNow bool, right float32) {
	return k.pushButton(label, x, y, true)
}

func (k *kit) pushButton(label string, x, y float32, repeat bool) (clickedNow bool, right float32) {
	bw := width(k.sans, label, sizeText) + 20
	r := rl.NewRectangle(x, y, bw, 24)
	fire, lit := k.pushed(pressID{label, x, y}, r, repeat)
	over := k.hovered(r)
	if over {
		k.setCursor(rl.MouseCursorPointingHand)
	}
	switch {
	case lit:
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPushed))
	case over:
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanelEdge))
	default:
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanel))
		rl.DrawRectangleRoundedLinesEx(r, 0.35, 6, k.hair(), col(colPanelEdge))
	}
	k.text(k.sans, label, x+10, y+5, sizeText, col(colText))
	return fire, x + bw
}

// pushed follows the left mouse button on a button: it reports a click
// when the mouse button goes down on it, and, for one that repeats, the
// clicks it goes on making while held there. lit says that the button
// shows as pressed. A button the pointer slid off of while held does
// nothing until the pointer comes back.
func (k *kit) pushed(id pressID, r rl.Rectangle, repeat bool) (fire, lit bool) {
	if k.clicked(r) {
		now := time.Now()
		k.press, k.pressing = &press{since: now, fired: now}, id
		k.moving = true // the next frame shows it pressed, or lets it go
		return true, true
	}
	p := k.press
	if p == nil || k.pressing != id {
		return false, false
	}
	if !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
		k.press = nil
		return false, false
	}
	k.moving = true // until it is let go of: the frames show the clicks
	if !k.hovered(r) {
		return false, false
	}
	return p.step(time.Now(), repeat)
}

// dimButton draws a button that may have nothing to do at the moment: it
// is faint then, and takes no click.
func (k *kit) dimButton(label string, enabled bool, x, y float32) (clickedNow bool, right float32) {
	if enabled {
		return k.button(label, x, y)
	}
	bw := width(k.sans, label, sizeText) + 20
	rl.DrawRectangleRoundedLinesEx(rl.NewRectangle(x, y, bw, 24), 0.35, 6, k.hair(), col(colPanelEdge))
	k.text(k.sans, label, x+10, y+5, sizeText, col(colOff))
	return false, x + bw
}

// chip draws a small switch with a name, lit when it is on, and reports a
// click on it.
func (k *kit) chip(label string, on bool, x, y float32) bool {
	r := rl.NewRectangle(x, y, k.chipWidth(label), chipH)
	over := k.hovered(r)
	if over {
		k.setCursor(rl.MouseCursorPointingHand)
	}
	switch {
	case on && over:
		rl.DrawRectangleRounded(r, 0.4, 6, col(Mix(colAccent, colText, 0.3)))
		k.text(k.mono, label, x+7, y+3, sizeMono, col(colBar))
	case on:
		rl.DrawRectangleRounded(r, 0.4, 6, col(colAccent))
		k.text(k.mono, label, x+7, y+3, sizeMono, col(colBar))
	case over:
		rl.DrawRectangleRounded(r, 0.4, 6, col(colPanelEdge))
		k.text(k.mono, label, x+7, y+3, sizeMono, col(colText))
	default:
		rl.DrawRectangleRoundedLinesEx(r, 0.4, 6, k.hair(), col(colPanelEdge))
		k.text(k.mono, label, x+7, y+3, sizeMono, col(colFaint))
	}
	return k.clicked(r)
}

func (k *kit) chipWidth(label string) float32 { return width(k.mono, label, sizeMono) + 14 }

// toggle draws an on/off switch that fills r, in lit while it is on, and
// reports a click on area: the switch, and whatever words belong to it.
func (k *kit) toggle(r rl.Rectangle, on bool, lit RGB, area rl.Rectangle) bool {
	half := r.Height / 2
	back, knob := colOff, r.X+half
	if on {
		back, knob = lit, r.X+r.Width-half
	}
	if k.hovered(area) {
		k.setCursor(rl.MouseCursorPointingHand)
		back = Mix(back, colText, 0.22)
	}
	segments := int32(12)
	if r.Height >= 24 {
		segments = 16
	}
	rl.DrawRectangleRounded(r, 1, segments, col(back))
	rl.DrawCircle(int32(knob), int32(r.Y+half), float32(int(r.Height*0.35)), col(colText))
	return k.clicked(area)
}

// frozen draws an on/off switch that cannot be used here, faintly: a
// setting that does not apply on this system.
func (k *kit) frozen(r rl.Rectangle, on bool) {
	half := r.Height / 2
	knob := r.X + half
	if on {
		knob = r.X + r.Width - half
	}
	rl.DrawRectangleRoundedLinesEx(r, 1, 12, k.hair(), col(colPanelEdge))
	rl.DrawCircle(int32(knob), int32(r.Y+half), float32(int(r.Height*0.35)), col(colPanelEdge))
}

// pill draws the on/off switch of a setting and reports a click on it.
func (k *kit) pill(on bool, x, y float32) bool { return k.pillIn(on, colOn, x, y) }

// pillIn is a pill that is lit in a colour of its own while it is on.
func (k *kit) pillIn(on bool, lit RGB, x, y float32) bool {
	r := rl.NewRectangle(x, y, 40, 20)
	return k.toggle(r, on, lit, r)
}

// typing is the state of a text field: whether it is being typed in, and
// what is typed meanwhile.
type typing struct {
	// on says that the field takes what is typed.
	on bool
	// edit is the text as it is being typed, the cursor and the
	// selection, and was the value when the typing began.
	edit lineEdit
	was  string
	// dragging says that the mouse button went down in the field and
	// is held: the selection follows the pointer.
	dragging bool
	// scroll is how far a text longer than the field is moved to the
	// left, so that the cursor stays in view.
	scroll float32
}

// start begins the typing in a field, with its whole text selected, as
// a field does that was given a new value to change.
func (t *typing) start(value string) {
	*t = typing{on: true, edit: newLineEdit(value), was: value}
}

// fieldLook is how a text field looks and what it takes.
type fieldLook struct {
	// hint is shown, faintly, while the field is empty: the value that
	// is used then, for one.
	hint string
	// rules say what may be typed: any text or a number, how much.
	rules fieldRules
	// valid, if set, says whether the text can be taken. While it
	// cannot, it is red, and Enter does nothing. It is asked about after
	// every change.
	valid func(string) bool
	// live gives every change to the value at once, as it is typed; a
	// field that is cancelled then takes its old value back. Otherwise
	// the value changes only when the typing is accepted.
	live bool
	// locked shows the text without letting it be changed.
	locked bool
	// with are the places of what belongs to the field, its "Add"
	// button for one: a click there does not end the typing, and what
	// is there acts on what was typed (see [typing.text]).
	with []rl.Rectangle
}

// text returns what is typed, while the field is typed in.
func (t *typing) text() string { return t.edit.String() }

// fieldEnd is how the typing in a field ended in a frame.
type fieldEnd uint8

const (
	// typingOn: it goes on, or there was none.
	typingOn fieldEnd = iota
	// accepted: Enter took what was typed.
	accepted
	// cancelled: Escape, or a click elsewhere, dropped it.
	cancelled
)

// field draws a text field over r and, while it is being typed in, takes
// what is typed. A click on it begins the typing, puts the cursor where
// the click was, and a drag selects; the keys do what they do in every
// text field: the arrows, with Ctrl by word and with Shift selecting,
// Home and End, Backspace and Delete, Ctrl+A, Ctrl+C, Ctrl+X and Ctrl+V.
// Enter accepts what was typed and Escape drops it; so does a click
// elsewhere, which a button that belongs to the field, such as "Add",
// takes as accepting when the caller asks about the button first.
//
// It reports whether the value changed in this frame, and how the typing
// ended, if it did. The keys reach the window only while the keyboard is
// not HIDI's alone. A field under the settings sheet takes no keys, as it
// takes no clicks: what is typed belongs to the sheet.
func (k *kit) field(state *typing, r rl.Rectangle, value *string, look fieldLook) (changed bool, end fieldEnd) {
	if look.locked && state.on {
		state.on = false
	}
	over := !look.locked && k.hovered(r)
	if over {
		k.setCursor(rl.MouseCursorIBeam)
	}
	inner := r.Width - 16
	at := func(x float32) int { return k.caretAt(state.edit.text, x-r.X-8+state.scroll) }

	// The mouse: a click on the field begins the typing there or moves
	// the cursor, with Shift selecting to it; a click elsewhere drops it.
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && !k.deaf {
		switch {
		case over && !state.on:
			state.start(*value)
			state.edit.place(at(k.mouse().X), false)
			state.dragging = true
		case over:
			state.edit.place(at(k.mouse().X), shiftHeld())
			state.dragging = true
		case state.on && !k.onAny(look.with):
			return k.endTyping(state, value, look, cancelled)
		}
	}
	if state.dragging {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			state.edit.place(at(k.mouse().X), true)
			k.moving = true
		} else {
			state.dragging = false
		}
	}

	if state.on && !k.deaf {
		edited := k.typeInto(state, look)
		if edited && look.live && state.edit.String() != *value {
			*value, changed = state.edit.String(), true
		}
		switch {
		case rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyKpEnter):
			if look.valid == nil || look.valid(state.edit.String()) {
				c, e := k.endTyping(state, value, look, accepted)
				return changed || c, e
			}
		case rl.IsKeyPressed(rl.KeyEscape):
			c, e := k.endTyping(state, value, look, cancelled)
			return changed || c, e
		}
		k.moving = true // the caret blinks
	}

	// What is shown: the text being typed, or else the value; the hint
	// while there is neither.
	text := *value
	if state.on {
		text = state.edit.String()
	}
	ok := look.valid == nil || look.valid(text)
	rl.DrawRectangleRounded(r, 0.3, 6, col(colBar))
	edge := colPanelEdge
	switch {
	case state.on && !ok:
		edge = colInvalid
	case state.on:
		edge = colAccent
	case over:
		rl.DrawRectangleRounded(r, 0.3, 6, rl.NewColor(255, 255, 255, 20))
		edge = colFaint
	}
	rl.DrawRectangleRoundedLinesEx(r, 0.3, 6, k.hair(), col(edge))
	if text == "" {
		k.text(k.mono, look.hint, r.X+8, r.Y+5, sizeMono, colA(colFaint, 110))
	}
	if !state.on {
		state.scroll = 0
	} else {
		// The cursor stays in view.
		caret := width(k.mono, string(state.edit.text[:state.edit.cursor]), sizeMono)
		state.scroll = min(max(state.scroll, caret-inner+2), caret)
		state.scroll = max(state.scroll, 0)
	}
	k.clip(rl.NewRectangle(r.X+4, r.Y, r.Width-8, r.Height))
	x := r.X + 8 - state.scroll
	if state.on {
		if from, to := state.edit.selection(); from != to {
			left := width(k.mono, string(state.edit.text[:from]), sizeMono)
			right := width(k.mono, string(state.edit.text[:to]), sizeMono)
			rl.DrawRectangleRec(rl.NewRectangle(x+left, r.Y+4, right-left, 16), colA(colAccent, 90))
		}
	}
	ink := col(colText)
	switch {
	case !ok:
		ink = col(colInvalid)
	case look.locked:
		ink = col(colFaint)
	}
	if text != "" {
		k.text(k.mono, text, x, r.Y+5, sizeMono, ink)
	}
	if state.on && time.Now().UnixMilli()/500%2 == 0 {
		caret := width(k.mono, string(state.edit.text[:state.edit.cursor]), sizeMono)
		rl.DrawRectangleRec(rl.NewRectangle(x+caret, r.Y+5, 1, 14), col(colText))
	}
	k.unclip()
	return changed, typingOn
}

// endTyping ends the typing in a field: accepted, the value is what was
// typed; cancelled, it is what it was when the typing began.
func (k *kit) endTyping(state *typing, value *string, look fieldLook, how fieldEnd) (changed bool, end fieldEnd) {
	now := state.edit.String()
	if how == cancelled {
		now = state.was
		if !look.live {
			now = *value
		}
	}
	state.on, state.dragging = false, false
	if now != *value {
		*value, changed = now, true
	}
	return changed, how
}

// typeInto applies the keys of a frame to a field that is typed in, and
// reports whether its text changed.
func (k *kit) typeInto(state *typing, look fieldLook) (edited bool) {
	e := &state.edit
	ctrl, shift := ctrlHeld(), shiftHeld()
	windows := runtime.GOOS == "windows"
	pressed := func(key int32) bool { return rl.IsKeyPressed(key) || rl.IsKeyPressedRepeat(key) }
	for c := rl.GetCharPressed(); c > 0; c = rl.GetCharPressed() {
		if !ctrl && e.insert(string(c), look.rules) {
			edited = true
		}
	}
	switch {
	case pressed(rl.KeyLeft):
		e.left(ctrl, shift)
	case pressed(rl.KeyRight):
		e.right(ctrl, shift, windows)
	case pressed(rl.KeyHome):
		e.place(0, shift)
	case pressed(rl.KeyEnd):
		e.place(len(e.text), shift)
	case pressed(rl.KeyBackspace):
		edited = e.erase(true, ctrl, windows) || edited
	case pressed(rl.KeyDelete):
		edited = e.erase(false, ctrl, windows) || edited
	case ctrl && rl.IsKeyPressed(rl.KeyA):
		e.selectAll()
	case ctrl && rl.IsKeyPressed(rl.KeyC):
		if s := e.selected(); s != "" {
			rl.SetClipboardText(s)
		}
	case ctrl && rl.IsKeyPressed(rl.KeyX):
		if s := e.cut(); s != "" {
			rl.SetClipboardText(s)
			edited = true
		}
	case ctrl && rl.IsKeyPressed(rl.KeyV):
		edited = e.insert(rl.GetClipboardText(), look.rules) || edited
	}
	return edited
}

// onAny reports whether the pointer is on one of the places.
func (k *kit) onAny(places []rl.Rectangle) bool {
	for _, r := range places {
		if k.hovered(r) {
			return true
		}
	}
	return false
}

// caretAt returns the place in text nearest to x points from its start.
func (k *kit) caretAt(text []rune, x float32) int {
	prev := float32(0)
	for i := range text {
		next := width(k.mono, string(text[:i+1]), sizeMono)
		if x < (prev+next)/2 {
			return i
		}
		prev = next
	}
	return len(text)
}

// shiftHeld reports whether a Shift key is held.
func shiftHeld() bool {
	return rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
}

// checkbox draws a box that is ticked or not and reports a click on it or
// on the text beside it, which is room wide.
func (k *kit) checkbox(ticked bool, x, y, room float32) bool {
	box := rl.NewRectangle(x, y+1, 16, 16)
	area := rl.NewRectangle(x-4, y-2, room+8, 22)
	over := k.hot(area, 0.3)
	if ticked {
		rl.DrawRectangleRounded(box, 0.3, 4, col(colAccent))
		rl.DrawLineEx(rl.NewVector2(x+3.5, y+9), rl.NewVector2(x+7, y+12.5), 2, col(colBar))
		rl.DrawLineEx(rl.NewVector2(x+7, y+12.5), rl.NewVector2(x+12.5, y+5), 2, col(colBar))
	} else {
		edge := colOff
		if over {
			edge = colFaint
		}
		rl.DrawRectangleRoundedLinesEx(box, 0.3, 4, 1.5, col(edge))
	}
	return k.clicked(area)
}

// hot lays a light over something that can be clicked while the pointer
// is on it, and asks for the hand.
func (k *kit) hot(r rl.Rectangle, roundness float32) bool {
	if !k.hovered(r) {
		return false
	}
	k.setCursor(rl.MouseCursorPointingHand)
	rl.DrawRectangleRounded(r, roundness, 6, rl.NewColor(255, 255, 255, 26))
	return true
}

// stepper draws "label  [-] value [+]" and returns the change asked for.
func (k *kit) stepper(label, value string, x, y float32) (delta int, right float32) {
	k.text(k.sans, label, x, y+5, sizeText, col(colFaint))
	x += width(k.sans, label, sizeText) + 8
	minus, x2 := k.repeatButton("−", x, y)
	valueW := width(k.mono, "+000", sizeMono)
	k.text(k.mono, value, x2+8+(valueW-width(k.mono, value, sizeMono))/2, y+6, sizeMono, col(colText))
	plus, x3 := k.repeatButton("+", x2+valueW+16, y)
	switch {
	case minus:
		delta = -1
	case plus:
		delta = 1
	}
	return delta, x3
}

// saveButton draws the button that saves, a floppy disk, yellow while
// there is something to save, and reports a click on it.
func (k *kit) saveButton(unsaved bool, x, y float32) bool {
	r := rl.NewRectangle(x, y, 30, 24)
	ink := colOff
	if unsaved {
		ink = colUnsaved
	}
	if k.hovered(r) && unsaved {
		k.setCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanelEdge))
	} else {
		rl.DrawRectangleRoundedLinesEx(r, 0.35, 6, k.hair(), col(colPanelEdge))
	}
	drawFloppy(x+7, y+4, 1, col(ink))
	return unsaved && k.clicked(r)
}

// drawFloppy draws the floppy disk of the buttons that save, each of its
// sixteen points pixel wide and high.
func drawFloppy(x, y, pixel float32, c rl.Color) {
	for row := range 16 {
		for column := 0; column < 16; column++ {
			if !FloppyPoint(column, row) {
				continue
			}
			from := column
			for column+1 < 16 && FloppyPoint(column+1, row) {
				column++
			}
			rl.DrawRectangleRec(rl.NewRectangle(x+float32(from)*pixel, y+float32(row)*pixel, float32(column-from+1)*pixel, pixel), c)
		}
	}
}
