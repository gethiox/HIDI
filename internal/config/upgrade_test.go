package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

// A profile of schema 1, where [global] was shared by the layouts.
const sharedGlobal = `schema = 1
name = "Old"

[global]
# The toggle stays here.
KEY_CAPSLOCK = "hidi_toggle"
KEY_F1 = "octave_down" # down an octave
"KEY_LEFTSHIFT+KEY_F1" = { command = "transpose_down" }

[[layout]]
name = "Own"

[layout.bindings]
KEY_F1 = "C1"
KEY_Z = "D1"

[[layout]]
name = "Bare"
`

// bindingIn returns the binding of a source in a layout, by the source as
// a file writes it.
func bindingIn(t *testing.T, p *Profile, layout, key string) (engine.Binding, bool) {
	t.Helper()
	for _, l := range p.Engine.Layouts {
		if l.Name != layout {
			continue
		}
		for _, b := range l.Bindings {
			if SourceKey(b.Source) == key {
				return b, true
			}
		}
	}
	return engine.Binding{}, false
}

// A file of schema 1 is read as it always worked: every layout has the
// bindings of [global] its own lines did not replace, and [global] keeps
// the toggle alone.
func TestSharedGlobalGoesIntoEveryLayout(t *testing.T) {
	p, err := Parse([]byte(sharedGlobal))
	if err != nil {
		t.Fatal(err)
	}
	if p.WrittenFor != 1 {
		t.Errorf("WrittenFor = %d", p.WrittenFor)
	}
	if len(p.Engine.Global) != 1 || p.Engine.Global[0].Action != engine.CommandToggle || len(p.Toggle) != 1 {
		t.Errorf("Global = %+v, Toggle = %v", p.Engine.Global, p.Toggle)
	}
	for _, c := range []struct {
		layout, key string
		want        engine.Action
	}{
		{"Own", "KEY_F1", engine.Note{Note: 36}},
		{"Own", "KEY_LEFTSHIFT+KEY_F1", engine.CommandTransposeDown},
		{"Bare", "KEY_F1", engine.CommandOctaveDown},
		{"Bare", "KEY_LEFTSHIFT+KEY_F1", engine.CommandTransposeDown},
	} {
		if b, ok := bindingIn(t, p, c.layout, c.key); !ok || b.Action != c.want {
			t.Errorf("layout %s, %s: %+v (found %v), want %v", c.layout, c.key, b.Action, ok, c.want)
		}
	}
}

// From schema 2 on, [global] holds the toggle and nothing else.
func TestGlobalHoldsOnlyTheToggle(t *testing.T) {
	text := strings.Replace(sharedGlobal, "schema = 1", "schema = 2", 1)
	_, err := Parse([]byte(text))
	if err == nil || !strings.Contains(err.Error(), "only hidi_toggle belongs in [global]") {
		t.Fatalf("Parse: %v", err)
	}
	if n := strings.Count(err.Error(), "only hidi_toggle"); n != 2 {
		t.Errorf("%d bindings refused, want 2:\n%v", n, err)
	}
}

// Upgrade writes a file of schema 1 in the new form, and it is the same
// profile it was.
func TestUpgrade(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		old := []byte(strings.ReplaceAll(sharedGlobal, "\n", ending))
		out, err := Upgrade(old)
		if err != nil {
			t.Fatal(err)
		}
		was, _ := Parse(old)
		now, err := Parse(out)
		if err != nil {
			t.Fatalf("the upgraded profile:\n%v\n%s", err, out)
		}
		if now.WrittenFor != Schema {
			t.Errorf("upgraded to schema %d", now.WrittenFor)
		}
		now.WrittenFor = was.WrittenFor
		if !Same(was, now) {
			t.Errorf("the upgraded profile differs:\n%s", out)
		}
		text := string(out)
		for _, want := range []string{
			"schema = 2" + ending,
			"# The toggle stays here." + ending + `KEY_CAPSLOCK = "hidi_toggle"` + ending + ending + "[[layout]]",
			`KEY_F1 = "C1"` + ending + `KEY_Z = "D1"` + ending + `"KEY_LEFTSHIFT+KEY_F1" = { command = "transpose_down" }` + ending,
			`[layout.bindings]` + ending + `KEY_F1 = "octave_down"` + ending + `"KEY_LEFTSHIFT+KEY_F1" = { command = "transpose_down" }` + ending,
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%q is not in the upgraded profile:\n%s", want, text)
			}
		}
		if ending == "\r\n" && strings.Count(text, "\n") != strings.Count(text, "\r\n") {
			t.Errorf("a line of the upgraded profile ends otherwise than the file's:\n%q", text)
		}
		again, err := Upgrade(out)
		if err != nil || !bytes.Equal(again, out) {
			t.Errorf("a profile of the current schema changed: %v\n%s", err, again)
		}
	}
}

// A profile for one device, made of one of schema 1, has the new form.
func TestForDeviceUpgrades(t *testing.T) {
	out, err := ForDevice([]byte(sharedGlobal), "Pad", control.DeviceGamepad, control.HardwareID{Vendor: 0x044f, Product: 0xb10a})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(out)
	if err != nil || p.WrittenFor != Schema {
		t.Fatalf("%v, written for %d:\n%s", err, p.WrittenFor, out)
	}
}
