package config

import (
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

func mustCode(t *testing.T, name string) control.Code {
	t.Helper()
	code, err := control.ParseCode(name)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

const fullProfile = `
schema = 1
name = "Test"

[match]
type = "gamepad"
vendor = 0x054c
product = 0x09cc
serial = "abc"

[defaults]
octave = 1
transpose = -2
channel = 3
velocity = 90
layout = "Second"
collision = "no_repeat"

[global]
KEY_F1 = "octave_down"
"KEY_LEFTSHIFT+KEY_F1" = "transpose_down"

[[layout]]
name = "First"

[layout.bindings]
KEY_Z = "C1"
KEY_X = 61
KEY_Q = { note = "D#3", channel_offset = 1, velocity = 127 }
KEY_SPACE = { cc = 64 }
KEY_1 = { program = 5, channel_offset = 2 }
KEY_2 = { command = "panic" }
ABS_X = { pitch_bend = true, centered = true, deadzone = 0.1, invert = true }
"ABS_Y+" = { cc = 1, deadzone = 0.25 }
"ABS_Y-" = { pitch_bend = "down" }
"Touchpad:ABS_X" = { cc = 10 }

[[layout]]
name = "Second"
`

func TestParse(t *testing.T) {
	p, err := Parse([]byte(fullProfile))
	if err != nil {
		t.Fatalf("Parse:\n%v", err)
	}

	if p.Name != "Test" || p.WrittenFor != 1 {
		t.Errorf("Name = %q, WrittenFor = %d", p.Name, p.WrittenFor)
	}
	m := p.Match
	if m.Type != control.DeviceGamepad || m.Vendor == nil || *m.Vendor != 0x054c ||
		m.Product == nil || *m.Product != 0x09cc || m.Bus != nil || m.Version != nil || m.Serial != "abc" {
		t.Errorf("Match = %+v", m)
	}

	wantDefaults := engine.Defaults{Octave: 1, Transpose: -2, Channel: 2, Velocity: 90, Layout: "Second"}
	if p.Engine.Defaults != wantDefaults {
		t.Errorf("Defaults = %+v, want %+v", p.Engine.Defaults, wantDefaults)
	}
	if p.Engine.Collision != engine.CollisionNoRepeat {
		t.Errorf("Collision = %v", p.Engine.Collision)
	}
	if len(p.Engine.Layouts) != 2 || p.Engine.Layouts[0].Name != "First" || p.Engine.Layouts[1].Name != "Second" {
		t.Fatalf("Layouts = %+v", p.Engine.Layouts)
	}

	shift, f1 := mustCode(t, "KEY_LEFTSHIFT"), mustCode(t, "KEY_F1")
	absX, absY := mustCode(t, "ABS_X"), mustCode(t, "ABS_Y")
	want := map[string]engine.Binding{
		"KEY_Z":     {Source: engine.Source{Code: mustCode(t, "KEY_Z")}, Action: engine.Note{Note: 36}},
		"KEY_X":     {Source: engine.Source{Code: mustCode(t, "KEY_X")}, Action: engine.Note{Note: 61}},
		"KEY_Q":     {Source: engine.Source{Code: mustCode(t, "KEY_Q")}, Action: engine.Note{Note: 63, ChannelOffset: 1, Velocity: 127}},
		"KEY_SPACE": {Source: engine.Source{Code: mustCode(t, "KEY_SPACE")}, Action: engine.ControlChange{Controller: 64}},
		"KEY_1":     {Source: engine.Source{Code: mustCode(t, "KEY_1")}, Action: engine.ProgramChange{Program: 5, ChannelOffset: 2}},
		"KEY_2":     {Source: engine.Source{Code: mustCode(t, "KEY_2")}, Action: engine.CommandPanic},
		"ABS_X":     {Source: engine.Source{Code: absX, Centered: true, Deadzone: 0.1, Invert: true}, Action: engine.PitchBend{}},
		"ABS_Y+":    {Source: engine.Source{Code: absY, Range: engine.RangeUpper, Deadzone: 0.25}, Action: engine.ControlChange{Controller: 1}},
		"ABS_Y-":    {Source: engine.Source{Code: absY, Range: engine.RangeLower}, Action: engine.PitchBend{Down: true}},
		"Touchpad":  {Source: engine.Source{Code: absX, Part: "Touchpad"}, Action: engine.ControlChange{Controller: 10}},
	}
	got := p.Engine.Layouts[0].Bindings
	if len(got) != len(want) {
		t.Errorf("layout has %d bindings, want %d", len(got), len(want))
	}
	for name, w := range want {
		found := false
		for _, g := range got {
			if sourceID(g.Source) == sourceID(w.Source) {
				found = true
				if g.Source.Centered != w.Source.Centered || g.Source.Invert != w.Source.Invert ||
					g.Source.Deadzone != w.Source.Deadzone || g.Action != w.Action {
					t.Errorf("%s = %+v, want %+v", name, g, w)
				}
			}
		}
		if !found {
			t.Errorf("binding %s is missing", name)
		}
	}

	if len(p.Engine.Global) != 2 {
		t.Fatalf("Global = %+v", p.Engine.Global)
	}
	for _, g := range p.Engine.Global {
		switch len(g.Source.Modifiers) {
		case 0:
			if g.Source.Code != f1 || g.Action != engine.CommandOctaveDown {
				t.Errorf("plain global binding = %+v", g)
			}
		case 1:
			if g.Source.Code != f1 || g.Source.Modifiers[0] != shift || g.Action != engine.CommandTransposeDown {
				t.Errorf("combination global binding = %+v", g)
			}
		}
	}

	if _, err := engine.New(p.Engine); err != nil {
		t.Errorf("engine rejects the parsed profile: %v", err)
	}
}

func TestParseMinimal(t *testing.T) {
	p, err := Parse([]byte("schema = 1\n[[layout]]\nname = \"Only\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := engine.Defaults{Velocity: DefaultVelocity}
	if p.Engine.Defaults != want || p.Engine.Collision != engine.CollisionInterrupt {
		t.Errorf("defaults = %+v, collision = %v", p.Engine.Defaults, p.Engine.Collision)
	}
	if p.Match != (Match{}) {
		t.Errorf("Match = %+v, want the zero value", p.Match)
	}
}

// Parsing must not depend on map iteration order.
func TestParseIsDeterministic(t *testing.T) {
	first, err := Parse([]byte(fullProfile))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		p, err := Parse([]byte(fullProfile))
		if err != nil {
			t.Fatal(err)
		}
		for j, b := range p.Engine.Layouts[0].Bindings {
			if sourceID(b.Source) != sourceID(first.Engine.Layouts[0].Bindings[j].Source) {
				t.Fatalf("binding order differs between runs")
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	layout := func(bindings string) string {
		return "schema = 1\n[[layout]]\nname = \"L\"\n[layout.bindings]\n" + bindings
	}
	tests := []struct {
		name, file, want string
	}{
		{"syntax", "schema = ", "line 1"},
		{"no schema", "name = \"x\"", `missing "schema"`},
		{"schema type", "schema = \"1\"", "whole number"},
		{"schema zero", "schema = 0", "first schema version is 1"},
		{"schema newer", "schema = 99", "update HIDI"},
		{"unknown top-level", "schema = 1\nbogus = 1\n[[layout]]\nname = \"L\"", `unknown setting "bogus"`},
		{"unknown default", "schema = 1\n[defaults]\nsemitone = 1\n[[layout]]\nname = \"L\"", `unknown setting "semitone"`},
		{"no layouts", "schema = 1", "no layouts"},
		{"layout not list", "schema = 1\n[layout]\nname = \"L\"", "[[layout]]"},
		{"match type", "schema = 1\n[match]\ntype = \"toaster\"\n[[layout]]\nname = \"L\"", "toaster"},
		{"match id range", "schema = 1\n[match]\nvendor = 70000\n[[layout]]\nname = \"L\"", "vendor is 70000"},
		{"channel range", "schema = 1\n[defaults]\nchannel = 0\n[[layout]]\nname = \"L\"", "channel is 0; it must be from 1 to 16"},
		{"velocity type", "schema = 1\n[defaults]\nvelocity = \"loud\"\n[[layout]]\nname = \"L\"", "velocity must be a whole number"},
		{"collision", "schema = 1\n[defaults]\ncollision = \"explode\"\n[[layout]]\nname = \"L\"", "explode"},
		{"axis mode", "schema = 1\n[defaults]\naxis_on_change = \"drop\"\n[[layout]]\nname = \"L\"", "use keep or rest"},
		{"axis rate", "schema = 1\n[defaults]\naxis_rate = 5000\n[[layout]]\nname = \"L\"", "axis_rate"},
		{"default layout", "schema = 1\n[defaults]\nlayout = \"Nope\"\n[[layout]]\nname = \"L\"", `default layout "Nope"`},
		{"unknown control", layout(`KEY_NOPE = "C1"`), `layout "L", KEY_NOPE: unknown control`},
		{"bad note", layout(`KEY_A = "H9"`), "neither a command nor a note"},
		{"note number range", layout(`KEY_A = 200`), "outside 0-127"},
		{"wrong value type", layout(`KEY_A = true`), "must be a note, a command or a table"},
		{"no action", layout(`KEY_A = { deadzone = 0.1 }`), "exactly one of"},
		{"two actions", layout(`KEY_A = { note = "C1", cc = 1 }`), "exactly one of"},
		{"unknown option", layout(`KEY_A = { cc = 1, wobble = true }`), `unknown setting "wobble"`},
		{"velocity on cc", layout(`KEY_A = { cc = 1, velocity = 5 }`), `unknown setting "velocity"`},
		{"cc range", layout(`KEY_A = { cc = 128 }`), "cc is 128"},
		{"pitch bend value", layout(`KEY_A = { pitch_bend = "sideways" }`), "pitch_bend must be"},
		{"bad command", layout(`KEY_A = { command = "dance" }`), `unknown command "dance"`},
		{"dead zone", layout(`ABS_X = { cc = 1, deadzone = 1.5 }`), "dead zone"},
		{"key half", layout(`"KEY_A+" = "C1"`), "no upper or lower half"},
		{"axis modifier", layout(`"ABS_X+KEY_A" = "C1"`), "cannot be held"},
		{"empty part", layout(`":KEY_A" = "C1"`), "part name"},
		{"duplicate alias", layout("BTN_A = \"C1\"\nBTN_SOUTH = \"D1\""), "same control"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.file))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v\nwant it to mention %q", err, tt.want)
			}
		})
	}
}

// All problems are reported together.
func TestParseReportsAllProblems(t *testing.T) {
	_, err := Parse([]byte("schema = 1\n[defaults]\nchannel = 20\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = \"H9\"\nKEY_NOPE = 1\n"))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"channel is 20", "KEY_A", "KEY_NOPE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestMatchScore(t *testing.T) {
	id := func(v uint16) *uint16 { return &v }
	ps4 := control.DeviceInfo{
		Hardware: control.HardwareID{Bus: 3, Vendor: 0x054c, Product: 0x09cc, Version: 0x8111},
		Serial:   "aa:bb", Type: control.DeviceGamepad,
	}
	tests := []struct {
		name  string
		match Match
		score int
		ok    bool
	}{
		{"anything", Match{}, 0, true},
		{"type", Match{Type: control.DeviceGamepad}, 1, true},
		{"wrong type", Match{Type: control.DeviceKeyboard}, 0, false},
		{"model", Match{Type: control.DeviceGamepad, Vendor: id(0x054c), Product: id(0x09cc)}, 3, true},
		{"wrong product", Match{Vendor: id(0x054c), Product: id(1)}, 0, false},
		{"everything", Match{Type: control.DeviceGamepad, Bus: id(3), Vendor: id(0x054c), Product: id(0x09cc), Version: id(0x8111), Serial: "aa:bb"}, 6, true},
		{"wrong serial", Match{Serial: "other"}, 0, false},
		{"zero is a value", Match{Bus: id(0)}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, ok := tt.match.Score(ps4)
			if score != tt.score || ok != tt.ok {
				t.Errorf("Score = %d, %v; want %d, %v", score, ok, tt.score, tt.ok)
			}
		})
	}
}

func TestMigrate(t *testing.T) {
	steps := map[int]migration{
		1: func(doc map[string]any) error {
			doc["renamed"] = doc["old"]
			delete(doc, "old")
			return nil
		},
		2: func(doc map[string]any) error {
			doc["renamed"] = doc["renamed"].(string) + "!"
			return nil
		},
	}

	doc := map[string]any{"schema": int64(1), "old": "value"}
	from, err := migrate(doc, steps, 3)
	if err != nil || from != 1 {
		t.Fatalf("migrate = %d, %v", from, err)
	}
	if doc["renamed"] != "value!" || doc["schema"] != int64(3) {
		t.Errorf("document after migration = %v", doc)
	}
	if _, stale := doc["old"]; stale {
		t.Error("old key survived the migration")
	}

	// A file already at the current version is left alone.
	doc = map[string]any{"schema": int64(3), "renamed": "kept"}
	if from, err := migrate(doc, steps, 3); err != nil || from != 3 || doc["renamed"] != "kept" {
		t.Errorf("current-version file: from = %d, err = %v, doc = %v", from, err, doc)
	}

	// A gap in the table is reported rather than skipped.
	if _, err := migrate(map[string]any{"schema": int64(1)}, map[int]migration{}, 2); err == nil {
		t.Error("migrate with a missing step succeeded")
	}
}

// The migrations table must cover every older schema version.
func TestMigrationsAreComplete(t *testing.T) {
	for v := 1; v < Schema; v++ {
		if migrations[v] == nil {
			t.Errorf("no migration from schema %d", v)
		}
	}
	if len(migrations) != Schema-1 {
		t.Errorf("migrations has %d entries, want %d", len(migrations), Schema-1)
	}
}

func TestBuiltin(t *testing.T) {
	profiles, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin:\n%v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("no built-in profiles")
	}

	names := map[string]bool{}
	var keyboard, gamepad bool
	for _, p := range profiles {
		if p.Name == "" || names[p.Name] {
			t.Errorf("profile name %q is empty or repeated", p.Name)
		}
		names[p.Name] = true
		if p.WrittenFor != Schema {
			t.Errorf("%s is written for schema %d, want the current schema %d", p.Name, p.WrittenFor, Schema)
		}
		if _, err := engine.New(p.Engine); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
		// A profile with only a type is the fallback for that class.
		if p.Match == (Match{Type: control.DeviceKeyboard}) {
			keyboard = true
		}
		if p.Match == (Match{Type: control.DeviceGamepad}) {
			gamepad = true
		}
	}
	if !keyboard || !gamepad {
		t.Errorf("fallback profiles: keyboard = %v, gamepad = %v; want both", keyboard, gamepad)
	}
}

// The default keyboard must behave as an instrument out of the box.
func TestBuiltinKeyboardPlays(t *testing.T) {
	profiles, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.Match != (Match{Type: control.DeviceKeyboard}) {
			continue
		}
		e, err := engine.New(p.Engine)
		if err != nil {
			t.Fatal(err)
		}
		out := e.Handle(control.Event{Code: mustCode(t, "KEY_Z"), Value: 1}, nil)
		if len(out) != 1 || out[0].String() != "NoteOn ch=1 note=C0 velocity=64" {
			t.Errorf("KEY_Z produced %v", out)
		}
		e.Handle(control.Event{Code: mustCode(t, "KEY_F2"), Value: 1}, nil)
		if got := e.State().Octave; got != 1 {
			t.Errorf("octave after KEY_F2 = %d, want 1", got)
		}
	}
}

func TestSelect(t *testing.T) {
	id := func(v uint16) *uint16 { return &v }
	general := &Profile{Name: "general", Match: Match{Type: control.DeviceGamepad}}
	model := &Profile{Name: "model", Match: Match{Type: control.DeviceGamepad, Vendor: id(1), Product: id(2)}}
	user := &Profile{Name: "user", Match: Match{Type: control.DeviceGamepad}}
	keyboard := &Profile{Name: "keyboard", Match: Match{Type: control.DeviceKeyboard}}

	pad := control.DeviceInfo{Type: control.DeviceGamepad, Hardware: control.HardwareID{Vendor: 1, Product: 2}}
	otherPad := control.DeviceInfo{Type: control.DeviceGamepad}
	mouse := control.DeviceInfo{Type: control.DeviceMouse}

	tests := []struct {
		name     string
		profiles []*Profile
		device   control.DeviceInfo
		want     *Profile
	}{
		{"most specific wins", []*Profile{model, general, keyboard}, pad, model},
		{"general for another model", []*Profile{model, general, keyboard}, otherPad, general},
		{"later wins a tie", []*Profile{general, user}, otherPad, user},
		{"specific beats a later general one", []*Profile{model, user}, pad, model},
		{"nothing selects it", []*Profile{general, keyboard}, mouse, nil},
		{"no profiles", nil, pad, nil},
	}
	for _, tt := range tests {
		if got := Select(tt.profiles, tt.device); got != tt.want {
			t.Errorf("%s: Select = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAxisSettings(t *testing.T) {
	parse := func(defaults string) *Profile {
		t.Helper()
		p, err := Parse([]byte("schema = 1\n[defaults]\n" + defaults + "\n[[layout]]\nname = \"L\"\n"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := parse(""); p.Engine.RestAxes || p.AxisRate != DefaultAxisRate {
		t.Errorf("defaults: RestAxes %v, AxisRate %d", p.Engine.RestAxes, p.AxisRate)
	}
	if p := parse("axis_on_change = \"rest\"\naxis_rate = 0"); !p.Engine.RestAxes || p.AxisRate != 0 {
		t.Errorf("rest and unlimited: RestAxes %v, AxisRate %d", p.Engine.RestAxes, p.AxisRate)
	}
	if p := parse("axis_on_change = \"keep\"\naxis_rate = 1000"); p.Engine.RestAxes || p.AxisRate != 1000 {
		t.Errorf("keep and 1000: RestAxes %v, AxisRate %d", p.Engine.RestAxes, p.AxisRate)
	}
}

func TestTouchOption(t *testing.T) {
	p, err := Parse([]byte("schema = 1\nname = \"T\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nABS_RZ = { note = \"C1\", touch = true }\nABS_Z = { note = \"D1\" }\n"))
	if err != nil {
		t.Fatal(err)
	}
	touch := map[bool]int{}
	for _, b := range p.Engine.Layouts[0].Bindings {
		touch[b.Action.(engine.Note).Touch]++
	}
	if touch[true] != 1 || touch[false] != 1 {
		t.Errorf("touch was read as %v", touch)
	}
	if _, err := Parse([]byte("schema = 1\nname = \"T\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nABS_RZ = { cc = 1, touch = true }\n")); err == nil {
		t.Error("touch on a controller was accepted")
	}
}
