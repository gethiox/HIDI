//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

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
	scroll  glide
	content float32 // the height of the page when it was last drawn
	// touched says that what is shown changed in this frame; the running
	// HIDI is then given it to play with.
	touched bool
	// adding says that a layout is being added, and newName is its name
	// as far as it is typed.
	adding  bool
	newName string
	// held are the controls that were down at the last look, to notice a
	// new press: pressing a control of the device chooses it.
	held map[string]bool
	// looked is when the file was last compared with what is shown.
	looked time.Time
}

// editProfile opens the page for a profile file.
func (w *window) editProfile(path string) {
	// A page that was left with changes on it is as it was left.
	if e := w.docs[path]; e != nil {
		w.edit = e
		return
	}
	doc, err := OpenProfile(path)
	if err != nil {
		// Said on the settings sheet, which is opened for it: the page
		// that would say it does not open.
		w.settings.message = fmt.Sprintf("%s cannot be changed here: %v", path, strings.ReplaceAll(err.Error(), "\n", " "))
		w.gear.set(true, time.Now())
		w.settings.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
		return
	}
	w.edit = &editor{doc: doc, layout: doc.DefaultLayout(), held: map[string]bool{}}
	if w.docs == nil {
		w.docs = map[string]*editor{}
	}
	w.docs[path] = w.edit
}

// try takes the outcome of a change: what went wrong goes to the message
// line, and what is shown now is to be played with.
func (e *editor) try(err error) {
	e.message, e.touched = "", true
	if err != nil {
		e.message = strings.ReplaceAll(err.Error(), "\n", " ")
	}
	// The layout that is shown may be gone, by a change or by taking one
	// back.
	if !slices.Contains(e.doc.Layouts(), e.layout) {
		e.layout = e.doc.DefaultLayout()
	}
}

// preview gives the running HIDI the profile as it is shown, to play
// with, if it differs from the file; otherwise the file counts again.
// The asking is done beside the drawing, the newest text for each file
// last.
func (w *window) preview(doc *ProfileDoc) {
	text := ""
	if doc.Dirty() {
		text = string(doc.Text())
	}
	w.tryMu.Lock()
	if w.trying == nil {
		w.trying = map[string]string{}
	}
	w.trying[doc.Path] = text
	idle := !w.tryBusy
	w.tryBusy = true
	w.tryMu.Unlock()
	if !idle {
		return // the one at work takes it along
	}
	go func() {
		for {
			w.tryMu.Lock()
			var path, text string
			found := false
			for path, text = range w.trying {
				found = true
				break
			}
			if !found {
				w.tryBusy = false
				w.tryMu.Unlock()
				return
			}
			delete(w.trying, path)
			w.tryMu.Unlock()
			ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
			w.hidi.Preview(ctx, path, text) // no HIDI, nothing to play with
			cancel()
		}
	}()
}

// dimButton draws a button that may have nothing to do at the moment: it
// is faint then, and takes no click.
func (w *window) dimButton(label string, enabled bool, x, y float32) (clickedNow bool, right float32) {
	if enabled {
		return w.button(label, x, y)
	}
	bw := width(w.sans, label, sizeText) + 20
	rl.DrawRectangleRoundedLinesEx(rl.NewRectangle(x, y, bw, 24), 0.35, 6, 1, col(colPanelEdge))
	text(w.sans, label, x+10, y+5, sizeText, col(colOff))
	return false, x + bw
}

// saveButton draws the button that saves, a floppy disk, yellow while
// there is something to save, and reports a click on it.
func (w *window) saveButton(unsaved bool, x, y float32) bool {
	r := rl.NewRectangle(x, y, 30, 24)
	ink := colOff
	if unsaved {
		ink = colUnsaved
	}
	if hovered(r) && unsaved {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanelEdge))
	} else {
		rl.DrawRectangleRoundedLinesEx(r, 0.35, 6, 1, col(colPanelEdge))
	}
	// The disk: a square with a corner cut off, the shutter at the top
	// and the label below.
	bx, by := x+7, y+4
	rl.DrawRectangleRounded(rl.NewRectangle(bx, by, 16, 16), 0.2, 4, col(ink))
	rl.DrawTriangle(rl.NewVector2(bx+16, by), rl.NewVector2(bx+12, by), rl.NewVector2(bx+16, by+4), col(colBackground))
	rl.DrawRectangleRec(rl.NewRectangle(bx+4, by+1.5, 7, 5), col(colBackground))
	rl.DrawRectangleRec(rl.NewRectangle(bx+8, by+2.5, 2, 3), col(ink))
	rl.DrawRectangleRec(rl.NewRectangle(bx+3, by+9.5, 10, 5), col(colBackground))
	return unsaved && clicked(r)
}

// freeLayoutName returns a name no layout of the profile has.
func freeLayoutName(doc *ProfileDoc) string {
	for n := len(doc.Layouts()) + 1; ; n++ {
		if name := fmt.Sprintf("Layout %d", n); !slices.Contains(doc.Layouts(), name) {
			return name
		}
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
		return NoteColour(int(r.Note))
	case RoleCommand:
		return colCommandKey
	case RoleOther, RoleController, RoleBend:
		return colOtherKey
	case RolePass:
		return colPassKey
	}
	return colBar
}

// drawEditor draws the page in place of the instruments.
func (w *window) drawEditor(state *ipc.Snapshot, area rl.Rectangle) {
	e := w.edit
	doc := e.doc
	// The file may be changed in an editor meanwhile; the page follows.
	if now := time.Now(); now.Sub(e.looked) > settingsRefresh {
		e.looked = now
		if changed, err := doc.Reload(); err != nil {
			e.message = strings.ReplaceAll(err.Error(), "\n", " ")
		} else if changed {
			e.message = "The file was changed elsewhere; this is how it is now."
			if !slices.Contains(doc.Layouts(), e.layout) {
				e.layout = doc.DefaultLayout()
			}
		}
	}
	// What a change in this frame leaves is given to HIDI when the frame
	// is drawn.
	defer func() {
		if e.touched {
			e.touched = false
			w.preview(doc)
		}
	}()
	// The keys that take a change back, make it again, and save. They
	// reach the window only while the keyboard is not HIDI's alone.
	if !inputOff && !e.adding && (rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)) {
		shift := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
		switch {
		case rl.IsKeyPressed(rl.KeyZ) && !shift:
			if doc.Undo() {
				e.try(nil)
			}
		case rl.IsKeyPressed(rl.KeyZ) && shift, rl.IsKeyPressed(rl.KeyY):
			if doc.Redo() {
				e.try(nil)
			}
		case rl.IsKeyPressed(rl.KeyS):
			if doc.Dirty() {
				e.save()
			}
		}
	}
	e.scroll.Target -= wheel(area) * 60
	e.scroll.Clamp(e.content - area.Height)
	if e.scroll.Step(w.dt) {
		w.moving = true
	}
	scrolled := float32(int(e.scroll.At))
	beginClip(area)
	defer endClip()
	x, y := float32(margin), area.Y+4-scrolled
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

	back, end := w.button("← Back", x, y)
	if back {
		// The page stays as it is, with what is not saved, for the next
		// time it is opened; a dot on the way to it says so.
		w.edit = nil
		return
	}
	// At the right: taking back, making again, and saving.
	tools := right - 30
	if w.saveButton(doc.Dirty(), tools, y) {
		e.save()
	}
	redoW := width(w.sans, "Redo", sizeText) + 20
	tools -= redoW + 8
	if hit, _ := w.dimButton("Redo", doc.CanRedo(), tools, y); hit && doc.Redo() {
		e.try(nil)
	}
	undoW := width(w.sans, "Undo", sizeText) + 20
	tools -= undoW + 6
	if hit, _ := w.dimButton("Undo", doc.CanUndo(), tools, y); hit && doc.Undo() {
		e.try(nil)
	}
	text(w.bold, doc.Profile.Name, end+14, y+2, sizeName, col(colText))
	w.drawPath(doc.Path, end+14+width(w.bold, doc.Profile.Name, sizeName)+14, y+6, tools-end-42-width(w.bold, doc.Profile.Name, sizeName))
	y += 34
	// One line for what is to be said: what went wrong, or else how
	// things stand with saving.
	status := e.message
	switch {
	case status != "":
	case doc.Elsewhere:
		status = "Not saved, and the file was changed elsewhere meanwhile: saving writes over that."
	case doc.Dirty():
		status = "Not saved. A running HIDI plays with it as it is shown here; the file is as it was."
	}
	text(w.sans, status, x, y, sizeText, col(colUnsaved))
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

	y = w.editLayouts(e, x, y, right)

	// The controls: a keyboard as a keyboard, anything else as a list.
	if doc.Profile.Match.Type == control.DeviceKeyboard {
		y = w.editKeyboard(e, x, y, right-x)
	} else {
		y = w.editList(e, device, x, y, right-x)
	}
	y += 14
	y = w.editChosen(e, x, y, right-x)

	e.content = y + scrolled - area.Y + 12
}

// save writes the profile and says what came of it.
func (e *editor) save() {
	e.message = ""
	if err := e.doc.Save(); err != nil {
		e.message = strings.ReplaceAll(err.Error(), "\n", " ")
	}
}

// editLayouts draws the layouts: which one is shown, which one an
// instrument starts with, and the buttons that add one, remove the one
// shown, and move it in the row the player steps through.
func (w *window) editLayouts(e *editor, x, y, right float32) float32 {
	doc := e.doc
	text(w.sans, "layouts", x, y+3, sizeText, col(colFaint))
	start := x + width(w.sans, "layouts", sizeText) + 12
	lx := start
	layouts := doc.Layouts()
	first := doc.DefaultLayout()
	for _, name := range layouts {
		// The name, a box that makes the layout the first one played,
		// and behind the one that is, the word for it.
		note := ""
		if name == first {
			note = "default"
		}
		itemW := w.chipWidth(name) + 6 + 16 + 14
		if note != "" {
			itemW += width(w.sans, note, sizeText-2) + 6
		}
		if lx > start && lx+itemW > right {
			lx, y = start, y+chipH+10
		}
		if w.chip(name, name == e.layout, lx, y) {
			e.layout = name
		}
		bx := lx + w.chipWidth(name) + 6
		if w.checkbox(name == first, bx, y+1, 16) && name != first {
			e.try(doc.SetDefaultLayout(name))
		}
		if note != "" {
			text(w.sans, note, bx+22, y+4, sizeText-2, col(colFaint))
		}
		lx += itemW
	}
	y += chipH + 12

	// What can be done with the layout that is shown.
	lx = start
	at := slices.Index(layouts, e.layout)
	if hit, end := w.dimButton("← Earlier", at > 0, lx, y); hit {
		e.try(doc.MoveLayout(e.layout, -1))
	} else {
		lx = end + 6
	}
	if hit, end := w.dimButton("Later →", at >= 0 && at < len(layouts)-1, lx, y); hit {
		e.try(doc.MoveLayout(e.layout, 1))
	} else {
		lx = end + 6
	}
	if hit, end := w.dimButton("Remove \""+e.layout+"\"", len(layouts) > 1, lx, y); hit {
		e.try(doc.RemoveLayout(e.layout))
	} else {
		lx = end + 18
	}
	if !e.adding {
		if hit, _ := w.button("+ Add a layout", lx, y); hit {
			e.adding, e.newName = true, freeLayoutName(doc)
		}
		return y + 38
	}
	// The name of the new layout: there is one to begin with, which can
	// be typed over while the keyboard is not HIDI's alone.
	for r := rl.GetCharPressed(); r > 0; r = rl.GetCharPressed() {
		if r >= 0x20 && len([]rune(e.newName)) < 32 {
			e.newName += string(r)
		}
	}
	if (rl.IsKeyPressed(rl.KeyBackspace) || rl.IsKeyPressedRepeat(rl.KeyBackspace)) && e.newName != "" {
		name := []rune(e.newName)
		e.newName = string(name[:len(name)-1])
	}
	field := rl.NewRectangle(lx, y, max(width(w.mono, e.newName, sizeMono)+24, 140), 24)
	rl.DrawRectangleRounded(field, 0.3, 6, col(colBar))
	rl.DrawRectangleRoundedLinesEx(field, 0.3, 6, 1, col(colAccent))
	text(w.mono, e.newName, field.X+8, y+5, sizeMono, col(colText))
	if time.Now().UnixMilli()/500%2 == 0 {
		rl.DrawRectangleRec(rl.NewRectangle(field.X+9+width(w.mono, e.newName, sizeMono), y+5, 1, 14), col(colText))
	}
	w.moving = true // the mark blinks
	add, end := w.button("Add", field.X+field.Width+8, y)
	cancel, _ := w.button("Cancel", end+6, y)
	switch {
	case add || rl.IsKeyPressed(rl.KeyEnter):
		name := strings.TrimSpace(e.newName)
		e.try(doc.AddLayout(name))
		if slices.Contains(doc.Layouts(), name) {
			e.layout, e.adding = name, false
		}
	case cancel || rl.IsKeyPressed(rl.KeyEscape):
		e.adding = false
	}
	return y + 38
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

// editList lists the buttons and axes the profile binds and those the
// device has reported, and chooses the one that is clicked.
func (w *window) editList(e *editor, device *ipc.Instrument, x, y, inner float32) float32 {
	seen := map[control.Code]bool{}
	var codes []control.Code
	add := func(code control.Code) {
		if !seen[code] {
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
		for _, a := range device.Axes {
			if code, err := control.ParseCode(a.Name); err == nil && a.Part == "" {
				add(code)
			}
		}
	}
	// The buttons first, then the axes.
	slices.SortFunc(codes, func(a, b control.Code) int {
		if a.Kind != b.Kind {
			return int(a.Kind) - int(b.Kind)
		}
		return int(a.Num) - int(b.Num)
	})
	if len(codes) == 0 {
		text(w.sans, "No controls yet. Press a button or move an axis on the device.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	cx, cy := x, y
	for _, code := range codes {
		role := e.doc.Role(e.layout, code)
		label := code.String()
		switch {
		case role.Kind != RoleNone:
			label += "  " + role.Text
		case role.Split:
			label += "  +/−"
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
	case RoleController:
		does = fmt.Sprintf("sends controller %d", role.Controller)
	case RoleBend:
		does = "bends the pitch"
	case RoleOther:
		does = "sends " + role.Text + " (change that in the file)"
	case RolePass:
		does = "is left to the system: it types, also while HIDI is on"
	}
	where := ""
	if role.Kind != RoleNone && role.Section.Layout == "" {
		where = ", in every layout"
	}
	text(w.bold, e.code.String(), x, y, sizeName, col(colText))
	text(w.sans, does+where, x+width(w.bold, e.code.String(), sizeName)+12, y+3, sizeText, col(colFaint))
	y += 32
	if e.code.Kind != control.KindKey {
		return w.editAxis(e, role, x, y)
	}

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

	// A controller, which a key sends as 127 while held and 0 after.
	y = w.editController(e, role, Shape{}, x, y)

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

	bx = x
	if role.Kind != RolePass {
		hit, end := w.button("Leave it to the system", bx, y)
		if hit {
			e.try(e.doc.SetPassthrough(e.layout, e.code))
		}
		bx = end + 8
	}
	if role.Kind != RoleNone {
		if hit, _ := w.button("Make it do nothing", bx, y); hit {
			e.try(e.doc.Clear(e.layout, e.code))
		}
	}
	return y + 32
}

// firstController is the controller a control is given by the first click:
// the modulation wheel.
const firstController = 1

// editController draws the row that makes the chosen control send a
// controller and moves its number.
func (w *window) editController(e *editor, role Role, shape Shape, x, y float32) float32 {
	text(w.sans, "controller", x, y+5, sizeText, col(colFaint))
	bx := x + 70
	number := int(role.Controller)
	for _, step := range []struct {
		label string
		delta int
	}{{"−10", -10}, {"−1", -1}, {"+1", 1}, {"+10", 10}} {
		if step.delta == 1 {
			shown := "—"
			if role.Kind == RoleController {
				shown = fmt.Sprintf("%d", number)
			}
			text(w.mono, shown, bx+6, y+6, sizeMono, col(colText))
			bx += width(w.mono, "000", sizeMono) + 18
		}
		hit, end := w.button(step.label, bx, y)
		if hit {
			next := number + step.delta
			if role.Kind != RoleController {
				next = firstController // the first click makes it a controller at all
			}
			e.try(e.doc.SetController(e.layout, e.code, uint8(min(max(next, 0), 127)), shape))
		}
		bx = end + 6
	}
	return y + 36
}

// editAxis draws the ways to change what the chosen axis sends: a
// controller or the pitch bend, and how its position is read.
func (w *window) editAxis(e *editor, role Role, x, y float32) float32 {
	name := e.code.String()
	if role.Split {
		text(w.sans, "Its two directions are bound apart, as "+name+"+ and "+name+"−. Change those in the file.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	if role.Kind == RoleNote || role.Kind == RoleCommand || role.Kind == RoleOther {
		text(w.sans, "Change that in the file, or make it do nothing and start anew.", x, y, sizeText, col(colFaint))
		y += 26
	}
	shape := role.Shape
	// again writes the binding with a changed shape.
	again := func() {
		switch role.Kind {
		case RoleController:
			e.try(e.doc.SetController(e.layout, e.code, role.Controller, shape))
		case RoleBend:
			e.try(e.doc.SetBend(e.layout, e.code, shape))
		}
	}

	y = w.editController(e, role, shape, x, y)

	text(w.sans, "pitch", x, y+5, sizeText, col(colFaint))
	if role.Kind == RoleBend {
		text(w.mono, "bends the pitch", x+76, y+6, sizeMono, col(colText))
	} else if hit, _ := w.button("Bend the pitch", x+70, y); hit {
		e.try(e.doc.SetBend(e.layout, e.code, shape))
	}
	y += 36

	if role.Kind == RoleController || role.Kind == RoleBend {
		// How the position is read.
		text(w.sans, "rests in the middle", x, y+2, sizeText, col(colFaint))
		px := x + width(w.sans, "rests in the middle", sizeText) + 10
		if w.pill(shape.Centered, px, y) {
			shape.Centered = !shape.Centered
			again()
		}
		px += 64
		text(w.sans, "reversed", px, y+2, sizeText, col(colFaint))
		px += width(w.sans, "reversed", sizeText) + 10
		if w.pill(shape.Invert, px, y) {
			shape.Invert = !shape.Invert
			again()
		}
		px += 64
		delta, _ := w.stepper("dead zone", fmt.Sprintf("%.2f", shape.Deadzone), px, y-2)
		if delta != 0 {
			// Whole hundredths, so that the file reads 0.05 and not
			// 0.05000000000000001.
			hundredths := int(shape.Deadzone*100+0.5) + delta*5
			shape.Deadzone = float64(min(max(hundredths, 0), 95)) / 100
			again()
		}
		y += 36
	}
	if role.Kind != RoleNone {
		if hit, _ := w.button("Make it do nothing", x, y); hit {
			e.try(e.doc.Clear(e.layout, e.code))
		}
		y += 32
	}
	return y
}
