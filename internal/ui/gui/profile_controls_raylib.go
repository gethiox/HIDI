//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/midi"
)

// live is what the device a profile is shown for does at this moment, as
// far as the page shows it: the controls of its main part.
type live struct {
	// seen are the keys and buttons the device has, or has used so far,
	// and axes its axes.
	seen, axes []control.Code
	down       map[control.Code]bool
	// at is where each axis stands, from 0 to 1.
	at map[control.Code]float64
}

func liveOf(device *ipc.Instrument) live {
	now := live{down: map[control.Code]bool{}, at: map[control.Code]float64{}}
	if device == nil {
		return now
	}
	for _, b := range device.Buttons {
		if code, err := control.ParseCode(b.Name); err == nil && b.Part == "" {
			now.seen = append(now.seen, code)
			now.down[code] = b.Down
		}
	}
	for _, a := range device.Axes {
		// An axis that has said nothing yet has no place to show.
		if code, err := control.ParseCode(a.Name); err == nil && a.Part == "" {
			now.axes = append(now.axes, code)
			if a.Known {
				now.at[code] = a.Value
			}
		}
	}
	return now
}

// pointAt handles the pointer on one spot of the page: a click chooses
// it, a press begins carrying it, and the right button empties it, or
// with Ctrl leaves a key to the system. It lays a light over the spot the
// pointer is on, and marks the one a carried spot would land on.
func (w *window) pointAt(e *editor, at Spot, r rl.Rectangle, roundness float32) {
	if !w.hovered(r) {
		return
	}
	e.over, e.overAny = at, true
	w.setCursor(rl.MouseCursorPointingHand)
	rl.DrawRectangleRounded(r, roundness, 6, rl.NewColor(255, 255, 255, 30))
	if e.carrying && at != e.press {
		rl.DrawRectangleRoundedLinesEx(r, roundness, 6, 2, col(colAccent))
	}
	switch {
	case rl.IsMouseButtonPressed(rl.MouseButtonLeft):
		e.at, e.chosen = at, true
		e.pressing, e.carrying, e.press, e.pressAt = true, false, at, w.mouse()
	case rl.IsMouseButtonPressed(rl.MouseButtonRight) && ctrlHeld():
		e.at, e.chosen = at, true
		e.try(e.doc.SetPassthrough(e.layout, at.Code))
	case rl.IsMouseButtonPressed(rl.MouseButtonRight):
		e.at, e.chosen = at, true
		if role := e.doc.RoleAt(e.layout, at); role.Kind != RoleNone {
			e.try(e.doc.ClearAt(e.layout, at))
		}
	}
}

// carry follows a spot that is being carried, once all controls of the
// frame are drawn: it draws what is carried at the pointer, and when the
// button is let go over another spot, that one gets what the carried one
// does. Without Ctrl the carried one does nothing afterwards; with Ctrl
// both do the same.
func (w *window) carry(e *editor, device *ipc.Instrument) {
	if !e.pressing {
		return
	}
	at := w.mouse()
	far := func(a, b float32) bool { return a-b > 6 || b-a > 6 }
	if !e.carrying && (far(at.X, e.pressAt.X) || far(at.Y, e.pressAt.Y)) {
		// Only a spot that does something can be carried.
		e.carrying = e.doc.RoleAt(e.layout, e.press).Kind != RoleNone
		if !e.carrying {
			e.pressing = false
			return
		}
	}
	copying := ctrlHeld()
	if e.carrying {
		w.moving = true
		role := e.doc.RoleAt(e.layout, e.press)
		label := w.spotLabel(device, e.press) + " => " + role.Short()
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
			e.try(e.doc.CarryAt(e.layout, e.press, e.over, !copying))
			e.at, e.chosen = e.over, true
		}
		e.pressing, e.carrying = false, false
	}
}

// spotLabel names a spot as the page does; a key of a keyboard without
// the "KEY_" every one of them begins with.
func (w *window) spotLabel(device *ipc.Instrument, at Spot) string {
	return strings.TrimPrefix(SpotLabel(device, at, w.names), "KEY_")
}

// mapping is the lettering of a spot with what it does: the two set apart
// by "=>", as "12 => C3".
func mapping(label string, role Role) string {
	if does := role.Short(); does != "" {
		return label + " => " + does
	}
	return label
}

// target is the lettering of what an axis is mapped to, behind the "=>"
// of its line: what it does, led by the sign of the half where it is one,
// and a dash for nothing.
func target(sign string, role Role) string {
	does := role.Short()
	if does == "" {
		does = "—"
	}
	if sign != "" {
		return sign + " " + does
	}
	return does
}

// spotChip draws one spot as a chip that says what it does, in the colour
// of that, takes the pointer for it, and shows what its control does at
// this moment, as the engine has it: a spot that is held like a key lights
// up while it counts as held, and one that sends a controller or bends the
// pitch fills by the value it sends. A spot that sends what another
// control sends too is ringed in yellow. It returns the width it took.
func (w *window) spotChip(e *editor, at Spot, text string, now live, x, y float32) float32 {
	role := e.doc.RoleAt(e.layout, at)
	cw := w.chipWidth(text)
	follows := role.Kind == RoleController || role.Kind == RoleBend
	if follows {
		cw = max(cw, fillChip) // room for the value to show
	}
	chip := rl.NewRectangle(x, y, cw, chipH)
	back := roleColour(role)
	// A key that is down shows, whatever it does; anything else when
	// what it does counts as held.
	held := e.shadow.Held(at) && !follows
	if at.Code.Kind != control.KindAxis && now.down[at.Code] {
		held = true
	}
	ink := col(colText)
	switch {
	case held:
		rl.DrawRectangleRounded(chip, 0.4, 6, col(w.pressed()))
		ink = col(colBar)
	case follows:
		// The value fills the chip, on a ground of the same colour
		// turned down.
		rl.DrawRectangleRounded(chip, 0.4, 6, col(Mix(colBar, back, 0.45)))
		if from, to, ok := e.shadow.Level(at); ok && cw*float32(to-from) >= 1 {
			w.clip(rl.NewRectangle(x+cw*float32(from), y, cw*float32(to-from), chipH))
			rl.DrawRectangleRounded(chip, 0.4, 6, col(Mix(back, colMIDI, 0.55)))
			w.unclip()
		}
	default:
		rl.DrawRectangleRounded(chip, 0.4, 6, col(back))
	}
	switch {
	case e.chosen && e.at == at:
		rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 2, col(colText))
	case e.doubled[at]:
		rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 2, col(colUnsaved))
	}
	w.pointAt(e, at, chip, 0.4)
	w.text(w.mono, text, x+7, y+3, sizeMono, ink)
	return cw
}

// neutralMark draws the mark of where an axis rests: a small triangle
// that points down at a bar, with its tip at x, y.
func (w *window) neutralMark(x, y float32) {
	rl.DrawTriangle(rl.NewVector2(x-4, y-6), rl.NewVector2(x, y), rl.NewVector2(x+4, y-6), col(colNeutral))
}

// heading draws the name of a group of controls and returns where the
// group begins.
func (w *window) heading(name string, x, y float32) float32 {
	w.text(w.sans, name, x, y, sizeText-1, col(colFaint))
	return y + 20
}

// editKeyboard draws the keyboard of a profile for keyboards: every key
// in the colour of what it does, with its name and its part, the keys
// that are down on the device marked. Keys that are not on the drawing,
// which the profile binds or the device has shown it has, follow in a row
// below and are changed like the others.
func (w *window) editKeyboard(e *editor, now live, x, y, inner float32) float32 {
	unit := min(inner/KeyboardUnitsW, 38)
	small := min(float32(10), unit*0.3)
	drawn := map[control.Code]bool{}
	for _, key := range Keyboard() {
		code, err := control.ParseCode(key.Name)
		if err != nil {
			continue
		}
		drawn[code] = true
		at := Whole(code)
		role := e.doc.RoleAt(e.layout, at)
		rect := rl.NewRectangle(x+key.X*unit+1, y+key.Y*unit+1, key.W*unit-2, key.H*unit-2)
		rl.DrawRectangleRounded(rect, 0.2, 4, col(roleColour(role)))
		switch {
		case now.down[code]:
			rl.DrawRectangleRounded(rect, 0.2, 4, rl.NewColor(255, 255, 255, 70))
			rl.DrawRectangleRoundedLinesEx(rect, 0.2, 4, 2, col(w.pressed()))
		case e.chosen && e.at == at:
			rl.DrawRectangleRoundedLinesEx(rect, 0.2, 4, 2, col(colText))
		}
		w.pointAt(e, at, rect, 0.2)
		// The key's own name small at the top, what it does below.
		w.text(w.mono, key.Label, rect.X+3, rect.Y+2, small, col(colFaint))
		if label := role.Short(); label != "" {
			for len(label) > 1 && width(w.mono, label, small+1) > rect.Width-4 {
				label = label[:len(label)-1]
			}
			w.text(w.mono, label, rect.X+3, rect.Y+rect.Height-small-4, small+1, col(colText))
		}
	}
	y += unit*KeyboardUnitsH + 6

	// The keys the drawing has no place for.
	others := slices.DeleteFunc(e.named(control.KindKey, now.seen), func(c control.Code) bool { return drawn[c] })
	if len(others) == 0 {
		return y
	}
	y = w.heading("Other keys", x, y)
	row := newFlow(x, x+inner, y, chipH+6, 6)
	for _, code := range others {
		at := Whole(code)
		text := mapping(w.spotLabel(nil, at), e.doc.RoleAt(e.layout, at))
		cx, cy := row.put(w.chipWidth(text))
		w.spotChip(e, at, text, now, cx, cy)
	}
	return row.y + chipH + 6
}

// named returns the controls of one kind that the profile names in any
// layout or for all of them, with those given beside them, each once, in
// the order of their codes.
func (e *editor) named(kind control.Kind, more []control.Code) []control.Code {
	var out []control.Code
	add := func(bindings []engine.Binding) {
		for _, b := range bindings {
			if b.Source.Part == "" && b.Source.Code.Kind == kind {
				out = append(out, b.Source.Code)
			}
		}
	}
	for _, l := range e.doc.Profile.Engine.Layouts {
		add(l.Bindings)
	}
	add(e.doc.Profile.Engine.Global)
	for _, c := range more {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b control.Code) int { return cmp.Compare(a.Num, b.Num) })
	return slices.Compact(out)
}

// isHat reports whether an axis is one of a hat: by what the device says
// where a device is there to say, and by its code otherwise.
func (e *editor) isHat(code control.Code) bool {
	for _, c := range e.doc.controls {
		if c.Code == code && c.Part == "" {
			return c.Hat
		}
	}
	return code.Kind == control.KindAxis && code.Num >= 0x10 && code.Num <= 0x17
}

// The measures of a row of the axes.
const (
	axisRow   = 26  // the height of one
	axisBar   = 110 // the width of the bar that shows where the axis stands
	axisCell  = 400 // the least width of a row, for two to stand abreast
	arrowSize = sizeMono
	fillChip  = 72 // the least width of a chip that fills by a value
)

// editControls draws the controls of a profile for anything but a
// keyboard, in three groups: the buttons as chips, the axes each with a
// bar for where it stands and with what it is mapped to, and the hats,
// whose directions are buttons. What the device does at this moment
// shows on all of them.
func (w *window) editControls(e *editor, device *ipc.Instrument, now live, x, y, inner float32) float32 {
	buttons := e.named(control.KindKey, now.seen)
	var axes, hats []control.Code
	for _, code := range e.named(control.KindAxis, now.axes) {
		if e.isHat(code) {
			hats = append(hats, code)
		} else {
			axes = append(axes, code)
		}
	}
	if len(buttons)+len(axes)+len(hats) == 0 {
		w.text(w.sans, "No controls yet. Press a button or move an axis on the device.", x, y, sizeText, col(colFaint))
		return y + 24
	}
	if e.doc.Profile.Generate.Any() && (device == nil || !device.Complete) {
		// What the rule makes depends on the device, and none is here
		// to ask.
		line := "A controller's other buttons and axes play by their numbers. Connect one to see them here."
		w.text(w.sans, fit(w.sans, line, sizeText, inner), x, y, sizeText, col(colFaint))
		y += 24
	}

	// What sends the same as something else, of all that is shown.
	var spots []Spot
	for _, code := range buttons {
		spots = append(spots, Whole(code))
	}
	for _, code := range slices.Concat(axes, hats) {
		spots = append(spots, Whole(code), Spot{code, engine.RangeLower}, Spot{code, engine.RangeUpper})
	}
	e.doubled = e.doc.Doubled(e.layout, spots)

	if len(buttons) > 0 {
		y = w.heading("Buttons", x, y)
		row := newFlow(x, x+inner, y, chipH+6, 6)
		for _, code := range buttons {
			at := Whole(code)
			text := mapping(w.spotLabel(device, at), e.doc.RoleAt(e.layout, at))
			cx, cy := row.put(w.chipWidth(text))
			w.spotChip(e, at, text, now, cx, cy)
		}
		y = row.y + chipH + 14
	}
	for _, group := range []struct {
		name  string
		codes []control.Code
		hat   bool
	}{{"Axes", axes, false}, {"Hats", hats, true}} {
		if len(group.codes) == 0 {
			continue
		}
		y = w.heading(group.name, x, y)
		// The names take the room of the widest of them.
		var labelW float32
		for _, code := range group.codes {
			labelW = max(labelW, width(w.mono, w.spotLabel(device, Whole(code)), sizeMono))
		}
		cols := max(int(inner/axisCell), 1)
		cell := inner / float32(cols)
		for i, code := range group.codes {
			w.axisLine(e, device, now, code, group.hat, x+float32(i%cols)*cell, y+float32(i/cols)*axisRow, labelW, cell-16)
		}
		y += float32((len(group.codes)+cols-1)/cols)*axisRow + 10
		if !group.hat {
			y = w.detectResting(e, now, group.codes, x, y, inner)
		}
	}
	if len(e.doubled) > 0 {
		line := "Ringed in yellow: more than one control sends this controller or bends the pitch. They overwrite each other."
		w.text(w.sans, fit(w.sans, line, sizeText-1, inner), x, y, sizeText-1, col(colUnsaved))
		y += 22
	}
	return y
}

// detectResting draws the button that looks at where the axes stand right
// now and takes those near their middle for axes that rest there, sticks,
// and the others for axes that do not, levers and triggers: with the
// controller left alone, that is the difference. "Rests in the middle" of
// every axis that sends a controller or bends the pitch is set by it, in
// one change. How near the middle counts is set beside the button.
func (w *window) detectResting(e *editor, now live, axes []control.Code, x, y, inner float32) float32 {
	if e.doc.ReadOnly || len(now.at) == 0 {
		return y // nothing to change, or no device to look at
	}
	if e.window == 0 {
		e.window = 10
	}
	hit, end := w.button("Detect axes that rest in the middle", x, y)
	delta, end := w.stepper("within", percent(e.window), end+14, y)
	if delta != 0 {
		e.window = min(max(e.window+delta*5, 5), 45)
	}
	note := "of the middle, as they stand now: leave the controller alone, then press"
	w.text(w.sans, fit(w.sans, note, sizeText-1, x+inner-end-10), end+10, y+5, sizeText-1, col(colFaint))
	if hit {
		rests := map[control.Code]bool{}
		for _, code := range axes {
			if v, known := now.at[code]; known {
				d := v - 0.5
				rests[code] = d <= float64(e.window)/100 && -d <= float64(e.window)/100
			}
		}
		yes, changed, err := e.doc.SetCentered(e.layout, rests)
		e.try(err)
		if err == nil {
			e.message = fmt.Sprintf("%d of %d axes stand near their middle and are taken to rest there; %d changed.", yes, len(rests), changed)
		}
	}
	return y + 34
}

// axisLine draws one axis: its name, a bar for where it stands, and
// behind "=>" what it is mapped to, as one chip for the axis as a whole or
// as two for its halves where they are bound apart. A hat has its two
// directions there always: they are what one binds of a hat.
func (w *window) axisLine(e *editor, device *ipc.Instrument, now live, code control.Code, hat bool, x, y, labelW, room float32) {
	whole := Whole(code)
	role := e.doc.RoleAt(e.layout, whole)
	label := w.spotLabel(device, whole)
	// The name and the bar stand for the axis: they are marked whenever
	// anything of the axis is chosen, and a click on them chooses the
	// first thing the axis is mapped to, the axis as a whole or else its
	// lower half.
	halves := role.Split || hat
	wholeToo := role.Kind != RoleNone || role.Silent || !halves
	first := whole
	if !wholeToo {
		first = Spot{code, engine.RangeLower}
	}
	head := rl.NewRectangle(x-3, y, labelW+14+axisBar+6, chipH)
	if e.chosen && e.at.Code == code {
		rl.DrawRectangleRoundedLinesEx(head, 0.3, 6, 1.5, col(colText))
	}
	w.pointAt(e, first, head, 0.3)
	w.text(w.mono, label, x+labelW-width(w.mono, label, sizeMono), y+3, sizeMono, col(colText))
	bar := rl.NewRectangle(float32(int(x+labelW+10)), y+(chipH-axisBarH)/2, axisBar, axisBarH)
	rl.DrawRectangleRounded(bar, 1, 4, col(colBar))
	if v, known := now.at[code]; known {
		if filled := bar.Width * float32(min(max(v, 0), 1)); filled >= 1 {
			rl.DrawRectangleRounded(rl.NewRectangle(bar.X, bar.Y, filled, bar.Height), 1, 4, col(w.pressed()))
		}
	}
	// Where the axis rests, as it is read: in the middle for a stick and
	// for halves, at an end for a lever.
	if at, ok := e.shadow.Neutral(code); ok {
		w.neutralMark(bar.X+bar.Width*float32(at), bar.Y)
	}

	cx := bar.X + bar.Width + 10
	w.text(w.mono, "=>", cx, y+3, arrowSize, col(colFaint))
	cx += width(w.mono, "=>", arrowSize) + 8
	end := x + room
	put := func(at Spot, sign string) {
		if cx >= end {
			return
		}
		cx += w.spotChip(e, at, target(sign, e.doc.RoleAt(e.layout, at)), now, cx, y) + 6
	}
	if wholeToo {
		put(whole, "")
	}
	if halves {
		put(Spot{code, engine.RangeLower}, "−")
		put(Spot{code, engine.RangeUpper}, "+")
	}
}

// editChosen draws what the chosen spot does and the ways to change it.
// A key or a button plays a note, sends a controller, runs a command or
// is left to the system. An axis does the same by its position, or bends
// the pitch, as a whole or by its two halves apart.
func (w *window) editChosen(e *editor, device *ipc.Instrument, x, y, inner float32) float32 {
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
	at := e.at
	role := e.doc.RoleAt(e.layout, at)
	axis := at.Code.Kind == control.KindAxis
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
	switch {
	case role.Kind == RoleNone && role.Split:
		does = "is bound by its two halves"
	case role.Generated:
		does += ", by its number"
	case role.Kind != RoleNone && role.Section.Layout == "":
		does += ", in every layout and while HIDI is off"
	}
	// The number the window shows it by, and the name profiles write.
	name := at.Key()
	if n, ok := ControlNumber(device, at.Code.String()); ok {
		name = strconv.Itoa(n) + " · " + name
	}
	w.text(w.bold, name, x, y, sizeName, col(colText))
	w.text(w.sans, does, x+width(w.bold, name, sizeName)+12, y+3, sizeText, col(colFaint))
	y += 32

	if axis && at.Half == engine.RangeWhole && role.Split {
		// The halves are what is changed; the axis as a whole can be
		// had back.
		w.text(w.sans, "Click one of its halves above to change it.", x, y+5, sizeText, col(colFaint))
		if !e.isHat(at.Code) {
			if hit, _ := w.button("Join the halves", x+width(w.sans, "Click one of its halves above to change it.", sizeText)+14, y); hit {
				e.try(e.doc.Join(e.layout, at.Code))
			}
		}
		return y + 36
	}

	// A note, moved by semitones and octaves.
	w.text(w.sans, "note", x, y+5, sizeText, col(colFaint))
	bx := x + 70
	note := int(role.Note)
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
		hit, end := w.repeatButton(step.label, bx, y)
		if hit {
			next := note + step.delta
			if role.Kind != RoleNote {
				next = 60 // the first click makes it a note at all
			}
			if next >= 0 && next <= 127 {
				e.try(e.doc.SetNoteAt(e.layout, at, midi.Note(next)))
			}
		}
		bx = end + 6
	}
	y += 36

	// A controller: an axis sends its position, a key 127 while held.
	w.text(w.sans, "controller", x, y+5, sizeText, col(colFaint))
	bx = x + 70
	for _, step := range []struct {
		label string
		delta int
	}{{"−10", -10}, {"−1", -1}, {"+1", 1}, {"+10", 10}} {
		if step.delta == 1 {
			shown := "—"
			if role.Kind == RoleController {
				shown = strconv.Itoa(int(role.Controller))
			}
			w.text(w.mono, shown, bx+6, y+6, sizeMono, col(colText))
			bx += width(w.mono, "000", sizeMono) + 18
		}
		hit, end := w.repeatButton(step.label, bx, y)
		if hit {
			next := int(role.Controller) + step.delta
			if role.Kind != RoleController {
				next = firstController // the first click makes it a controller at all
			}
			e.try(e.doc.SetControllerAt(e.layout, at, uint8(min(max(next, 0), 127)), role.Shape))
		}
		bx = end + 6
	}
	if axis {
		// The pitch bend, which only something with a position sends.
		bx += 16
		if role.Kind == RoleBend {
			w.text(w.mono, "bends the pitch", bx, y+6, sizeMono, col(colText))
		} else if hit, _ := w.button("Bend the pitch", bx, y); hit {
			e.try(e.doc.SetBendAt(e.layout, at, role.Shape))
		}
	}
	y += 36

	// A command.
	w.text(w.sans, "command", x, y+3, sizeText, col(colFaint))
	row := newFlow(x+70, x+inner, y, chipH+6, 6)
	for _, command := range engine.Commands() {
		label := command.String()
		cx, cy := row.put(w.chipWidth(label))
		if w.chip(label, role.Kind == RoleCommand && role.Command == command, cx, cy) {
			e.try(e.doc.SetCommandAt(e.layout, at, command))
		}
	}
	y = row.y + chipH + 14

	if axis && role.Kind != RoleNone && role.Kind != RoleOther {
		y = w.editShape(e, at, role, x, y, inner)
	}

	bx = x
	if !axis && role.Kind != RolePass {
		hit, end := w.button("Leave it to the system", bx, y)
		if hit {
			e.try(e.doc.SetPassthrough(e.layout, at.Code))
		}
		bx = end + 8
	}
	if axis && at.Half == engine.RangeWhole && !e.isHat(at.Code) {
		hit, end := w.button("Split into two halves", bx, y)
		if hit {
			e.try(e.doc.Split(e.layout, at.Code))
			if e.message == "" {
				e.at = Spot{at.Code, engine.RangeUpper}
			}
		}
		bx = end + 8
	}
	if axis && at.Half != engine.RangeWhole && !e.isHat(at.Code) {
		hit, end := w.button("Join the halves", bx, y)
		if hit {
			e.try(e.doc.Join(e.layout, at.Code))
			if e.message == "" {
				e.at = Whole(at.Code)
			}
		}
		bx = end + 8
	}
	if role.Kind != RoleNone {
		if hit, _ := w.button("Make it do nothing", bx, y); hit {
			e.try(e.doc.ClearAt(e.layout, at))
		}
	}
	y += 32
	if axis && at.Half == engine.RangeWhole && !e.isHat(at.Code) {
		for _, line := range []string{
			"Split: each half of the axis is bound by itself, from the middle outwards. A controller becomes two,",
			"one for each direction, and a stick at rest sends 0 on both instead of 64 on one.",
		} {
			w.text(w.sans, fit(w.sans, line, sizeText-1, inner), x, y, sizeText-1, col(colFaint))
			y += 17
		}
		y += 6
	}
	return y
}

// firstController is the controller a control is given by the first click:
// the modulation wheel, which is what one most often wants to try.
const firstController = 1

// editShape draws how the position of the chosen axis is read. What
// follows the position, a controller or the pitch bend, has its rest in
// the middle or not, a direction and a dead zone; what is held like a
// key, a note or a command, has a direction and the point where it
// triggers.
func (w *window) editShape(e *editor, at Spot, role Role, x, y, inner float32) float32 {
	shape := role.Shape
	again := func() { e.try(e.doc.SetShapeAt(e.layout, at, shape)) }
	follows := role.Kind == RoleController || role.Kind == RoleBend
	px := x
	toggle := func(label string, on *bool) {
		w.text(w.sans, label, px, y+2, sizeText, col(colFaint))
		px += width(w.sans, label, sizeText) + 10
		if w.pill(*on, px, y) {
			*on = !*on
			again()
		}
		px += 64
	}
	if follows && at.Half == engine.RangeWhole {
		toggle("rests in the middle", &shape.Centered)
	}
	toggle("reversed", &shape.Invert)
	if follows {
		// The dead zone is the profile's unless the axis has its own.
		toggle("own dead zone", &shape.OwnDeadzone)
		if shape.OwnDeadzone {
			delta, _ := w.stepper("", percentOf(shape.Deadzone), px-10, y-2)
			if delta != 0 {
				hundredths := int(shape.Deadzone*100+0.5) + delta*5
				shape.Deadzone = float64(min(max(hundredths, 0), 95)) / 100
				again()
			}
		} else {
			w.text(w.mono, percentOf(shape.Deadzone)+", the profile's", px, y+3, sizeMono, col(colFaint))
		}
	} else {
		// Where the axis counts as pressed, in steps of a twentieth.
		at := shape.Threshold
		if at == 0 {
			at = 0.5
		}
		delta, _ := w.stepper("triggers at", fmt.Sprintf("%d%%", int(at*100+0.5)), px, y-2)
		if delta != 0 {
			hundredths := int(at*100+0.5) + delta*5
			shape.Threshold = float64(min(max(hundredths, 5), 95)) / 100
			again()
		}
	}
	y += 34
	if follows && at.Half == engine.RangeWhole {
		line := "Rests in the middle: for a stick. A dead zone is then around the middle, the pitch bends both ways from there, and a controller counts 64 as its rest."
		w.text(w.sans, fit(w.sans, line, sizeText-1, inner), x, y, sizeText-1, col(colFaint))
		y += 22
	}
	return y
}
