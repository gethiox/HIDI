package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

const ruleProfile = `
schema = 1
name = "Rule"
[match]
type = "gamepad"
[generate]
notes = "C1"
controllers = 16
[global]
BTN_START = "panic"
[[layout]]
name = "One"
[layout.bindings]
ABS_Y = { pitch_bend = true, centered = true }
"ABS_HAT0X+" = "octave_up"
BTN_B = "nothing"
[[layout]]
name = "Two"
[layout.bindings]
BTN_A = "C5"
`

// stick is a controller with five buttons, three axes and a hat, listed
// in no order, one of them twice, and with a part that is not its own.
func stick() []control.Input {
	return []control.Input{
		{Code: control.Axis(0x11), Hat: true}, {Code: control.Axis(0x10), Hat: true},
		{Code: control.Key(0x13b)}, // BTN_START
		{Code: control.Key(0x131)}, {Code: control.Key(0x130)}, {Code: control.Key(0x130)},
		{Code: control.Key(0x133)}, {Code: control.Key(0x134)},
		{Code: control.Axis(2)}, {Code: control.Axis(0)}, {Code: control.Axis(1)},
		{Code: control.Key(0x110), Part: "Touchpad"}, {Code: control.Axis(0), Part: "Touchpad"},
	}
}

// does returns what a control does in a layout of an instrument, by the
// layout's own bindings, as "note 36", "cc 16" and the like, the halves
// of an axis marked.
func does(p engine.Profile, layout string) map[string]string {
	out := map[string]string{}
	for _, l := range p.Layouts {
		if l.Name != layout {
			continue
		}
		for _, b := range l.Bindings {
			key := b.Source.Code.String()
			switch b.Source.Range {
			case engine.RangeLower:
				key += "-"
			case engine.RangeUpper:
				key += "+"
			}
			switch a := b.Action.(type) {
			case engine.Note:
				out[key] = "note " + a.Note.String()
			case engine.ControlChange:
				out[key] = "cc " + midi.Note(a.Controller).String()
			case engine.Nothing:
				out[key] = "nothing"
			default:
				out[key] = "named"
			}
		}
	}
	return out
}

func TestGenerate(t *testing.T) {
	p, err := Parse([]byte(ruleProfile))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Generate.Notes || p.Generate.FirstNote != 36 || !p.Generate.Controllers || p.Generate.FirstController != 16 {
		t.Fatalf("the rule: %+v", p.Generate)
	}
	made := p.For(stick())
	if err := made.Validate(); err != nil {
		t.Fatalf("what was made is no instrument: %v", err)
	}
	cc := func(n uint8) string { return "cc " + midi.Note(n).String() }

	// Layout One: the buttons by their numbers, in the order of their
	// codes; B is told to be silent, START and ABS_Y are named, and so
	// is the hat's first axis, by one half. The hat's directions are the
	// four notes after those of the five buttons; its other axis gives
	// the last two of them.
	one := does(made, "One")
	for key, want := range map[string]string{
		"BTN_SOUTH": "note C1", "BTN_EAST": "nothing", "BTN_NORTH": "note D1", "BTN_WEST": "note D#1",
		"ABS_X": cc(16), "ABS_Y": "named", "ABS_Z": cc(18),
		"ABS_HAT0X+": "named", "ABS_HAT0Y-": "note G1", "ABS_HAT0Y+": "note G#1",
	} {
		if one[key] != want {
			t.Errorf("layout One, %s: %q, want %q", key, one[key], want)
		}
	}
	for _, key := range []string{"BTN_START", "ABS_HAT0X-", "ABS_HAT0X", "BTN_LEFT"} {
		if got, there := one[key]; there {
			t.Errorf("layout One, %s: %q, want no binding of the layout", key, got)
		}
	}
	// Layout Two names A itself, and nothing else: B plays there, and
	// so does the whole hat.
	two := does(made, "Two")
	for key, want := range map[string]string{
		"BTN_SOUTH": "note C5", "BTN_EAST": "note C#1", "ABS_Y": cc(17),
		"ABS_HAT0X-": "note F1", "ABS_HAT0X+": "note F#1",
	} {
		if two[key] != want {
			t.Errorf("layout Two, %s: %q, want %q", key, two[key], want)
		}
	}
	if _, there := two["BTN_START"]; there {
		t.Error("a control named for every layout was given a part by the rule")
	}

	// The same device gives the same instrument, however it lists what
	// it has.
	shuffled := stick()
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	if a, b := does(p.For(shuffled), "One"), one; len(a) != len(b) {
		t.Errorf("another order of the list gives another instrument: %v", a)
	} else {
		for key, want := range b {
			if a[key] != want {
				t.Errorf("another order of the list, %s: %q, want %q", key, a[key], want)
			}
		}
	}

	// What was written is not touched, and a device that lists nothing
	// gets what was written.
	if len(p.Engine.Layouts[1].Bindings) != 1 {
		t.Error("making an instrument changed the profile")
	}
	if got := p.For(nil); len(got.Layouts[0].Bindings) != len(p.Engine.Layouts[0].Bindings) {
		t.Error("a device that lists nothing was given bindings")
	}

	// Every generated binding can be written as a line that says the
	// same.
	var lines strings.Builder
	lines.WriteString("schema = 1\nname = \"Lines\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\n")
	generated := p.Generated(stick())
	for _, g := range generated {
		lines.WriteString(`"` + g.Key + `" = ` + g.Value + "\n")
	}
	written, err := Parse([]byte(lines.String()))
	if err != nil {
		t.Fatalf("the generated lines are no profile: %v\n%s", err, lines.String())
	}
	if got := written.Engine.Layouts[0].Bindings; len(got) != len(generated) {
		t.Errorf("%d lines, %d bindings", len(generated), len(got))
	} else {
		byKey := does(written.Engine, "L")
		for _, g := range generated {
			if want := does(engine.Profile{Layouts: []engine.Layout{{Name: "L", Bindings: []engine.Binding{g.Binding}}}}, "L")[g.Key]; byKey[g.Key] != want {
				t.Errorf("%s = %s reads as %q, want %q", g.Key, g.Value, byKey[g.Key], want)
			}
		}
	}
}

func TestGenerateRunsOut(t *testing.T) {
	// Controllers that mean something of their own are stepped over.
	for n, want := range map[int]uint8{0: 16, 15: 31, 16: 70, 41: 95, 42: 102, 59: 119} {
		if got, ok := controller(16, n); !ok || got != want {
			t.Errorf("axis %d sends controller %d, %v; want %d", n, got, ok, want)
		}
	}
	if got, ok := controller(16, 60); ok {
		t.Errorf("axis 60 was given controller %d", got)
	}
	if got, ok := controller(0, 0); !ok || got != 1 {
		t.Errorf("the first controller from 0 on is %d, %v", got, ok)
	}

	// A device with more buttons than there are notes left: the rest
	// stay silent, and nothing is out of range.
	p, err := Parse([]byte("schema = 1\nname = \"High\"\n[generate]\nnotes = 120\n[[layout]]\nname = \"L\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	var many []control.Input
	for n := range 40 {
		many = append(many, control.Input{Code: control.Key(0x120 + uint16(n))})
	}
	many = append(many, control.Input{Code: control.Axis(0)})
	made := p.For(many)
	if err := made.Validate(); err != nil {
		t.Fatal(err)
	}
	if n := len(made.Layouts[0].Bindings); n != 8 {
		t.Errorf("%d bindings, want the 8 notes from 120 to 127 and no controller", n)
	}
}

func TestGenerateErrors(t *testing.T) {
	for text, want := range map[string]string{
		"[generate]\nnotes = \"H1\"\n":      "generate: notes:",
		"[generate]\ncontrollers = 128\n":   "controllers",
		"[generate]\nbuttons = 3\n":         "buttons",
		"generate = 5\n":                    "generate",
		"[generate]\nnotes = 3\nnotes2=1\n": "notes2",
	} {
		_, err := Parse([]byte("schema = 1\nname = \"P\"\n" + text + "[[layout]]\nname = \"L\"\n"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want it to mention %q", text, err, want)
		}
	}
}

// HIDI comes with two profiles, one for keyboards and one for game
// controllers, and none for one product: a controller of any make plays
// by the general one.
func TestBuiltinProfilesAreGeneral(t *testing.T) {
	profiles, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Errorf("%d built-in profiles, want one for keyboards and one for game controllers", len(profiles))
	}
	for _, p := range profiles {
		m := p.Match
		if m.Type == control.DeviceUnknown || m.Bus != nil || m.Vendor != nil || m.Product != nil || m.Version != nil || m.Serial != "" {
			t.Errorf("%s is for %s, want a kind of device and nothing more", p.Name, m)
		}
	}
	for _, hardware := range []control.HardwareID{
		{Bus: 3, Vendor: 0x28de, Product: 0x1102}, // a Steam Controller
		{Bus: 3, Vendor: 0x054c, Product: 0x09cc}, // a PS4 controller
		{Bus: 3, Vendor: 0x0738, Product: 0x2221}, // a flight stick
	} {
		info := control.DeviceInfo{Type: control.DeviceGamepad, Hardware: hardware}
		p := Select(profiles, info)
		if p == nil || p.Name != "Default gamepad" {
			t.Fatalf("%s gets %v", hardware, p)
		}
		if !p.Generate.Notes || !p.Generate.Controllers {
			t.Errorf("the general profile has no rule for what it does not name: %+v", p.Generate)
		}
	}
}

// The profiles written for one product, which HIDI no longer comes with,
// are kept as examples, and are still profiles.
func TestExampleProfiles(t *testing.T) {
	files, err := filepath.Glob("../../docs/examples/profiles/*.toml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no example profiles: %v", err)
	}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(text)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if p.Match.Vendor == nil {
			t.Errorf("%s is not for the devices of one maker", file)
		}
	}
}
