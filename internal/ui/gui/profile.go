package gui

import (
	"bytes"
	"cmp"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// ProfileDoc is a profile file that is open for changing in the window.
// Every change is made in the text of the file, one key at a time, so
// that what a person wrote there by hand stays. The changes are kept here
// and can be taken back; the file is written when [ProfileDoc.Save] is
// called.
type ProfileDoc struct {
	Path    string
	Profile *config.Profile
	// text is the profile as it is shown, and saved as it is in the file,
	// as far as the window knows; was is what saved says.
	text, saved []byte
	was         *config.Profile
	// undo holds the texts before each change, the newest last, and redo
	// those that were taken back.
	undo, redo [][]byte
	// Elsewhere says that the file was changed by other hands while
	// there are changes here that are not saved: saving would write over
	// what they did.
	Elsewhere bool
	// ReadOnly says that the profile is shown and not changed: it is one
	// of those HIDI comes with, which HIDI keeps as they are.
	ReadOnly bool
	// controls is what the device has that the profile is shown for, and
	// made what the profile's rule makes of it, control by control, for
	// the profile madeFor.
	controls []control.Input
	made     map[control.Code][]config.Generated
	madeFor  *config.Profile
}

// ShowFor says which device the profile is shown for, by the controls it
// has: what the profile's rule makes of them is shown beside what the
// profile names (see [config.Generate]). No controls is no device.
func (d *ProfileDoc) ShowFor(controls []control.Input) {
	if !slices.Equal(d.controls, controls) {
		d.controls, d.madeFor = slices.Clone(controls), nil
	}
}

// generated returns what the profile's rule makes of a control of the
// device the profile is shown for, as long as no line names the control.
func (d *ProfileDoc) generated(code control.Code) []config.Generated {
	if d.madeFor != d.Profile {
		d.madeFor, d.made = d.Profile, map[control.Code][]config.Generated{}
		for _, g := range d.Profile.Generated(d.controls) {
			d.made[g.Source.Code] = append(d.made[g.Source.Code], g)
		}
	}
	return d.made[code]
}

// maxUndo bounds how many changes can be taken back.
const maxUndo = 200

// Text returns the profile as it is shown, saved or not.
func (d *ProfileDoc) Text() []byte { return d.text }

// Dirty reports whether there are changes that are not saved.
func (d *ProfileDoc) Dirty() bool { return !bytes.Equal(d.text, d.saved) }

// CanUndo and CanRedo report whether there is a change to take back, and
// one that was taken back to make again.
func (d *ProfileDoc) CanUndo() bool { return len(d.undo) > 0 }
func (d *ProfileDoc) CanRedo() bool { return len(d.redo) > 0 }

// show makes text what is shown. The text is a valid profile.
func (d *ProfileDoc) show(text []byte) {
	p, err := config.Parse(text)
	if err != nil {
		return // it was valid when it was kept
	}
	p.Source = d.Path
	d.text, d.Profile = text, p
	d.settle()
}

// settle looks whether the profile as it is shown is the profile that is
// saved, after every change. It compares the profiles, not their text: a
// binding that was carried to another control and back is written at
// another place in the file and binds what it bound. Where they are the
// same profile, the text is again the saved one, to the letter, so that
// nothing waits to be saved and nothing in the file would move.
func (d *ProfileDoc) settle() {
	if d.was != nil && !bytes.Equal(d.text, d.saved) && config.Same(d.Profile, d.was) {
		d.text = d.saved
		d.Profile = d.was
	}
}

// Undo takes the last change back and reports whether there was one.
func (d *ProfileDoc) Undo() bool {
	n := len(d.undo)
	if n == 0 {
		return false
	}
	d.redo = append(d.redo, d.text)
	before := d.undo[n-1]
	d.undo = d.undo[:n-1]
	d.show(before)
	return true
}

// Redo makes the change again that was last taken back.
func (d *ProfileDoc) Redo() bool {
	n := len(d.redo)
	if n == 0 {
		return false
	}
	d.undo = append(d.undo, d.text)
	again := d.redo[n-1]
	d.redo = d.redo[:n-1]
	d.show(again)
	return true
}

// Save writes the profile to its file, whole or not at all. A running
// HIDI takes the file over like any other change to it.
func (d *ProfileDoc) Save() error {
	if err := writeWhole(d.Path, d.text); err != nil {
		return err
	}
	d.saved, d.was, d.Elsewhere = d.text, d.Profile, false
	return nil
}

// Revert goes back to the profile as it is saved, and forgets the changes
// that led away from it: there is nothing to take back afterwards.
func (d *ProfileDoc) Revert() {
	d.undo, d.redo = nil, nil
	d.show(d.saved)
}

// OpenProfile reads a profile file for changing.
func OpenProfile(path string) (*ProfileDoc, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := config.Parse(text)
	if err != nil {
		return nil, err
	}
	p.Source = path
	return &ProfileDoc{Path: path, Profile: p, text: text, saved: text, was: p}, nil
}

// Layouts returns the names of the profile's layouts, in order.
func (d *ProfileDoc) Layouts() []string {
	names := make([]string, len(d.Profile.Engine.Layouts))
	for i, l := range d.Profile.Engine.Layouts {
		names[i] = l.Name
	}
	return names
}

// RoleKind is the kind of thing a control does.
type RoleKind uint8

const (
	// RoleNone: the control does nothing.
	RoleNone RoleKind = iota
	RoleNote
	RoleCommand
	// RoleOther: something the window shows and does not change, a
	// program change for one.
	RoleOther
	// RoleController: the control sends a controller.
	RoleController
	// RoleBend: the control bends the pitch.
	RoleBend
	// RolePass: the key is left to the system.
	RolePass
)

// Spot is what a binding names: a control of the main part of a device,
// or one half of an axis, from its middle to one end.
type Spot struct {
	Code control.Code
	// Half is engine.RangeWhole for the control itself.
	Half engine.Range
}

// Whole is the spot of a control itself.
func Whole(code control.Code) Spot { return Spot{Code: code} }

// Key writes the spot as a profile does: "ABS_X", "ABS_X+", "ABS_X-".
func (s Spot) Key() string {
	switch s.Half {
	case engine.RangeUpper:
		return s.Code.String() + "+"
	case engine.RangeLower:
		return s.Code.String() + "-"
	}
	return s.Code.String()
}

// Role is what a control, or one half of an axis, does when it is used
// alone, and where in the file that is written.
type Role struct {
	Kind RoleKind
	// Text says it in a few characters, for a key cap: "C3", "octave_up".
	Text    string
	Note    midi.Note
	Command engine.Command
	// Controller is the number of the controller sent.
	Controller uint8
	// Shape is how the position of an axis is read.
	Shape Shape
	// What the window does not show and must keep when it writes the
	// binding anew: the channel a binding is moved by, the velocity and
	// the touch of a note, and a pitch bend that goes down.
	Offset   uint8
	Velocity uint8
	Touch    bool
	Down     bool
	// Split says, of a control itself, that the two halves of the axis
	// are bound apart, as "ABS_X+" and "ABS_X-". Lower and Upper say
	// what the halves do, as Text does; empty for a half that does
	// nothing.
	Split        bool
	Lower, Upper string
	// Generated says that no line gives the control this part: the
	// profile's rule does, by the number of the control.
	Generated bool
	// Silent says that a line tells the control to do nothing.
	Silent bool
	// Section is where the binding is, or where a new one would go.
	Section config.Section
}

// Shape is how the position of an axis is read before it is sent.
type Shape struct {
	// Centered is for an axis that rests in the middle, such as a stick:
	// a dead zone is then around the middle, the pitch bends both ways
	// from there, and a controller rests at 64.
	Centered bool
	// Invert reverses the direction.
	Invert bool
	// Deadzone is the fraction of travel next to the rest position that
	// is ignored, from 0 to below 1. It is the profile's, which every
	// axis has, unless OwnDeadzone says that the binding names its own.
	Deadzone    float64
	OwnDeadzone bool
	// Threshold is how far an axis has to move for a note or a command
	// to trigger, as a fraction of its travel; 0 is the usual half.
	Threshold float64
}

// number writes a fraction as a file should read: 0.05, not
// 0.05000000000000001.
func number(v float64) string { return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64) }

// text writes the options as they stand in a binding, each led by a
// comma: those of something that follows the position, or with held
// those of something that is held like a key.
func (s Shape) text(held bool) string {
	out := ""
	if s.Centered && !held {
		out += ", centered = true"
	}
	if s.Invert {
		out += ", invert = true"
	}
	if s.OwnDeadzone {
		out += ", deadzone = " + number(s.Deadzone)
	}
	if s.Threshold > 0 && held {
		out += ", threshold = " + number(s.Threshold)
	}
	return out
}

// take fills in what an action does.
func (r *Role) take(action engine.Action) {
	switch a := action.(type) {
	case engine.Note:
		r.Kind, r.Note, r.Text = RoleNote, a.Note, a.Note.String()
		r.Offset, r.Velocity, r.Touch = a.ChannelOffset, a.Velocity, a.Touch
	case engine.Command:
		r.Kind, r.Command, r.Text = RoleCommand, a, a.String()
	case engine.ControlChange:
		r.Kind, r.Controller, r.Text = RoleController, a.Controller, fmt.Sprintf("cc %d", a.Controller)
		r.Offset = a.ChannelOffset
	case engine.ProgramChange:
		r.Kind, r.Text = RoleOther, fmt.Sprintf("program %d", a.Program)
	case engine.PitchBend:
		r.Kind, r.Text = RoleBend, "pitch bend"
		r.Offset, r.Down = a.ChannelOffset, a.Down
	case engine.Passthrough:
		r.Kind, r.Text = RolePass, "system"
	case engine.Nothing:
		r.Silent = true
	}
}

// Short says what the role is in as few characters as a label on the
// page has room for: "C3", "cc16", "PB", "octave_up", and "nothing" for a
// control that is told to do nothing. A control that simply does nothing
// has no word.
func (r Role) Short() string {
	switch r.Kind {
	case RoleController:
		return "cc" + strconv.Itoa(int(r.Controller))
	case RoleBend:
		if r.Down {
			return "PB↓"
		}
		return "PB"
	case RoleNone:
		if r.Silent {
			return engine.NothingName
		}
		return ""
	}
	return r.Text
}

// become makes the role one of another kind: what only the old kind had
// is dropped, how the position is read stays.
func (r *Role) become(kind RoleKind) {
	if r.Kind != kind {
		*r = Role{Kind: kind, Shape: r.Shape, Section: r.Section}
	}
}

// value writes the role as the right side of a binding of a spot. A
// control that is no axis has no position to read, a half of an axis
// rests in the middle by what it is, and a role the window only shows
// cannot be written.
func (r Role) value(at Spot) (string, error) {
	shape := r.Shape
	if at.Code.Kind != control.KindAxis {
		shape = Shape{}
	}
	if at.Half != engine.RangeWhole {
		shape.Centered = false
	}
	moved := ""
	if r.Offset != 0 {
		moved = fmt.Sprintf(", channel_offset = %d", r.Offset)
	}
	// short writes what can be said in a word that way, and as a table
	// what has more to say.
	short := func(key, word, more string) string {
		if more == "" {
			return word
		}
		return "{ " + key + " = " + word + more + " }"
	}
	switch r.Kind {
	case RoleNote:
		more := ""
		if r.Velocity != 0 {
			more += fmt.Sprintf(", velocity = %d", r.Velocity)
		}
		if r.Touch {
			more += ", touch = true"
		}
		return short("note", strconv.Quote(r.Note.String()), more+moved+shape.text(true)), nil
	case RoleCommand:
		return short("command", strconv.Quote(r.Command.String()), shape.text(true)), nil
	case RoleController:
		return fmt.Sprintf("{ cc = %d%s%s }", r.Controller, moved, shape.text(false)), nil
	case RoleBend:
		way := "true"
		if r.Down {
			way = `"down"`
		}
		return fmt.Sprintf("{ pitch_bend = %s%s%s }", way, moved, shape.text(false)), nil
	case RolePass:
		return strconv.Quote(engine.PassthroughName), nil
	case RoleNone:
		return strconv.Quote(engine.NothingName), nil
	}
	return "", fmt.Errorf("what %s does is changed in the file; %w", at.Key(), config.ErrNotEditable)
}

// find looks for the binding of a spot: the layout's own, or else the one
// of [global], which only the toggle has, or else what the profile's rule
// makes of the control, which it does as long as no line of the layout or
// of [global] names the control in any way.
func (d *ProfileDoc) find(layout string, at Spot) (b engine.Binding, section config.Section, generated, found bool) {
	named := false
	in := func(bindings []engine.Binding) bool {
		for _, c := range bindings {
			if c.Source.Code != at.Code || c.Source.Part != "" {
				continue
			}
			named = true
			if len(c.Source.Modifiers) == 0 && c.Source.Range == at.Half && !found {
				b, found = c, true
			}
		}
		return found
	}
	section = config.Bindings(layout)
	for _, l := range d.Profile.Engine.Layouts {
		if l.Name == layout && in(l.Bindings) {
			// [global] still says whether the control is named,
			// which nothing asks once it is found.
			return b, section, false, true
		}
	}
	if in(d.Profile.Engine.Global) {
		return b, config.Global, false, true
	}
	if !named {
		for _, g := range d.generated(at.Code) {
			if g.Source.Range == at.Half {
				return g.Binding, section, true, true
			}
		}
	}
	return engine.Binding{}, section, false, false
}

// RoleAt returns what a spot does in a layout.
func (d *ProfileDoc) RoleAt(layout string, at Spot) Role {
	b, section, generated, found := d.find(layout, at)
	role := Role{Section: section, Generated: generated}
	if found {
		role.take(b.Action)
		role.Shape = Shape{Centered: b.Source.Centered, Invert: b.Source.Invert, Deadzone: b.Source.Deadzone, Threshold: b.Source.Threshold}
		role.Shape.OwnDeadzone = !generated && d.Profile.OwnDeadzone(section.Layout, b.Source)
	}
	if at.Half != engine.RangeWhole {
		return role
	}
	// The halves of the control, where they are bound apart.
	for _, half := range []struct {
		which engine.Range
		text  *string
	}{{engine.RangeLower, &role.Lower}, {engine.RangeUpper, &role.Upper}} {
		if hb, _, hg, ok := d.find(layout, Spot{at.Code, half.which}); ok {
			var does Role
			does.take(hb.Action)
			role.Split, *half.text = true, does.Text
			role.Generated = role.Generated || hg
		}
	}
	return role
}

// Role returns what a control of the main part does in a layout: by the
// layout's own binding, or else as the toggle of [global].
func (d *ProfileDoc) Role(layout string, code control.Code) Role {
	return d.RoleAt(layout, Whole(code))
}

// Bound reports whether a control does anything in a layout, itself or
// by one of its halves.
func (d *ProfileDoc) Bound(layout string, code control.Code) bool {
	r := d.Role(layout, code)
	return r.Kind != RoleNone || r.Lower != "" || r.Upper != ""
}

// keepOther writes out what the profile's rule makes of the other half of
// an axis, before a line names one half: a control that a line names is
// the rule's no longer, and the half nobody touched would fall silent.
func (d *ProfileDoc) keepOther(text []byte, layout string, at Spot) ([]byte, error) {
	if at.Half == engine.RangeWhole {
		return text, nil
	}
	for _, half := range []engine.Range{engine.RangeLower, engine.RangeUpper} {
		other := Spot{at.Code, half}
		b, _, generated, found := d.find(layout, other)
		if half == at.Half || !found || !generated {
			continue
		}
		var role Role
		role.take(b.Action)
		value, err := role.value(other)
		if err != nil {
			return nil, err
		}
		if text, err = config.Set(text, config.Bindings(layout), other.Key(), value); err != nil {
			return nil, err
		}
	}
	return text, nil
}

// PutAt changes what a spot does in a layout: change is given what it does
// now and makes of it what it is to do. The binding is written into the
// layout. A key that was the toggle is the toggle no more, in no layout:
// a profile has one toggle, which works the same everywhere.
func (d *ProfileDoc) PutAt(layout string, at Spot, change func(*Role)) error {
	return d.change(func(text []byte) ([]byte, error) {
		role := d.RoleAt(layout, at)
		change(&role)
		value, err := role.value(at)
		if err != nil {
			return nil, err
		}
		if text, role.Section, err = d.ownLine(text, layout, role.Section, at); err != nil {
			return nil, err
		}
		if text, err = d.keepOther(text, layout, at); err != nil {
			return nil, err
		}
		return config.Set(text, role.Section, at.Key(), value)
	})
}

// ownLine makes the line of a spot one of the layout: where the spot is
// the toggle, the line of [global] goes, and the layout is where the
// spot's new line belongs.
func (d *ProfileDoc) ownLine(text []byte, layout string, section config.Section, at Spot) ([]byte, config.Section, error) {
	if section != config.Global {
		return text, section, nil
	}
	out, err := config.Remove(text, config.Global, at.Key())
	return out, config.Bindings(layout), err
}

// SetNoteAt makes a spot play a note. One that plays a note already
// keeps what else its line says about the note.
func (d *ProfileDoc) SetNoteAt(layout string, at Spot, note midi.Note) error {
	return d.PutAt(layout, at, func(r *Role) { r.become(RoleNote); r.Note = note })
}

// SetCommandAt makes a spot run a command in a layout. The command that
// turns HIDI off and on is one of its kind and works the same in every
// layout and while HIDI is off, so it is written in [global], in place of
// the one that was there, and what the spot did in the layout goes.
func (d *ProfileDoc) SetCommandAt(layout string, at Spot, command engine.Command) error {
	if command != engine.CommandToggle {
		return d.PutAt(layout, at, func(r *Role) { r.become(RoleCommand); r.Command = command })
	}
	if at.Half != engine.RangeWhole || at.Code.Kind != control.KindKey {
		return fmt.Errorf("%s is for a key or a button", engine.ToggleName)
	}
	return d.change(func(text []byte) ([]byte, error) {
		out := text
		var err error
		for _, b := range d.Profile.Engine.Global {
			if b.Action != engine.CommandToggle {
				continue
			}
			if out, err = config.Remove(out, config.Global, config.SourceKey(b.Source)); err != nil {
				return nil, err
			}
		}
		if was := d.RoleAt(layout, at); !was.Generated && (was.Kind != RoleNone || was.Silent) && was.Section != config.Global {
			if out, err = config.Remove(out, was.Section, at.Key()); err != nil {
				return nil, err
			}
		}
		return config.Set(out, config.Global, at.Key(), strconv.Quote(engine.ToggleName))
	})
}

// SetControllerAt makes a spot send a controller: an axis its position,
// read as shape says, and a key 127 while it is held.
func (d *ProfileDoc) SetControllerAt(layout string, at Spot, controller uint8, shape Shape) error {
	if controller > 127 {
		return fmt.Errorf("there is no controller %d; they go from 0 to 127", controller)
	}
	return d.PutAt(layout, at, func(r *Role) { r.become(RoleController); r.Controller, r.Shape = controller, shape })
}

// SetBendAt makes a spot bend the pitch: both ways for a centered axis,
// upwards otherwise.
func (d *ProfileDoc) SetBendAt(layout string, at Spot, shape Shape) error {
	return d.PutAt(layout, at, func(r *Role) { r.become(RoleBend); r.Shape = shape })
}

// SetShapeAt changes how the position of an axis is read for what it
// does already.
func (d *ProfileDoc) SetShapeAt(layout string, at Spot, shape Shape) error {
	return d.PutAt(layout, at, func(r *Role) { r.Shape = shape })
}

// SetCentered says of each axis given whether it rests in the middle, for
// what it does as a whole in a layout: an axis that sends a controller or
// bends the pitch is read accordingly from then on. Axes that do
// something else, or are bound by their halves, are left as they are. It
// is one change, taken back in one step, and returns how many of the axes
// rest in the middle and how many bindings were changed.
func (d *ProfileDoc) SetCentered(layout string, rests map[control.Code]bool) (resting, changed int, err error) {
	codes := slices.SortedFunc(maps.Keys(rests), func(a, b control.Code) int { return cmp.Compare(a.Num, b.Num) })
	err = d.change(func(text []byte) ([]byte, error) {
		resting, changed = 0, 0
		for _, code := range codes {
			if rests[code] {
				resting++
			}
			at := Whole(code)
			role := d.RoleAt(layout, at)
			if role.Kind != RoleController && role.Kind != RoleBend || role.Shape.Centered == rests[code] {
				continue
			}
			role.Shape.Centered = rests[code]
			value, err := role.value(at)
			if err != nil {
				return nil, err
			}
			if text, err = config.Set(text, role.Section, at.Key(), value); err != nil {
				return nil, err
			}
			changed++
		}
		return text, nil
	})
	return resting, changed, err
}

// Doubled finds the spots among those given that send a controller, or
// bend the pitch, which another control among them sends as well, in a
// layout. Two controls on one controller overwrite each
// other; that may be meant, so it is shown and not refused. The two
// halves of one axis that bend the pitch up and down are one control.
func (d *ProfileDoc) Doubled(layout string, spots []Spot) map[Spot]bool {
	type sent struct {
		bend       bool
		controller uint8
		offset     uint8
	}
	by := map[sent][]Spot{}
	for _, at := range spots {
		switch role := d.RoleAt(layout, at); role.Kind {
		case RoleController:
			key := sent{controller: role.Controller, offset: role.Offset}
			by[key] = append(by[key], at)
		case RoleBend:
			key := sent{bend: true, offset: role.Offset}
			by[key] = append(by[key], at)
		}
	}
	out := map[Spot]bool{}
	for _, same := range by {
		several := false
		for _, at := range same {
			several = several || at.Code != same[0].Code
		}
		for _, at := range same {
			if several {
				out[at] = true
			}
		}
	}
	return out
}

// SetPassthrough leaves a key to the system: it plays nothing and types
// what it always types, also while HIDI is on.
func (d *ProfileDoc) SetPassthrough(layout string, code control.Code) error {
	if code.Kind != control.KindKey {
		return fmt.Errorf("%s is not a key; only a key can be left to the system", code)
	}
	return d.PutAt(layout, Whole(code), func(r *Role) { r.become(RolePass) })
}

// The same for a control itself.
func (d *ProfileDoc) SetNote(layout string, code control.Code, note midi.Note) error {
	return d.SetNoteAt(layout, Whole(code), note)
}
func (d *ProfileDoc) SetCommand(layout string, code control.Code, command engine.Command) error {
	return d.SetCommandAt(layout, Whole(code), command)
}
func (d *ProfileDoc) SetController(layout string, code control.Code, controller uint8, shape Shape) error {
	return d.SetControllerAt(layout, Whole(code), controller, shape)
}
func (d *ProfileDoc) SetBend(layout string, code control.Code, shape Shape) error {
	return d.SetBendAt(layout, Whole(code), shape)
}
func (d *ProfileDoc) Clear(layout string, code control.Code) error {
	return d.ClearAt(layout, Whole(code))
}
func (d *ProfileDoc) Carry(layout string, from, to control.Code, move bool) error {
	return d.CarryAt(layout, Whole(from), Whole(to), move)
}

// silence takes the line of a spot out of a section. Where the profile's
// rule would give the control a part once no line names it, the spot is
// told to do nothing instead: in the layout, also where it was the toggle,
// which is the toggle no more.
func (d *ProfileDoc) silence(text []byte, layout string, section config.Section, at Spot) ([]byte, error) {
	if len(d.generated(at.Code)) == 0 {
		return config.Remove(text, section, at.Key())
	}
	text, section, err := d.ownLine(text, layout, section, at)
	if err != nil {
		return nil, err
	}
	if text, err = d.keepOther(text, layout, at); err != nil {
		return nil, err
	}
	return config.Set(text, section, at.Key(), strconv.Quote(engine.NothingName))
}

// ClearAt makes a spot do nothing: it removes the binding the spot has
// in the layout, or the toggle of [global]. A control the profile's rule
// would then give a part is told to do nothing instead.
func (d *ProfileDoc) ClearAt(layout string, at Spot) error {
	return d.change(func(text []byte) ([]byte, error) {
		role := d.RoleAt(layout, at)
		out, err := d.silence(text, layout, role.Section, at)
		if err == nil && bytes.Equal(out, text) && !role.Silent {
			return nil, fmt.Errorf("the line of %s was not found; %w", at.Key(), config.ErrNotEditable)
		}
		return out, err
	})
}

// CarryAt gives one spot what another does in a layout, exactly as the
// profile writes it. With move the first does nothing afterwards;
// without, both do the same. It is one change, taken back in one step.
//
// The toggle is one for the profile and works in every layout: it can be
// moved to another key, and is then the toggle of that key everywhere,
// but not copied.
func (d *ProfileDoc) CarryAt(layout string, from, to Spot, move bool) error {
	if from == to {
		return nil
	}
	if d.RoleAt(layout, from).Section == config.Global {
		if !move {
			return fmt.Errorf("a profile has one %s; it can be moved to another key, not copied", engine.ToggleName)
		}
		return d.SetCommandAt(layout, to, engine.CommandToggle)
	}
	return d.change(func(text []byte) ([]byte, error) {
		source, target := d.RoleAt(layout, from), d.RoleAt(layout, to)
		value, found, err := config.Get(text, source.Section, from.Key())
		if err != nil {
			return nil, err
		}
		if !found && source.Generated && source.Kind != RoleNone {
			// What the rule makes of the control, written out.
			value, err = source.value(from)
			found = err == nil
		}
		if !found {
			if source.Kind == RoleNone {
				return nil, fmt.Errorf("%s does nothing; there is nothing to give to %s", from.Key(), to.Key())
			}
			return nil, fmt.Errorf("the line of %s was not found; %w", from.Key(), config.ErrNotEditable)
		}
		// The target gets its line in the layout; where it was the
		// toggle, it is the toggle no more.
		out, section, err := d.ownLine(text, layout, target.Section, to)
		if err != nil {
			return nil, err
		}
		if out, err = d.keepOther(out, layout, to); err != nil {
			return nil, err
		}
		if out, err = config.Set(out, section, to.Key(), value); err != nil {
			return nil, err
		}
		if move {
			return d.silence(out, layout, source.Section, from)
		}
		return out, nil
	})
}

// written returns what a spot does in a layout as a line writes it: its
// own line, or else what the profile's rule makes of it, written out. It
// reports false for a spot that does nothing.
func (d *ProfileDoc) written(text []byte, layout string, at Spot) (string, bool, error) {
	role := d.RoleAt(layout, at)
	value, found, err := config.Get(text, role.Section, at.Key())
	if err != nil {
		return "", false, err
	}
	if !found && role.Generated && role.Kind != RoleNone {
		value, err = role.value(at)
		found = err == nil
	}
	if !found && role.Kind != RoleNone {
		return "", false, fmt.Errorf("the line of %s was not found; %w", at.Key(), config.ErrNotEditable)
	}
	return value, found, nil
}

// SwapAt gives two spots what the other does in a layout, exactly as the
// profile writes it; one that does nothing leaves the other doing
// nothing. The toggle, which is one for the profile, goes to the other
// key, everywhere, as CarryAt moves it. It is one change, taken back in
// one step.
func (d *ProfileDoc) SwapAt(layout string, a, b Spot) error {
	if a == b {
		return nil
	}
	ra, rb := d.RoleAt(layout, a), d.RoleAt(layout, b)
	if rb.Section == config.Global {
		a, b, ra, rb = b, a, rb, ra // the toggle is a's from here on
	}
	return d.change(func(text []byte) ([]byte, error) {
		va, hasA, err := d.written(text, layout, a)
		if err != nil {
			return nil, err
		}
		vb, hasB, err := d.written(text, layout, b)
		if err != nil {
			return nil, err
		}
		out := text
		// The toggle moves: b is the toggle now, in [global], and a
		// gets what b did.
		if ra.Section == config.Global {
			if out, err = config.Remove(out, config.Global, a.Key()); err != nil {
				return nil, err
			}
			if !rb.Generated && (rb.Kind != RoleNone || rb.Silent) {
				if out, err = config.Remove(out, rb.Section, b.Key()); err != nil {
					return nil, err
				}
			}
			if out, err = config.Set(out, config.Global, b.Key(), va); err != nil {
				return nil, err
			}
			return d.put(out, layout, a, vb, hasB)
		}
		if out, err = d.put(out, layout, a, vb, hasB); err != nil {
			return nil, err
		}
		return d.put(out, layout, b, va, hasA)
	})
}

// put writes a line of a spot in a layout, or with none, makes the spot
// do nothing.
func (d *ProfileDoc) put(text []byte, layout string, at Spot, value string, has bool) ([]byte, error) {
	if !has {
		return d.silence(text, layout, config.Bindings(layout), at)
	}
	out, err := d.keepOther(text, layout, at)
	if err != nil {
		return nil, err
	}
	return config.Set(out, config.Bindings(layout), at.Key(), value)
}

// nextController returns the controller after n that means nothing of its
// own, or the one before it where the numbers end.
func nextController(n uint8) uint8 {
	for c := int(n) + 1; c <= 127; c++ {
		if !config.ReservedController(uint8(c)) {
			return uint8(c)
		}
	}
	return n - 1
}

// Split binds the two halves of an axis apart in place of the axis as a
// whole, each from the middle outwards, so that the axis at rest sends
// nothing at all instead of the middle of something. A controller becomes
// two, the upper half keeping the number and the lower getting the next;
// the pitch bends up by one half and down by the other; anything else
// stays on the upper half. Both halves can be changed afterwards like
// any control.
func (d *ProfileDoc) Split(layout string, code control.Code) error {
	return d.change(func(text []byte) ([]byte, error) {
		whole := d.Role(layout, code)
		if whole.Split {
			return text, nil
		}
		upper, lower := whole, Role{}
		upper.Shape.Centered = false
		switch whole.Kind {
		case RoleController:
			lower = upper
			lower.Controller = nextController(upper.Controller)
		case RoleBend:
			lower = upper
			upper.Down, lower.Down = false, true
		case RoleOther, RolePass:
			return nil, fmt.Errorf("what %s does is changed in the file; %w", code, config.ErrNotEditable)
		}
		if whole.Shape.Invert {
			// Reversed as a whole is the two halves the other way
			// round.
			upper, lower = lower, upper
			upper.Shape.Invert, lower.Shape.Invert = false, false
		}
		out := text
		var err error
		if !whole.Generated && (whole.Kind != RoleNone || whole.Silent) {
			if out, err = config.Remove(out, whole.Section, code.String()); err != nil {
				return nil, err
			}
		}
		for _, half := range []struct {
			at   Spot
			role Role
		}{{Spot{code, engine.RangeUpper}, upper}, {Spot{code, engine.RangeLower}, lower}} {
			value, err := half.role.value(half.at)
			if err != nil {
				return nil, err
			}
			if out, err = config.Set(out, whole.Section, half.at.Key(), value); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
}

// Join binds an axis as a whole in place of its two halves: it does what
// the upper half did, read from one end of its travel to the other with
// its rest in the middle. Halves that Split made of a reversed axis give
// that axis back, reversed.
func (d *ProfileDoc) Join(layout string, code control.Code) error {
	return d.change(func(text []byte) ([]byte, error) {
		upperAt, lowerAt := Spot{code, engine.RangeUpper}, Spot{code, engine.RangeLower}
		upper, lower := d.RoleAt(layout, upperAt), d.RoleAt(layout, lowerAt)
		out := text
		var err error
		for _, half := range []struct {
			at   Spot
			role Role
		}{{upperAt, upper}, {lowerAt, lower}} {
			if out, err = config.Remove(out, half.role.Section, half.at.Key()); err != nil {
				return nil, err
			}
		}
		whole := upper
		if whole.Kind == RoleNone {
			whole = lower
		}
		// Halves the way Split makes of an axis reversed as a whole join
		// into that axis again: the pitch bending down by the upper half,
		// or the lower half on the controller before the upper's.
		reversed := upper.Kind == RoleBend && lower.Kind == RoleBend && upper.Down && !lower.Down ||
			upper.Kind == RoleController && lower.Kind == RoleController && upper.Controller != lower.Controller &&
				nextController(lower.Controller) == upper.Controller
		if reversed {
			whole = lower
		}
		switch whole.Kind {
		case RoleController, RoleBend:
			whole.Shape.Centered, whole.Down = true, false
			whole.Shape.Invert = reversed
		case RoleOther, RolePass:
			return nil, fmt.Errorf("what %s does is changed in the file; %w", code, config.ErrNotEditable)
		case RoleNone:
			// Nothing to carry over: the axis is as a profile without
			// a line for it has it, the rule's where there is one.
			return out, nil
		}
		value, err := whole.value(Whole(code))
		if err != nil {
			return nil, err
		}
		return config.Set(out, upper.Section, code.String(), value)
	})
}

// Reload looks whether the file was changed by other hands since the
// window last read or wrote it. If nothing here waits to be saved, the
// file is taken over as it is now, and Reload reports that. If something
// does, what is shown stays and [ProfileDoc.Elsewhere] is set. A file
// that has mistakes now is not taken over, and the mistakes are returned.
func (d *ProfileDoc) Reload() (bool, error) {
	fresh, err := os.ReadFile(d.Path)
	if err != nil {
		return false, err
	}
	if bytes.Equal(fresh, d.saved) {
		// As the window last knew it: whatever other hands did, they
		// took it back, and saving writes over nothing of theirs.
		d.Elsewhere = false
		return false, nil
	}
	if d.Dirty() {
		// Our own save is not other hands.
		d.Elsewhere = !bytes.Equal(fresh, d.text)
		return false, nil
	}
	p, err := config.Parse(fresh)
	if err != nil {
		return false, fmt.Errorf("the file was changed meanwhile and has mistakes now:\n%w", err)
	}
	p.Source = d.Path
	d.text, d.saved, d.Profile, d.was = fresh, fresh, p, p
	d.undo, d.redo, d.Elsewhere = nil, nil, false
	return true, nil
}

// AddLayout adds a layout behind the others. It begins with what layout
// from does besides playing: its commands, among them those that lead
// from layout to layout, and the keys it leaves to the system or keeps
// silent. Every layout has its own of these, which can be changed apart
// from the others' afterwards; the notes and controllers are left for
// the new layout to get.
func (d *ProfileDoc) AddLayout(name, from string) error {
	return d.change(func(text []byte) ([]byte, error) {
		out, err := config.AddLayout(text, name)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(d.Layouts(), from) {
			return out, nil
		}
		return config.CopyBindings(out, from, name, func(a engine.Action) bool {
			switch a.(type) {
			case engine.Command, engine.Passthrough, engine.Nothing:
				return a != engine.CommandToggle
			}
			return false
		})
	})
}

// RemoveLayout takes a layout out, with its bindings. If the profile
// starts with it, it starts with its first layout from then on.
func (d *ProfileDoc) RemoveLayout(name string) error {
	return d.change(func(text []byte) ([]byte, error) {
		if d.Profile.Engine.Defaults.Layout == name {
			var err error
			if text, err = removeDefaultLayout(text, name); err != nil {
				return nil, err
			}
		}
		return config.RemoveLayout(text, name)
	})
}

// removeDefaultLayout takes the layout a profile starts with out of its
// defaults, where the file names it.
func removeDefaultLayout(text []byte, name string) ([]byte, error) {
	out, err := config.Remove(text, config.Defaults, "layout")
	if err != nil && bytes.Contains(text, []byte("[defaults]")) {
		return nil, err
	}
	if err != nil {
		return text, nil // no such section: nothing names it
	}
	return out, nil
}

// MoveLayout moves a layout one place towards the start (-1) or the end
// (1) of the row the player steps through.
func (d *ProfileDoc) MoveLayout(name string, by int) error {
	return d.change(func(text []byte) ([]byte, error) { return config.MoveLayout(text, name, by) })
}

// DefaultLayout returns the layout an instrument starts with.
func (d *ProfileDoc) DefaultLayout() string {
	if name := d.Profile.Engine.Defaults.Layout; name != "" {
		return name
	}
	if layouts := d.Layouts(); len(layouts) > 0 {
		return layouts[0]
	}
	return ""
}

// SetDefaultLayout makes a layout the one an instrument starts with.
func (d *ProfileDoc) SetDefaultLayout(name string) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, config.Defaults, "layout", strconv.Quote(name))
	})
}

// SetDeadzone sets the dead zone of the profile, which every axis has
// that names none of its own.
func (d *ProfileDoc) SetDeadzone(zone float64) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, config.Defaults, "deadzone", number(zone))
	})
}

// SetDefault sets one of the numbers an instrument starts with: "octave",
// "transpose", "channel" or "velocity".
func (d *ProfileDoc) SetDefault(key string, value int) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, config.Defaults, key, strconv.Itoa(value))
	})
}

// change applies a change to the text that is shown and remembers how it
// was before, so that the change can be taken back. On an error nothing
// changes. Nothing is written; see [ProfileDoc.Save].
//
// If nothing here waits to be saved, the change is made to the file as it
// is now: whoever edits it in an editor beside the window, as the window
// itself tells to for what it cannot change, must not lose that.
func (d *ProfileDoc) change(edit func([]byte) ([]byte, error)) error {
	if d.ReadOnly {
		return ErrFactory
	}
	if _, err := d.Reload(); err != nil {
		return err
	}
	// A profile in an older form is written in the new one with its
	// first change, so that the change lands where the new form has it:
	// the profile is shown as HIDI reads it, which is in the new form
	// already. The upgrade alone is no change.
	base := d.text
	if d.Profile.WrittenFor < config.Schema {
		var err error
		if base, err = config.Upgrade(d.text); err != nil {
			return err
		}
	}
	text, err := edit(base)
	if err != nil {
		return err
	}
	if bytes.Equal(text, base) {
		return nil
	}
	p, err := config.Parse(text)
	if err != nil {
		return err
	}
	d.undo = append(d.undo, d.text)
	if len(d.undo) > maxUndo {
		d.undo = d.undo[len(d.undo)-maxUndo:]
	}
	d.redo = nil
	p.Source = d.Path
	d.text, d.Profile = text, p
	d.settle()
	return nil
}

// writeWhole writes a file so that it is either there complete or not
// changed at all, also when the power fails in the middle: the new text
// goes to a file beside it, is brought to the disk, and then takes the
// place of the old file in one step.
func writeWhole(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// A file that is there keeps who may read it; a new one is a file
	// like any other of the user's.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp.Chmod(mode)
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
