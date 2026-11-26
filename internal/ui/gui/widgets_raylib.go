//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// The widgets: what the window is put together from. Each draws itself
// at the place it is given and reports what the pointer did to it in the
// frame being drawn. Everything that takes a click lights up while the
// pointer is on it.

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
		k.press = &press{id: id, since: now, fired: now}
		k.moving = true // the next frame shows it pressed, or lets it go
		return true, true
	}
	p := k.press
	if p == nil || p.id != id {
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

// typing is what a text field keeps between frames.
type typing struct {
	// on says that the field takes what is typed.
	on bool
}

// fieldLook is how a text field looks and what it takes.
type fieldLook struct {
	// hint is shown, faintly, while the field is empty.
	hint string
	// most is the most characters the field holds.
	most int
	// accept says which characters can be typed; nil accepts all that
	// can be printed.
	accept func(rune) bool
	// locked shows the text without letting it be changed.
	locked bool
}

// field draws a text field over r and, while it is being typed in, takes
// what is typed. A click on it begins the typing and a click elsewhere
// ends it. It reports whether the text changed in this frame, and the key
// that asks to be done with it, rl.KeyEnter or rl.KeyEscape, or 0.
//
// The keys reach the window only while the keyboard is not HIDI's alone.
func (k *kit) field(state *typing, r rl.Rectangle, value *string, look fieldLook) (changed bool, key int32) {
	free := !look.locked
	if !free {
		state.on = false
	}
	over := free && k.hovered(r)
	if over {
		k.setCursor(rl.MouseCursorIBeam)
	}
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		state.on = over
	}
	if state.on {
		before := *value
		for c := rl.GetCharPressed(); c > 0; c = rl.GetCharPressed() {
			if c < 0x20 || c == 0x7f || len([]rune(*value)) >= look.most {
				continue
			}
			if look.accept == nil || look.accept(c) {
				*value += string(c)
			}
		}
		if (rl.IsKeyPressed(rl.KeyBackspace) || rl.IsKeyPressedRepeat(rl.KeyBackspace)) && *value != "" {
			runes := []rune(*value)
			*value = string(runes[:len(runes)-1])
		}
		changed = *value != before
		switch {
		case rl.IsKeyPressed(rl.KeyEnter):
			key = rl.KeyEnter
		case rl.IsKeyPressed(rl.KeyEscape):
			key = rl.KeyEscape
		}
		k.moving = true // the caret blinks
	}

	rl.DrawRectangleRounded(r, 0.3, 6, col(colBar))
	edge := colPanelEdge
	switch {
	case state.on:
		edge = colAccent
	case over:
		rl.DrawRectangleRounded(r, 0.3, 6, rl.NewColor(255, 255, 255, 20))
		edge = colFaint
	}
	rl.DrawRectangleRoundedLinesEx(r, 0.3, 6, k.hair(), col(edge))
	switch {
	case *value == "":
		k.text(k.mono, look.hint, r.X+8, r.Y+5, sizeMono, colA(colFaint, 110))
	case free:
		k.text(k.mono, *value, r.X+8, r.Y+5, sizeMono, col(colText))
	default:
		k.text(k.mono, *value, r.X+8, r.Y+5, sizeMono, col(colFaint))
	}
	if state.on && time.Now().UnixMilli()/500%2 == 0 {
		rl.DrawRectangleRec(rl.NewRectangle(r.X+9+width(k.mono, *value, sizeMono), r.Y+5, 1, 14), col(colText))
	}
	return changed, key
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
