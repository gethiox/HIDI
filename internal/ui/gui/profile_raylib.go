//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/ipc"
)

// editor is the page on which a profile is changed.
type editor struct {
	doc    *ProfileDoc
	layout string
	// at is the control, or the half of an axis, that is chosen, if
	// chosen says that one is.
	at      Spot
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
	press              Spot
	pressAt            rl.Vector2
	over               Spot
	overAny            bool
	// offering says that the list of devices that can have a profile
	// of their own is unfolded.
	offering bool
	// saved says that the file was written in this frame; the running
	// HIDI is then asked to look at it at once.
	saved bool
	// shadow plays what the device does through an engine made of the
	// profile as it is shown, for what the page shows of it.
	shadow Shadow
	// doubled are the spots that send a controller, or bend the pitch,
	// which another control sends as well, in the frame being drawn.
	doubled map[Spot]bool
	// window is how far from its middle an axis may stand to be taken
	// for one that rests there, in hundredths of its travel; 0 before
	// it was set.
	window int
	// device names the instrument the page was opened for, as
	// [EntryKey] does: the page shows what that device has and does.
	// Empty where it was opened for none, from the list of profiles.
	device string
}

// editProfile opens the page for a profile file, for the instrument that
// device names by its [EntryKey], or for none in particular.
func (w *window) editProfile(path, device string) {
	// A page that was left with changes on it is as it was left, and
	// shown for the device it is opened for now.
	if e := w.docs[path]; e != nil {
		w.edit = e
		if device != "" && device != e.device {
			e.device, e.chosen = device, false
		}
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
	w.edit = &editor{doc: doc, layout: doc.DefaultLayout(), held: map[string]bool{}, device: device}
	if w.docs == nil {
		w.docs = map[string]*editor{}
	}
	w.docs[path] = w.edit
}

// offerOwn draws the button that gives a device a profile for itself
// alone, where a device plays with the profile shown that can have one.
// The button unfolds a line with those devices; a click on one makes a
// profile of the user's out of what is shown, for devices of its vendor
// and product, and opens it. It returns where the button ends, how much
// room the unfolded line takes below it, and whether the page was left
// for the new profile.
func (w *window) offerOwn(e *editor, devices []*ipc.Instrument, x, y, left, right float32) (end, below float32, gone bool) {
	var can []*ipc.Instrument
	offered := map[string]bool{}
	for _, device := range devices {
		id := device.Name + "\x00" + device.Hardware
		hardware, err := control.ParseHardwareID(device.Hardware)
		if offered[id] || err != nil || hardware.Vendor == 0 && hardware.Product == 0 {
			continue // a device that does not say what it is cannot be told apart
		}
		offered[id] = true
		can = append(can, device)
	}
	if len(can) == 0 {
		e.offering = false
		return x - 12, 0, false // nothing drawn: what follows begins at x
	}
	mark := " ▼"
	if e.offering {
		mark = " ▲"
	}
	hit, end := w.button("Customise for specific device"+mark, x, y)
	if hit {
		e.offering = !e.offering
	}
	if !e.offering {
		return end, 0, false
	}
	row := newFlow(left, right, y+32, 30, 8)
	for _, device := range can {
		label := fit(w.sans, device.Name, sizeText, 260)
		bw := width(w.sans, label, sizeText) + 20
		bx, by := row.put(bw)
		hit, _ := w.button(label, bx, by)
		if EntryKey(*device) == e.device {
			// The device the page was opened for.
			rl.DrawRectangleRounded(rl.NewRectangle(bx, by, bw, 24), 0.35, 6, colA(colAccent, 60))
			rl.DrawRectangleRoundedLinesEx(rl.NewRectangle(bx, by, bw, 24), 0.35, 6, 1.5, colA(colAccent, 200))
		}
		if !hit {
			continue
		}
		own, made, err := CustomiseFor(e.doc.Text(), w.opts.Paths.User, *device)
		if err != nil {
			e.message = strings.ReplaceAll(err.Error(), "\n", " ")
			continue
		}
		// What was shown and not saved is the new profile's now.
		if made && e.doc.Dirty() {
			e.doc.Revert()
			e.touched = true
		}
		e.offering = false
		w.settings.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
		w.reload()
		w.edit = nil
		w.editProfile(own, EntryKey(*device))
		if w.edit != nil && made {
			w.edit.message = fmt.Sprintf("A profile for %s alone, made of %q.", device.Name, e.doc.Profile.Name)
		}
		return end, 0, true
	}
	return end, row.y + 30 - (y + 32) + 4, false
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
	return colNothingKey
}

// drawEditor draws the page in place of the instruments.
func (w *window) drawEditor(state *ipc.Snapshot, area rl.Rectangle) {
	e := w.edit
	doc := e.doc
	w.active = state != nil && state.Active
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
		if e.saved {
			e.saved = false
			w.reload()
		}
	}()
	// The keys that take a change back, make it again, and save. They
	// reach the window only while the keyboard is not HIDI's alone.
	if !w.deaf && !w.blocked && !e.adding && ctrlHeld() {
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
	// choose a control. It is the one the page was opened for, whatever
	// profile HIDI has it play with at this moment: a profile that was
	// just made for it is its own a moment later. A page opened for no
	// device shows the first that plays with the profile.
	var device *ipc.Instrument
	var devices []*ipc.Instrument // all that play with it
	if state != nil {
		for i := range state.Instruments {
			in := &state.Instruments[i]
			if in.ProfileFile == doc.Path {
				devices = append(devices, in)
			}
			if e.device != "" && EntryKey(*in) == e.device {
				device = in
			}
		}
	}
	if device == nil && len(devices) > 0 {
		device = devices[0]
	}
	// What the profile's rule makes of that device shows with the rest.
	doc.ShowFor(DeviceControls(device))
	if device != nil {
		now := map[string]bool{}
		for _, b := range device.Buttons {
			if b.Down && b.Part == "" {
				now[b.Name] = true
				if !e.held[b.Name] {
					if code, err := control.ParseCode(b.Name); err == nil {
						e.at, e.chosen = Whole(code), true
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
	hx := end + 14 + width(w.bold, doc.Profile.Name, sizeName) + 12
	if doc.ReadOnly && doc.Profile.Generate.Any() {
		// A factory profile with a rule: what is shown is made for the
		// device, not written anywhere.
		w.text(w.sans, "(generated)", hx-4, y+5, sizeText, col(colFaint))
		hx += width(w.sans, "(generated)", sizeText) + 8
	}
	if device != nil {
		// The device the page shows the profile for: its controls, what
		// the rule makes of them, and what it does at this moment.
		shown := fit(w.sans, "for "+device.Name, sizeText, max(tools-hx-120, 80))
		w.text(w.sans, shown, hx, y+5, sizeText, col(colAccent))
		hx += width(w.sans, shown, sizeText) + 12
	}
	w.drawPath(doc.Path, hx, y+6, tools-hx-14)
	y += 34
	// One line for what is to be said: what went wrong, or else how
	// things stand with saving.
	switch {
	case doc.ReadOnly:
		// A factory profile is looked at; a copy of the user's own is
		// what gets changed.
		label := "Customise Default"
		if _, err := os.Stat(filepath.Join(w.opts.Paths.User, filepath.Base(doc.Path))); err == nil {
			label = "Edit yours"
		}
		hit, end := w.button(label, x, y-4)
		if hit {
			w.edit = nil
			w.customise(doc.Path, e.device)
			return
		}
		end, below, gone := w.offerOwn(e, devices, end+8, y-4, x, right)
		if gone {
			return
		}
		status := e.message
		if status == "" {
			status = "A factory profile: HIDI keeps it as it is. A copy of your own is used in its place, and can be changed."
		}
		w.text(w.sans, fit(w.sans, status, sizeText, right-end-12), end+12, y, sizeText, col(colUnsaved))
		y += 30 + below
	case !OwnProfile(doc.Profile) && len(devices) > 0:
		// A profile for a kind of device, with a device of that kind
		// playing: the device can have one for itself alone.
		end, below, gone := w.offerOwn(e, devices, x, y-4, x, right)
		if gone {
			return
		}
		status := e.message
		switch {
		case status != "":
		case doc.Elsewhere:
			status = "Not saved, and the file was changed elsewhere meanwhile: saving writes over that."
		case doc.Dirty():
			status = "Not saved. A running HIDI plays with it as it is shown here; the file is as it was."
		}
		w.text(w.sans, fit(w.sans, status, sizeText, right-end-12), end+12, y, sizeText, col(colUnsaved))
		y += 30 + below
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
	if doc.Profile.Match.Type != control.DeviceKeyboard {
		// The dead zone of every axis that names none of its own.
		need := width(w.sans, "dead zone", sizeText) + 130
		if sx > x && sx+need > right {
			sx, y = x, y+34
		}
		zone := doc.Profile.Deadzone
		if delta, _ := w.stepper("dead zone", percentOf(zone), sx, y); delta != 0 {
			hundredths := int(zone*100+0.5) + delta*5
			e.try(doc.SetDeadzone(float64(min(max(hundredths, 0), 95)) / 100))
		}
	}
	y += 40

	y = w.editLayouts(e, x, y, right)

	// The controls: a keyboard as a keyboard, anything else as a list.
	e.overAny = false
	now := liveOf(device)
	e.shadow.Follow(doc, e.layout, float64(w.settings.edit.shown.Input.Hysteresis)/100, device)
	if doc.Profile.Match.Type == control.DeviceKeyboard {
		y = w.editKeyboard(e, now, x, y, right-x)
	} else {
		y = w.editControls(e, device, now, x, y, right-x)
	}
	// Above everything else on the page, also what is drawn below.
	defer w.carry(e, device)
	y += 14
	y = w.editChosen(e, device, x, y, right-x)

	e.content = y + scrolled - area.Y + 12
}

// save writes the profile and says what came of it.
func (e *editor) save() {
	e.message, e.saved = "", true
	if err := e.doc.Save(); err != nil {
		e.message, e.saved = strings.ReplaceAll(err.Error(), "\n", " "), false
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
		if w.checkbox(name == first, bx, y+1, 16) && name != first && !doc.ReadOnly {
			e.try(doc.SetDefaultLayout(name))
		}
		if note != "" {
			w.text(w.sans, note, bx+22, y+4, sizeText-2, col(colFaint))
		}
	}
	y = row.y + chipH + 12

	// What can be done with the layout that is shown. Every button
	// takes its place whether it was clicked or not: one drawn where
	// the clicked one is would take the same click.
	lx := start
	at := slices.Index(layouts, e.layout)
	can := !doc.ReadOnly // a factory profile is only looked at
	moveLeft, end := w.dimButton("← Move left", can && at > 0, lx, y)
	moveRight, end := w.dimButton("Move right →", can && at >= 0 && at < len(layouts)-1, end+6, y)
	remove, end := w.dimButton("Remove \""+e.layout+"\"", can && len(layouts) > 1, end+6, y)
	lx = end + 18
	switch {
	case moveLeft:
		e.try(doc.MoveLayout(e.layout, -1))
	case moveRight:
		e.try(doc.MoveLayout(e.layout, 1))
	case remove:
		e.try(doc.RemoveLayout(e.layout))
	}
	if !e.adding {
		if hit, _ := w.dimButton("+ Add a layout", can, lx, y); hit {
			e.adding = true
			e.naming.start(freeLayoutName(doc))
		}
		return y + 38
	}
	// The name of the new layout: there is one to begin with, selected,
	// so that what is typed takes its place. Add and Cancel belong to the
	// field; a click anywhere else drops the new layout, as Escape does.
	name := e.naming.text()
	field := rl.NewRectangle(lx, y, max(width(w.mono, name, sizeMono)+24, 140), 24)
	addAt := rl.NewRectangle(field.X+field.Width+8, y, width(w.sans, "Add", sizeText)+20, 24)
	cancelAt := rl.NewRectangle(addAt.X+addAt.Width+6, y, width(w.sans, "Cancel", sizeText)+20, 24)
	_, ended := w.field(&e.naming, field, &e.newName, fieldLook{rules: fieldRules{most: 32}, with: []rl.Rectangle{addAt, cancelAt}})
	add, _ := w.button("Add", addAt.X, y)
	cancel, _ := w.button("Cancel", cancelAt.X, y)
	switch {
	case add || ended == accepted:
		if add {
			name = e.naming.text()
		} else {
			name = e.newName
		}
		name = strings.TrimSpace(name)
		e.try(doc.AddLayout(name, e.layout))
		if slices.Contains(doc.Layouts(), name) {
			e.layout, e.adding, e.naming.on = name, false, false
		} else if !e.naming.on {
			e.naming.start(name) // refused: typed again, the message says why
		}
	case cancel || ended == cancelled:
		e.adding, e.naming.on = false, false
	}
	return y + 38
}
