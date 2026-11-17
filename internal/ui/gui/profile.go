package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// ProfileDoc is a profile file that is open for changing in the window.
// Every change is made in the text of the file, one key at a time, so
// that what a person wrote there by hand stays; the file is written at
// once, and a running HIDI takes it over like any other change to it.
type ProfileDoc struct {
	Path    string
	Profile *config.Profile
	text    []byte
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
	return &ProfileDoc{Path: path, Profile: p, text: text}, nil
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
	// Split says that the two halves of an axis are bound apart, as
	// "ABS_X+" and "ABS_X-", which the window shows and does not change.
	Split bool
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
	find := func(bindings []engine.Binding) (engine.Action, bool) {
		var action engine.Action
		found := false
		for _, b := range bindings {
			if b.Source.Code != code || b.Source.Part != "" || len(b.Source.Modifiers) != 0 {
				continue
			}
			if b.Source.Range != engine.RangeWhole {
				role.Split = true
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
	switch a := action.(type) {
	case engine.Note:
		role.Kind, role.Note, role.Text = RoleNote, a.Note, a.Note.String()
	case engine.Command:
		role.Kind, role.Command, role.Text = RoleCommand, a, a.String()
	case engine.ControlChange:
		role.Kind, role.Controller, role.Text = RoleController, a.Controller, fmt.Sprintf("cc %d", a.Controller)
	case engine.ProgramChange:
		role.Kind, role.Text = RoleOther, fmt.Sprintf("program %d", a.Program)
	case engine.PitchBend:
		role.Kind, role.Text = RoleBend, "pitch bend"
	}
	return role
}

// SetNote makes a control play a note.
func (d *ProfileDoc) SetNote(layout string, code control.Code, note midi.Note) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, d.Role(layout, code).Section, code.String(), strconv.Quote(note.String()))
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
		value := fmt.Sprintf("{ cc = %d%s }", controller, shape.text())
		return config.Set(text, d.Role(layout, code).Section, code.String(), value)
	})
}

// SetBend makes a control bend the pitch: both ways for a centered axis,
// upwards otherwise.
func (d *ProfileDoc) SetBend(layout string, code control.Code, shape Shape) error {
	return d.change(func(text []byte) ([]byte, error) {
		value := fmt.Sprintf("{ pitch_bend = true%s }", shape.text())
		return config.Set(text, d.Role(layout, code).Section, code.String(), value)
	})
}

// Clear makes a control do nothing: it removes the binding the control
// has in the layout, or the one that applies in every layout.
func (d *ProfileDoc) Clear(layout string, code control.Code) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Remove(text, d.Role(layout, code).Section, code.String())
	})
}

// SetDefault sets one of the numbers an instrument starts with: "octave",
// "transpose", "channel" or "velocity".
func (d *ProfileDoc) SetDefault(key string, value int) error {
	return d.change(func(text []byte) ([]byte, error) {
		return config.Set(text, config.Defaults, key, strconv.Itoa(value))
	})
}

// change applies a change to the text, writes the file, and reads the
// profile anew. On an error nothing is written and nothing changes.
func (d *ProfileDoc) change(edit func([]byte) ([]byte, error)) error {
	text, err := edit(d.text)
	if err != nil {
		return err
	}
	p, err := config.Parse(text)
	if err != nil {
		return err
	}
	if err := writeWhole(d.Path, text); err != nil {
		return err
	}
	p.Source = d.Path
	d.text, d.Profile = text, p
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
