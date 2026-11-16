//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"fmt"
	"slices"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/midi"
)

// editor is the page on which a profile is changed.
type editor struct {
	doc     *ProfileDoc
	layout  string
	code    control.Code
	chosen  bool
	message string
	scroll  float32
	// held are the controls that were down at the last look, to notice a
	// new press: pressing a control of the device chooses it.
	held map[string]bool
}

// editProfile opens the page for a profile file.
func (w *window) editProfile(path string) {
	doc, err := OpenProfile(path)
	if err != nil {
		w.settings.message = fmt.Sprintf("%s cannot be changed here: %v", path, strings.ReplaceAll(err.Error(), "\n", " "))
		return
	}
	w.edit = &editor{doc: doc, layout: doc.Profile.Engine.Defaults.Layout, held: map[string]bool{}}
	if !slices.Contains(doc.Layouts(), w.edit.layout) {
		w.edit.layout = doc.Layouts()[0]
	}
}

// try makes a change and keeps what came of it for the message line.
func (e *editor) try(err error) {
	e.message = "Saved. A running HIDI takes this over within a few seconds."
	if err != nil {
		e.message = strings.ReplaceAll(err.Error(), "\n", " ")
	}
}

// stepper draws "label  [-] value [+]" and returns the change asked for.
func (w *window) stepper(label, value string, x, y float32) (delta int, right float32) {
	text(w.sans, label, x, y+5, sizeText, col(colFaint))
	x += width(w.sans, label, sizeText) + 8
	minus, x2 := w.button("−", x, y)
	valueW := width(w.mono, "+000", sizeMono)
	text(w.mono, value, x2+8+(valueW-width(w.mono, value, sizeMono))/2, y+6, sizeMono, col(colText))
	plus, x3 := w.button("+", x2+valueW+16, y)
	switch {
	case minus:
		delta = -1
	case plus:
		delta = 1
	}
	return delta, x3
}

// roleColour is the colour of a key cap for what the key does.
func roleColour(r Role) RGB {
	switch r.Kind {
	case RoleNote:
		switch {
		case int(r.Note)%12 == 0:
			return RGB{0x6b, 0x64, 0x1e} // every C
		case IsBlack(int(r.Note)):
			return RGB{0x24, 0x33, 0x66}
		}
		return RGB{0x1f, 0x5a, 0x38}
	case RoleCommand:
		return RGB{0x5a, 0x2f, 0x66}
	case RoleOther:
		return RGB{0x2a, 0x4a, 0x52}
	}
	return colBar
}

// drawEditor draws the page in place of the instruments.
func (w *window) drawEditor(state *ipc.Snapshot, area rl.Rectangle) {
	e := w.edit
	doc := e.doc
	if hovered(area) {
		e.scroll -= rl.GetMouseWheelMove() * 40
	}
	e.scroll = max(e.scroll, 0)
	rl.BeginScissorMode(int32(area.X), int32(area.Y), int32(area.Width), int32(area.Height))
	defer rl.EndScissorMode()
	x, y := float32(margin), area.Y+4-e.scroll
	right := area.Width - margin

	// The device that plays with this profile now, if any: its presses
	// choose a control.
	var device *ipc.Instrument
	if state != nil {
		for i := range state.Instruments {
			if state.Instruments[i].ProfileFile == doc.Path {
				device = &state.Instruments[i]
			}
		}
	}
	if device != nil {
		now := map[string]bool{}
		for _, b := range device.Buttons {
			if b.Down && b.Part == "" {
				now[b.Name] = true
				if !e.held[b.Name] {
					if code, err := control.ParseCode(b.Name); err == nil {
						e.code, e.chosen = code, true
					}
				}
			}
		}
		e.held = now
	}

	if back, end := w.button("← Back", x, y); back {
		w.edit = nil
		return
	} else {
		text(w.bold, doc.Profile.Name, end+14, y+2, sizeName, col(colText))
		w.drawPath(doc.Path, end+14+width(w.bold, doc.Profile.Name, sizeName)+14, y+6, right-end-28-width(w.bold, doc.Profile.Name, sizeName))
	}
	y += 34
	text(w.sans, e.message, x, y, sizeText, col(AnsiColour(93)))
	y += 26

	// What an instrument starts with.
	d := doc.Profile.Engine.Defaults
	sx := x
	for _, field := range []struct {
		label, key string
		value      int
		shown      string
	}{
		{"octave", "octave", d.Octave, fmt.Sprintf("%+d", d.Octave)},
		{"transpose", "transpose", d.Transpose, fmt.Sprintf("%+d", d.Transpose)},
		{"channel", "channel", int(d.Channel) + 1, fmt.Sprintf("%d", int(d.Channel)+1)},
		{"velocity", "velocity", int(d.Velocity), fmt.Sprintf("%d", d.Velocity)},
	} {
		delta, end := w.stepper(field.label, field.shown, sx, y)
		if delta != 0 {
			e.try(doc.SetDefault(field.key, field.value+delta))
		}
		sx = end + 22
	}
	y += 40

	// The layouts.
	text(w.sans, "layout", x, y+3, sizeText, col(colFaint))
	lx := x + width(w.sans, "layout", sizeText) + 10
	for _, name := range doc.Layouts() {
		chip := rl.NewRectangle(lx, y, w.chipWidth(name), chipH)
		if hovered(chip) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if name == e.layout {
			rl.DrawRectangleRounded(chip, 0.4, 6, col(colAccent))
			text(w.mono, name, lx+7, y+3, sizeMono, col(colBar))
		} else {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 1, col(colPanelEdge))
			text(w.mono, name, lx+7, y+3, sizeMono, col(colFaint))
		}
		if clicked(chip) {
			e.layout = name
		}
		lx += chip.Width + 6
	}
	y += 34

	// The controls: a keyboard as a keyboard, anything else as a list.
	if doc.Profile.Match.Type == control.DeviceKeyboard {
		y = w.editKeyboard(e, x, y, right-x)
	} else {
		y = w.editList(e, device, x, y, right-x)
	}
	y += 14
	y = w.editChosen(e, x, y, right-x)

	if content := y + e.scroll - area.Y + 12; content > area.Height {
		e.scroll = min(e.scroll, content-area.Height)
	} else {
		e.scroll = 0
	}
}

// editKeyboard draws the keyboard with what every key does, and chooses
// the key that is clicked.
func (w *window) editKeyboard(e *editor, x, y, inner float32) float32 {
	unit := min(inner/KeyboardUnitsW, 38)
	small := min(float32(10), unit*0.3)
	for _, key := range Keyboard() {
		code, err := control.ParseCode(key.Name)
		if err != nil {
			continue
		}
		role := e.doc.Role(e.layout, code)
		rect := rl.NewRectangle(x+key.X*unit+1, y+key.Y*unit+1, key.W*unit-2, key.H*unit-2)
		rl.DrawRectangleRounded(rect, 0.2, 4, col(roleColour(role)))
		if e.chosen && e.code == code {
			rl.DrawRectangleRoundedLinesEx(rect, 0.2, 4, 2, col(colText))
		}
		if hovered(rect) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if clicked(rect) {
			e.code, e.chosen = code, true
		}
		// The key's own name small at the top, what it does below.
		text(w.mono, key.Label, rect.X+3, rect.Y+2, small, col(colFaint))
		if role.Kind != RoleNone {
			label := role.Text
			for len(label) > 1 && width(w.mono, label, small+1) > rect.Width-4 {
				label = label[:len(label)-1]
			}
			text(w.mono, label, rect.X+3, rect.Y+rect.Height-small-4, small+1, col(colText))
		}
	}
	return y + unit*KeyboardUnitsH
}

// editList lists the buttons the profile binds and those the device has
// reported, and chooses the one that is clicked.
func (w *window) editList(e *editor, device *ipc.Instrument, x, y, inner float32) float32 {
	seen := map[control.Code]bool{}
	var codes []control.Code
	add := func(code control.Code) {
		if code.Kind == control.KindKey && !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	for _, l := range e.doc.Profile.Engine.Layouts {
		for _, b := range l.Bindings {
			if b.Source.Part == "" {
				add(b.Source.Code)
			}
		}
	}
	for _, b := range e.doc.Profile.Engine.Global {
		if b.Source.Part == "" {
			add(b.Source.Code)
		}
	}
	if device != nil {
		for _, b := range device.Buttons {
			if code, err := control.ParseCode(b.Name); err == nil && b.Part == "" {
				add(code)
			}
		}
	}
	slices.SortFunc(codes, func(a, b control.Code) int { return int(a.Num) - int(b.Num) })
	if len(codes) == 0 {
		text(w.sans, "No buttons yet. Press one on the device.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	cx, cy := x, y
	for _, code := range codes {
		role := e.doc.Role(e.layout, code)
		label := code.String()
		if role.Kind != RoleNone {
			label += "  " + role.Text
		}
		cw := w.chipWidth(label)
		if cx > x && cx+cw > x+inner {
			cx, cy = x, cy+chipH+6
		}
		chip := rl.NewRectangle(cx, cy, cw, chipH)
		rl.DrawRectangleRounded(chip, 0.4, 6, col(roleColour(role)))
		if e.chosen && e.code == code {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 2, col(colText))
		}
		if hovered(chip) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if clicked(chip) {
			e.code, e.chosen = code, true
		}
		text(w.mono, label, cx+7, cy+3, sizeMono, col(colText))
		cx += cw + 6
	}
	return cy + chipH + 6
}

// editChosen draws what the chosen control does and the ways to change
// it.
func (w *window) editChosen(e *editor, x, y, inner float32) float32 {
	if !e.chosen {
		text(w.sans, "Click a control, or press it on the device, to change what it does.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	role := e.doc.Role(e.layout, e.code)
	does := "does nothing"
	switch role.Kind {
	case RoleNote:
		does = "plays " + role.Text
	case RoleCommand:
		does = "runs " + role.Text
	case RoleOther:
		does = "sends " + role.Text + " (change that in the file)"
	}
	where := ""
	if role.Kind != RoleNone && role.Section.Layout == "" {
		where = ", in every layout"
	}
	text(w.bold, e.code.String(), x, y, sizeName, col(colText))
	text(w.sans, does+where, x+width(w.bold, e.code.String(), sizeName)+12, y+3, sizeText, col(colFaint))
	y += 32

	// A note, moved by semitones and octaves.
	text(w.sans, "note", x, y+5, sizeText, col(colFaint))
	bx := x + 70
	note := int(role.Note)
	if role.Kind != RoleNote {
		note = 60
	}
	for _, step := range []struct {
		label string
		delta int
	}{{"−12", -12}, {"−1", -1}, {"+1", 1}, {"+12", 12}} {
		if step.delta == 1 {
			shown := "—"
			if role.Kind == RoleNote {
				shown = role.Text
			}
			text(w.mono, shown, bx+6, y+6, sizeMono, col(colText))
			bx += width(w.mono, "C#-2", sizeMono) + 18
		}
		hit, end := w.button(step.label, bx, y)
		if hit {
			next := note + step.delta
			if role.Kind != RoleNote {
				next = 60 // the first click makes it a note at all
			}
			if next >= 0 && next <= 127 {
				e.try(e.doc.SetNote(e.layout, e.code, midi.Note(next)))
			}
		}
		bx = end + 6
	}
	y += 36

	// A command.
	text(w.sans, "command", x, y+3, sizeText, col(colFaint))
	cx, cy := x+70, y
	for _, command := range engine.Commands() {
		label := command.String()
		cw := w.chipWidth(label)
		if cx > x+70 && cx+cw > x+inner {
			cx, cy = x+70, cy+chipH+6
		}
		chip := rl.NewRectangle(cx, cy, cw, chipH)
		if hovered(chip) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if role.Kind == RoleCommand && role.Command == command {
			rl.DrawRectangleRounded(chip, 0.4, 6, col(colAccent))
			text(w.mono, label, cx+7, cy+3, sizeMono, col(colBar))
		} else {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 1, col(colPanelEdge))
			text(w.mono, label, cx+7, cy+3, sizeMono, col(colFaint))
		}
		if clicked(chip) {
			e.try(e.doc.SetCommand(e.layout, e.code, command))
		}
		cx += cw + 6
	}
	y = cy + chipH + 14

	if role.Kind != RoleNone {
		if hit, _ := w.button("Make it do nothing", x, y); hit {
			e.try(e.doc.Clear(e.layout, e.code))
		}
		y += 32
	}
	return y
}
