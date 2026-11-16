package config

import (
	"errors"
	"strings"
	"testing"
)

const edited = `# My keyboard, tuned by hand.
schema = 1
name = "Mine"   # shown in the window

[match]
type = "keyboard"

[defaults]
channel = 1
velocity = 100 # loud
layout = "Piano"

[global]
KEY_F1 = "octave_down"
"KEY_LEFTSHIFT+KEY_F1" = "transpose_down"  # with shift
KEY_ESC = "panic"

# The main layout.
[[layout]]
name = "Piano"

[layout.bindings]
KEY_Z = "C3"      # middle C
KEY_X = { note = "D3", velocity = 90 }   # a table, with a # in "a#b" nowhere

[[layout]]
name = 'Drums'

[layout.bindings]
KEY_Z = "C1"
`

func mustEdit(t *testing.T, text string, f func([]byte) ([]byte, error)) string {
	t.Helper()
	out, err := f([]byte(text))
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	return string(out)
}

// diff returns the lines that differ, for a test that expects one change.
func diff(a, b string) (removed, added []string) {
	as, bs := strings.Split(a, "\n"), strings.Split(b, "\n")
	count := map[string]int{}
	for _, l := range as {
		count[l]++
	}
	for _, l := range bs {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, l)
		}
	}
	count = map[string]int{}
	for _, l := range bs {
		count[l]++
	}
	for _, l := range as {
		if count[l] > 0 {
			count[l]--
		} else {
			removed = append(removed, l)
		}
	}
	return removed, added
}

func TestSetKeepsEverythingElse(t *testing.T) {
	for _, tt := range []struct {
		name           string
		section        Section
		key, value     string
		removed, added string
	}{
		{"a binding, its note kept", Bindings("Piano"), "KEY_Z", `"E3"`,
			`KEY_Z = "C3"      # middle C`, `KEY_Z = "E3"      # middle C`},
		{"the same key in another layout", Bindings("Drums"), "KEY_Z", `"D1"`,
			`KEY_Z = "C1"`, `KEY_Z = "D1"`},
		{"a table for a plain value", Bindings("Piano"), "KEY_X", `"D#3"`,
			`KEY_X = { note = "D3", velocity = 90 }   # a table, with a # in "a#b" nowhere`,
			`KEY_X = "D#3"   # a table, with a # in "a#b" nowhere`},
		{"a default", Defaults, "velocity", "64", `velocity = 100 # loud`, `velocity = 64 # loud`},
		{"a key in quotes", Global, "KEY_LEFTSHIFT+KEY_F1", `"transpose_up"`,
			`"KEY_LEFTSHIFT+KEY_F1" = "transpose_down"  # with shift`, `"KEY_LEFTSHIFT+KEY_F1" = "transpose_up"  # with shift`},
	} {
		out := mustEdit(t, edited, func(b []byte) ([]byte, error) { return Set(b, tt.section, tt.key, tt.value) })
		removed, added := diff(edited, out)
		if len(removed) != 1 || len(added) != 1 || removed[0] != tt.removed || added[0] != tt.added {
			t.Errorf("%s:\n removed %q\n   added %q", tt.name, removed, added)
		}
	}
}

func TestSetAddsAtTheEndOfItsSection(t *testing.T) {
	out := mustEdit(t, edited, func(b []byte) ([]byte, error) { return Set(b, Bindings("Piano"), "KEY_C", `"E3"`) })
	removed, added := diff(edited, out)
	if len(removed) != 0 || len(added) != 1 || added[0] != `KEY_C = "E3"` {
		t.Fatalf("removed %q, added %q", removed, added)
	}
	// Right after the last binding of that layout, before the blank line
	// and the next layout.
	if !strings.Contains(out, "nowhere\nKEY_C = \"E3\"\n\n[[layout]]\nname = 'Drums'") {
		t.Errorf("the new binding is in the wrong place:\n%s", out)
	}

	// A key that needs quotes gets them; the last section of the file,
	// and a file without a line break at its end.
	out = mustEdit(t, strings.TrimSuffix(edited, "\n"), func(b []byte) ([]byte, error) {
		return Set(b, Bindings("Drums"), "ABS_RX+", `{ cc = 1, deadzone = 0.1 }`)
	})
	if !strings.HasSuffix(out, "KEY_Z = \"C1\"\n\"ABS_RX+\" = { cc = 1, deadzone = 0.1 }\n") {
		t.Errorf("the end of the file:\n%q", out[len(out)-80:])
	}
	p, err := Parse([]byte(out))
	if err != nil || len(p.Engine.Layouts[1].Bindings) != 2 {
		t.Errorf("the result: %v", err)
	}
}

func TestRemove(t *testing.T) {
	out := mustEdit(t, edited, func(b []byte) ([]byte, error) { return Remove(b, Global, "KEY_F1") })
	removed, added := diff(edited, out)
	if len(added) != 0 || len(removed) != 1 || removed[0] != `KEY_F1 = "octave_down"` {
		t.Errorf("removed %q, added %q", removed, added)
	}
	// A key that is not there changes nothing.
	if out := mustEdit(t, edited, func(b []byte) ([]byte, error) { return Remove(b, Global, "KEY_F9") }); out != edited {
		t.Error("removing a key that is not there changed the text")
	}
}

func TestEditRefuses(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func([]byte) ([]byte, error)
		want string
	}{
		{"a value that is not valid", func(b []byte) ([]byte, error) { return Set(b, Bindings("Piano"), "KEY_Z", `"H9"`) }, "valid profile"},
		{"a key that is no control", func(b []byte) ([]byte, error) { return Set(b, Global, "KEY_NOPE", `"panic"`) }, "valid profile"},
		{"a layout that is not there", func(b []byte) ([]byte, error) { return Set(b, Bindings("Organ"), "KEY_Z", `"C3"`) }, "Organ"},
		{"a line break", func(b []byte) ([]byte, error) { return Set(b, Global, "KEY_F2", "\"a\"\nx = 1") }, "line break"},
		{"removing what the profile needs", func(b []byte) ([]byte, error) { return Remove(b, Defaults, "layout") }, ""},
	} {
		out, err := tt.edit([]byte(edited))
		if tt.want == "" {
			// Allowed or not, the answer is a valid profile or an error.
			if err == nil {
				if _, perr := Parse(out); perr != nil {
					t.Errorf("%s: an invalid profile was returned: %v", tt.name, perr)
				}
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want one about %q", tt.name, err, tt.want)
		}
	}

	// Text written in a way the editor does not follow is left alone.
	multi := strings.Replace(edited, `KEY_X = { note = "D3", velocity = 90 }`, "KEY_X = { note = \"D3\",\n  velocity = 90 }", 1)
	if _, err := Set([]byte(multi), Bindings("Piano"), "KEY_X", `"E3"`); !errors.Is(err, ErrNotEditable) {
		t.Errorf("a value over several lines: %v", err)
	}
	// A key before it in the section can still be changed.
	if out, err := Set([]byte(multi), Bindings("Piano"), "KEY_Z", `"E3"`); err != nil || !strings.Contains(string(out), "velocity = 90 }") {
		t.Errorf("a key before a value over several lines: %v", err)
	}
	inline := "schema = 1\nname = \"x\"\nglobal = { KEY_ESC = \"panic\" }\n[[layout]]\nname = \"L\"\n"
	if _, err := Set([]byte(inline), Global, "KEY_F1", `"panic"`); !errors.Is(err, ErrNotEditable) {
		t.Errorf("a section written in one line: %v", err)
	}
}

// Every built-in profile can be changed in every section it has.
func TestEditBuiltinProfiles(t *testing.T) {
	files, err := BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		p, err := Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Set(text, Defaults, "velocity", "101")
		if err != nil {
			t.Errorf("%s: defaults: %v", name, err)
			continue
		}
		layout := p.Engine.Layouts[len(p.Engine.Layouts)-1].Name
		out, err = Set(out, Bindings(layout), "KEY_F24", `"C3"`)
		if err != nil {
			t.Errorf("%s: bindings of %q: %v", name, layout, err)
			continue
		}
		got, err := Parse(out)
		if err != nil || got.Engine.Defaults.Velocity != 101 {
			t.Errorf("%s: after the changes: %v", name, err)
		}
		if removed, added := diff(string(text), string(out)); len(added) != 2 || len(removed) > 1 {
			t.Errorf("%s: %d lines removed, %d added", name, len(removed), len(added))
		}
	}
}
