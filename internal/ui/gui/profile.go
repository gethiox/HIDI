package gui

import (
	"bytes"
	"fmt"
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

// Role is what a control does when it is used alone, and where in the
// file that is written.
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
	// Split says that the two halves of an axis are bound apart, as
	// "ABS_X+" and "ABS_X-", which the window shows and does not change.
	// Lower and Upper say what the halves do, as Text does; empty for a
	// half that does nothing.
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
	// Centered is for an axis that rests in the middle, such as a stick.
	Centered bool
	// Invert reverses the direction.
	Invert bool
	// Deadzone is the fraction of travel next to the rest position that
	// is ignored, from 0 to below 1.
	Deadzone float64
}

// text writes the options as they stand in a binding, each led by a comma.
func (s Shape) text() string {
	out := ""
	if s.Centered {
		out += ", centered = true"
	}
	if s.Invert {
		out += ", invert = true"
	}
	if s.Deadzone > 0 {
		out += ", deadzone = " + strconv.FormatFloat(s.Deadzone, 'f', -1, 64)
	}
	return out
}

// Role returns what a control of the main part does in a layout: by the
// layout's own binding, or else by one that applies in every layout.
func (d *ProfileDoc) Role(layout string, code control.Code) Role {
	role := Role{Section: config.Bindings(layout)}
	named := false // whether a line names the control at all
	find := func(bindings []engine.Binding) (engine.Action, bool) {
		var action engine.Action
		found := false
		for _, b := range bindings {
			if b.Source.Code != code || b.Source.Part != "" {
				continue
			}
			named = true
			if len(b.Source.Modifiers) != 0 {
				continue
			}
			if b.Source.Range != engine.RangeWhole {
				role.half(b.Source.Range, b.Action)
				continue
			}
			action, found = b.Action, true
			role.Shape = Shape{Centered: b.Source.Centered, Invert: b.Source.Invert, Deadzone: b.Source.Deadzone}
		}
		return action, found
	}
	var action engine.Action
	found := false
	for _, l := range d.Profile.Engine.Layouts {
		if l.Name == layout {
			action, found = find(l.Bindings)
		}
	}
	if !found {
		if action, found = find(d.Profile.Engine.Global); found {
			role.Section = config.Global
		}
	}
	if !named {
		for _, g := range d.generated(code) {
			role.Generated = true
			if g.Source.Range != engine.RangeWhole {
				role.half(g.Source.Range, g.Action)
				continue
			}
			action = g.Action
		}
	}
	role.take(action)
	return role
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

// half notes that one half of an axis is bound by itself, and to what.
// The first binding seen for a half is the one that counts: a layout's
// own comes before one that applies in every layout.
func (r *Role) half(which engine.Range, action engine.Action) {
	r.Split = true
	var does Role
	does.take(action)
	switch {
	case which == engine.RangeLower && r.Lower == "":
		r.Lower = does.Text
	case which == engine.RangeUpper && r.Upper == "":
		r.Upper = does.Text
	}
}

// silence takes the line of a control out of a section. Where the
// profile's rule would give the control a part once no line names it, the
// line stays and says that the control does nothing.
func (d *ProfileDoc) silence(text []byte, section config.Section, code control.Code) ([]byte, error) {
	if len(d.generated(code)) > 0 {
		return config.Set(text, section, code.String(), strconv.Quote(engine.NothingName))
	}
	return config.Remove(text, section, code.String())
}

// SetPassthrough leaves a key to the system: it plays nothing and types
// what it always types, also while HIDI is on.
func (d *ProfileDoc) SetPassthrough(layout string, code control.Code) error {
	if code.Kind != control.KindKey {
		return fmt.Errorf("%s is not a key; only a key can be left to the system", code)
	}
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, d.Role(layout, code).Section, code.String(), strconv.Quote(engine.PassthroughName))
	})
}

// offset writes the channel a binding is moved by, if it is.
func offset(n uint8) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", channel_offset = %d", n)
}

// SetNote makes a control play a note. A control that plays a note
// already keeps what else its line says about the note.
func (d *ProfileDoc) SetNote(layout string, code control.Code, note midi.Note) error {
	return d.change(func(text []byte) ([]byte, error) {
		was := d.Role(layout, code)
		value := strconv.Quote(note.String())
		if was.Kind == RoleNote && (was.Offset != 0 || was.Velocity != 0 || was.Touch || was.Shape != (Shape{})) {
			value = "{ note = " + value
			if was.Velocity != 0 {
				value += fmt.Sprintf(", velocity = %d", was.Velocity)
			}
			if was.Touch {
				value += ", touch = true"
			}
			value += offset(was.Offset) + was.Shape.text() + " }"
		}
		return config.Set(text, was.Section, code.String(), value)
	})
}

// SetCommand makes a control run a command.
func (d *ProfileDoc) SetCommand(layout string, code control.Code, command engine.Command) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, d.Role(layout, code).Section, code.String(), strconv.Quote(command.String()))
	})
}

// SetController makes a control send a controller: an axis its position,
// read as shape says, and a key 127 while it is held.
func (d *ProfileDoc) SetController(layout string, code control.Code, controller uint8, shape Shape) error {
	if controller > 127 {
		return fmt.Errorf("there is no controller %d; they go from 0 to 127", controller)
	}
	return d.change(func(text []byte) ([]byte, error) {
		was := d.Role(layout, code)
		moved := ""
		if was.Kind == RoleController {
			moved = offset(was.Offset)
		}
		value := fmt.Sprintf("{ cc = %d%s%s }", controller, moved, shape.text())
		return config.Set(text, was.Section, code.String(), value)
	})
}

// SetBend makes a control bend the pitch: both ways for a centered axis,
// upwards otherwise.
func (d *ProfileDoc) SetBend(layout string, code control.Code, shape Shape) error {
	return d.change(func(text []byte) ([]byte, error) {
		was := d.Role(layout, code)
		way, moved := "true", ""
		if was.Kind == RoleBend {
			moved = offset(was.Offset)
			if was.Down {
				way = `"down"`
			}
		}
		value := fmt.Sprintf("{ pitch_bend = %s%s%s }", way, moved, shape.text())
		return config.Set(text, was.Section, code.String(), value)
	})
}

// Clear makes a control do nothing: it removes the binding the control
// has in the layout, or the one that applies in every layout. A control
// the profile's rule would then give a part is told to do nothing
// instead.
func (d *ProfileDoc) Clear(layout string, code control.Code) error {
	return d.change(func(text []byte) ([]byte, error) {
		role := d.Role(layout, code)
		out, err := d.silence(text, role.Section, code)
		if err == nil && bytes.Equal(out, text) && !role.Silent {
			return nil, fmt.Errorf("the line of %s was not found; %w", code, config.ErrNotEditable)
		}
		return out, err
	})
}

// Carry gives one control what another does in a layout, exactly as the
// profile writes it. With move the first control does nothing afterwards;
// without, both do the same. It is one change, taken back in one step.
func (d *ProfileDoc) Carry(layout string, from, to control.Code, move bool) error {
	if from == to {
		return nil
	}
	return d.change(func(text []byte) ([]byte, error) {
		source, target := d.Role(layout, from), d.Role(layout, to)
		value, found, err := config.Get(text, source.Section, from.String())
		if err != nil {
			return nil, err
		}
		if !found && source.Generated && !source.Split {
			// What the rule makes of the control, written out.
			value, found = d.generated(from)[0].Value, true
		}
		if !found {
			if source.Kind == RoleNone {
				return nil, fmt.Errorf("%s does nothing; there is nothing to give to %s", from, to)
			}
			return nil, fmt.Errorf("the line of %s was not found; %w", from, config.ErrNotEditable)
		}
		// Where the target is bound already, it is bound anew there;
		// a target that does nothing gets its line beside the source's.
		section := target.Section
		if target.Kind == RoleNone {
			section = source.Section
		}
		out, err := config.Set(text, section, to.String(), value)
		if err != nil {
			return nil, err
		}
		if move {
			return d.silence(out, source.Section, from)
		}
		return out, nil
	})
}

// Reload looks whether the file was changed by other hands since the
// window last read or wrote it. If nothing here waits to be saved, the
// file is taken over as it is now, and Reload reports that. If something
// does, what is shown stays and [ProfileDoc.Elsewhere] is set. A file
// that has mistakes now is not taken over, and the mistakes are returned.
func (d *ProfileDoc) Reload() (bool, error) {
	fresh, err := os.ReadFile(d.Path)
	if err != nil || bytes.Equal(fresh, d.saved) {
		return false, err
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

// AddLayout adds an empty layout behind the others.
func (d *ProfileDoc) AddLayout(name string) error {
	return d.change(func(text []byte) ([]byte, error) { return config.AddLayout(text, name) })
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
	text, err := edit(d.text)
	if err != nil {
		return err
	}
	if bytes.Equal(text, d.text) {
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
	if info, err := os.Stat(path); err == nil {
		tmp.Chmod(info.Mode().Perm())
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
