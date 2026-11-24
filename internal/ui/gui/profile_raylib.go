//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	naming  typing
	// held are the controls that were down at the last look, to notice a
	// new press: pressing a control of the device chooses it.
	held map[string]bool
	// looked is when the file was last compared with what is shown.
	looked time.Time
	// A control that is pressed and carried to another: pressing says
	// that the button went down on press, at pressAt; carrying that the
	// pointer has moved far enough since to mean it. over is the control
	// under the pointer in the frame being drawn, if overAny.
	pressing, carrying bool
	press              control.Code
	pressAt            rl.Vector2
	over               control.Code
	overAny            bool
}

// pointAt handles the pointer on one control of the page: a click chooses
// it, a press begins carrying it, and the right button empties it, or
// with Ctrl leaves it to the system. It lays a light over the control the
// pointer is on, and marks the one a carried control would land on.
func (w *window) pointAt(e *editor, code control.Code, r rl.Rectangle, roundness float32) {
	if !w.hovered(r) {
		return
	}
	e.over, e.overAny = code, true
	w.setCursor(rl.MouseCursorPointingHand)
	rl.DrawRectangleRounded(r, roundness, 6, rl.NewColor(255, 255, 255, 30))
	if e.carrying && code != e.press {
		rl.DrawRectangleRoundedLinesEx(r, roundness, 6, 2, col(colAccent))
	}
	control := ctrlHeld()
	switch {
	case rl.IsMouseButtonPressed(rl.MouseButtonLeft):
		e.code, e.chosen = code, true
		e.pressing, e.carrying, e.press, e.pressAt = true, false, code, w.mouse()
	case rl.IsMouseButtonPressed(rl.MouseButtonRight) && control:
		e.code, e.chosen = code, true
		e.try(e.doc.SetPassthrough(e.layout, code))
	case rl.IsMouseButtonPressed(rl.MouseButtonRight):
		e.code, e.chosen = code, true
		if e.doc.Role(e.layout, code).Kind != RoleNone {
			e.try(e.doc.Clear(e.layout, code))
		}
	}
}

// carry follows a control that is being carried, once all controls of
// the frame are drawn: it draws what is carried at the pointer, and when
// the button is let go over another control, that one gets what the
// carried one does. Without Ctrl the carried one does nothing afterwards;
// with Ctrl both do the same.
func (w *window) carry(e *editor) {
	if !e.pressing {
		return
	}
	at := w.mouse()
	if !e.carrying && (abs32(at.X-e.pressAt.X) > 6 || abs32(at.Y-e.pressAt.Y) > 6) {
		// Only a control that does something can be carried.
		e.carrying = e.doc.Role(e.layout, e.press).Kind != RoleNone
		if !e.carrying {
			e.pressing = false
			return
		}
	}
	copying := ctrlHeld()
	if e.carrying {
		w.moving = true
		role := e.doc.Role(e.layout, e.press)
		label := strings.TrimPrefix(e.press.String(), "KEY_") + "  " + role.Text
		if copying {
			label += "  (copy)"
		}
		tag := rl.NewRectangle(at.X+12, at.Y+10, w.chipWidth(label), chipH)
		rl.DrawRectangleRounded(tag, 0.4, 6, col(roleColour(role)))
		rl.DrawRectangleRoundedLinesEx(tag, 0.4, 6, 1.5, col(colText))
		w.text(w.mono, label, tag.X+7, tag.Y+3, sizeMono, col(colText))
		w.setCursor(rl.MouseCursorPointingHand)
	}
	if rl.IsMouseButtonReleased(rl.MouseButtonLeft) || !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
		if e.carrying && e.overAny && e.over != e.press {
			e.try(e.doc.Carry(e.layout, e.press, e.over, !copying))
			e.code, e.chosen = e.over, true
		}
		e.pressing, e.carrying = false, false
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
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
	doc.ReadOnly = IsFactory(path, w.opts.Paths)
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
	w.profiles.put(doc.Path, text)
}

// freeLayoutName returns a name no layout of the profile has.
func freeLayoutName(doc *ProfileDoc) string {
	for n := len(doc.Layouts()) + 1; ; n++ {
		if name := fmt.Sprintf("Layout %d", n); !slices.Contains(doc.Layouts(), name) {
			return name
		}
	}
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
	if !w.deaf && !e.adding && ctrlHeld() {
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
	e.scroll.Target -= w.wheel(area) * 60
	e.scroll.Clamp(e.content - area.Height)
	if e.scroll.Step(w.dt) {
		w.moving = true
	}
	scrolled := float32(int(e.scroll.At))
	w.clip(area)
	defer w.unclip()
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
	// Back to the profile as it is saved, with nothing left to take
	// back.
	revertW := width(w.sans, "Revert", sizeText) + 20
	tools -= revertW + 8
	if hit, _ := w.dimButton("Revert", doc.Dirty(), tools, y); hit {
		doc.Revert()
		e.try(nil)
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
	w.text(w.bold, doc.Profile.Name, end+14, y+2, sizeName, col(colText))
	w.drawPath(doc.Path, end+14+width(w.bold, doc.Profile.Name, sizeName)+14, y+6, tools-end-42-width(w.bold, doc.Profile.Name, sizeName))
	y += 34
	// One line for what is to be said: what went wrong, or else how
	// things stand with saving.
	switch {
	case doc.ReadOnly:
		// A factory profile is looked at; a copy of the user's own is
		// what gets changed.
		label := "Customise"
		if _, err := os.Stat(filepath.Join(w.opts.Paths.User, filepath.Base(doc.Path))); err == nil {
			label = "Edit yours"
		}
		hit, end := w.button(label, x, y-4)
		if hit {
			w.edit = nil
			w.customise(doc.Path)
			return
		}
		status := e.message
		if status == "" {
			status = "A factory profile: HIDI keeps it as it is. A copy of your own is used in its place, and can be changed."
		}
		w.text(w.sans, status, end+12, y, sizeText, col(colUnsaved))
		y += 30
	default:
		status := e.message
		switch {
		case status != "":
		case doc.Elsewhere:
			status = "Not saved, and the file was changed elsewhere meanwhile: saving writes over that."
		case doc.Dirty():
			status = "Not saved. A running HIDI plays with it as it is shown here; the file is as it was."
		}
		w.text(w.sans, status, x, y, sizeText, col(colUnsaved))
		y += 26
	}

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
		// On the next line where the page is too narrow for all four.
		need := width(w.sans, field.label, sizeText) + 8 + width(w.sans, "−", sizeText) + width(w.sans, "+", sizeText) + 40 + width(w.mono, "+000", sizeMono) + 16
		if sx > x && sx+need > right {
			sx, y = x, y+34
		}
		delta, end := w.stepper(field.label, field.shown, sx, y)
		if delta != 0 {
			e.try(doc.SetDefault(field.key, field.value+delta))
		}
		sx = end + 22
	}
	y += 40

	y = w.editLayouts(e, x, y, right)

	// The controls: a keyboard as a keyboard, anything else as a list.
	e.overAny = false
	if doc.Profile.Match.Type == control.DeviceKeyboard {
		y = w.editKeyboard(e, x, y, right-x)
	} else {
		y = w.editList(e, device, x, y, right-x)
	}
	// Above everything else on the page, also what is drawn below.
	defer w.carry(e)
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
	w.text(w.sans, "layouts", x, y+3, sizeText, col(colFaint))
	start := x + width(w.sans, "layouts", sizeText) + 12
	row := newFlow(start, right, y, chipH+10, 0)
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
		lx, y := row.put(itemW)
		if w.chip(name, name == e.layout, lx, y) {
			e.layout = name
		}
		bx := lx + w.chipWidth(name) + 6
		if w.checkbox(name == first, bx, y+1, 16) && name != first {
			e.try(doc.SetDefaultLayout(name))
		}
		if note != "" {
			w.text(w.sans, note, bx+22, y+4, sizeText-2, col(colFaint))
		}
	}
	y = row.y + chipH + 12

	// What can be done with the layout that is shown.
	lx := start
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
	// be typed over. The field takes what is typed for as long as it is
	// there.
	e.naming.on = true
	field := rl.NewRectangle(lx, y, max(width(w.mono, e.newName, sizeMono)+24, 140), 24)
	_, key := w.field(&e.naming, field, &e.newName, fieldLook{most: 32})
	add, end := w.button("Add", field.X+field.Width+8, y)
	cancel, _ := w.button("Cancel", end+6, y)
	switch {
	case add || key == rl.KeyEnter:
		name := strings.TrimSpace(e.newName)
		e.try(doc.AddLayout(name))
		if slices.Contains(doc.Layouts(), name) {
			e.layout, e.adding = name, false
		}
	case cancel || key == rl.KeyEscape:
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
		w.pointAt(e, code, rect, 0.2)
		// The key's own name small at the top, what it does below.
		w.text(w.mono, key.Label, rect.X+3, rect.Y+2, small, col(colFaint))
		if role.Kind != RoleNone {
			label := role.Text
			for len(label) > 1 && width(w.mono, label, small+1) > rect.Width-4 {
				label = label[:len(label)-1]
			}
			w.text(w.mono, label, rect.X+3, rect.Y+rect.Height-small-4, small+1, col(colText))
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
		w.text(w.sans, "No controls yet. Press a button or move an axis on the device.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	row := newFlow(x, x+inner, y, chipH+6, 6)
	for _, code := range codes {
		role := e.doc.Role(e.layout, code)
		// The number the window shows the control by, where the device
		// is there to give it one, and the name the profile knows it by.
		label := code.String()
		if n, ok := ControlNumber(device, label); ok && !w.names {
			label = strconv.Itoa(n) + " · " + label
		}
		switch {
		case role.Kind != RoleNone:
			label += "  " + role.Text
		case role.Split:
			label += "  +/−"
		}
		cw := w.chipWidth(label)
		cx, cy := row.put(cw)
		chip := rl.NewRectangle(cx, cy, cw, chipH)
		rl.DrawRectangleRounded(chip, 0.4, 6, col(roleColour(role)))
		if e.chosen && e.code == code {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 2, col(colText))
		}
		w.pointAt(e, code, chip, 0.4)
		w.text(w.mono, label, cx+7, cy+3, sizeMono, col(colText))
	}
	return row.y + chipH + 6
}

// editChosen draws what the chosen control does and the ways to change
// it.
func (w *window) editChosen(e *editor, x, y, inner float32) float32 {
	if !e.chosen {
		for _, line := range []string{
			"Click a control, or press it on the device, to change what it does.",
			"Drag one onto another to move what it does there; hold Ctrl to copy it instead.",
			"The right button empties a control; with Ctrl it leaves a key to the system.",
		} {
			w.text(w.sans, fit(w.sans, line, sizeText, inner), x, y, sizeText, col(colFaint))
			y += 20
		}
		return y + 4
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
	w.text(w.bold, e.code.String(), x, y, sizeName, col(colText))
	w.text(w.sans, does+where, x+width(w.bold, e.code.String(), sizeName)+12, y+3, sizeText, col(colFaint))
	y += 32
	if e.code.Kind != control.KindKey {
		return w.editAxis(e, role, x, y)
	}

	// A note, moved by semitones and octaves.
	w.text(w.sans, "note", x, y+5, sizeText, col(colFaint))
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
			w.text(w.mono, shown, bx+6, y+6, sizeMono, col(colText))
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
	w.text(w.sans, "command", x, y+3, sizeText, col(colFaint))
	row := newFlow(x+70, x+inner, y, chipH+6, 6)
	for _, command := range engine.Commands() {
		label := command.String()
		cx, cy := row.put(w.chipWidth(label))
		if w.chip(label, role.Kind == RoleCommand && role.Command == command, cx, cy) {
			e.try(e.doc.SetCommand(e.layout, e.code, command))
		}
	}
	y = row.y + chipH + 14

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
	w.text(w.sans, "controller", x, y+5, sizeText, col(colFaint))
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
			w.text(w.mono, shown, bx+6, y+6, sizeMono, col(colText))
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
		w.text(w.sans, "Its two directions are bound apart, as "+name+"+ and "+name+"−. Change those in the file.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	if role.Kind == RoleNote || role.Kind == RoleCommand || role.Kind == RoleOther {
		w.text(w.sans, "Change that in the file, or make it do nothing and start anew.", x, y, sizeText, col(colFaint))
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

	w.text(w.sans, "pitch", x, y+5, sizeText, col(colFaint))
	if role.Kind == RoleBend {
		w.text(w.mono, "bends the pitch", x+76, y+6, sizeMono, col(colText))
	} else if hit, _ := w.button("Bend the pitch", x+70, y); hit {
		e.try(e.doc.SetBend(e.layout, e.code, shape))
	}
	y += 36

	if role.Kind == RoleController || role.Kind == RoleBend {
		// How the position is read.
		w.text(w.sans, "rests in the middle", x, y+2, sizeText, col(colFaint))
		px := x + width(w.sans, "rests in the middle", sizeText) + 10
		if w.pill(shape.Centered, px, y) {
			shape.Centered = !shape.Centered
			again()
		}
		px += 64
		w.text(w.sans, "reversed", px, y+2, sizeText, col(colFaint))
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
