package engine

import (
	"strings"
	"testing"
)

func TestNewRejectsInvalidProfiles(t *testing.T) {
	valid := func() Profile { return baseProfile(key(keyA, Note{Note: 60})) }
	binding := func(b Binding) func(*Profile) {
		return func(p *Profile) { p.Layouts[0].Bindings = []Binding{b} }
	}

	tests := []struct {
		name    string
		mutate  func(*Profile)
		wantErr string
	}{
		{"no layouts", func(p *Profile) { p.Layouts = nil }, "no layouts"},
		{"unnamed layout", func(p *Profile) { p.Layouts[0].Name = "" }, "has no name"},
		{"duplicate layout", func(p *Profile) { p.Layouts = append(p.Layouts, p.Layouts[0]) }, "more than once"},
		{"missing default layout", func(p *Profile) { p.Defaults.Layout = "nope" }, `default layout "nope"`},
		{"octave", func(p *Profile) { p.Defaults.Octave = MaxOctave + 1 }, "default octave"},
		{"transpose", func(p *Profile) { p.Defaults.Transpose = -MaxTranspose - 1 }, "default transpose"},
		{"channel", func(p *Profile) { p.Defaults.Channel = 16 }, "default channel"},
		{"velocity zero", func(p *Profile) { p.Defaults.Velocity = 0 }, "default velocity"},
		{"velocity high", func(p *Profile) { p.Defaults.Velocity = 128 }, "default velocity"},
		{"collision", func(p *Profile) { p.Collision = 99 }, "collision mode"},
		{"no control", binding(Binding{Action: Note{}}), "no control"},
		{"no action", binding(Binding{Source: Source{Code: keyA}}), "no action"},
		{"range", binding(Binding{Source: Source{Code: keyA, Range: 9}, Action: Note{}}), "invalid range"},
		{"dead zone", binding(Binding{Source: Source{Code: keyA, Deadzone: 1}, Action: Note{}}), "dead zone"},
		{"centered half", binding(Binding{Source: Source{Code: stickX, Range: RangeUpper, Centered: true}, Action: Note{}}), "centered"},
		{"axis modifier", binding(key(keyA, Note{}, stickX)), "not a key"},
		{"own modifier", binding(key(keyA, Note{}, keyA)), "own modifier"},
		{"note", binding(key(keyA, Note{Note: 128})), "out of range"},
		{"note velocity", binding(key(keyA, Note{Velocity: 128})), "out of range"},
		{"controller", binding(key(keyA, ControlChange{Controller: 128})), "out of range"},
		{"bend offset", binding(key(keyA, PitchBend{ChannelOffset: 16})), "out of range"},
		{"program", binding(key(keyA, ProgramChange{Program: 128})), "out of range"},
		{"command", binding(key(keyA, Command(0))), "invalid command"},
		{"global binding", func(p *Profile) { p.Global = []Binding{{Action: Note{}}} }, "global binding 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := valid()
			tt.mutate(&p)
			_, err := New(p)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}

	if _, err := New(valid()); err != nil {
		t.Errorf("New rejected a valid profile: %v", err)
	}
}

// Every problem is reported at once, so a user fixes a file in one pass.
func TestNewReportsAllProblems(t *testing.T) {
	p := baseProfile(key(keyA, Note{Note: 128}))
	p.Defaults.Channel = 99
	p.Defaults.Velocity = 0
	_, err := New(p)
	for _, want := range []string{"default channel", "default velocity", "out of range"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not mention %q", err, want)
		}
	}
}

func TestDefaultLayout(t *testing.T) {
	p := baseProfile()
	p.Layouts = append(p.Layouts, Layout{Name: "second"})
	p.Defaults.Layout = "second"
	e, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.State().Layout; got != "second" {
		t.Errorf("initial layout = %q, want %q", got, "second")
	}
}

func TestNames(t *testing.T) {
	for c := Command(1); c < commandCount; c++ {
		got, err := ParseCommand(c.String())
		if err != nil || got != c {
			t.Errorf("ParseCommand(%q) = %v, %v", c.String(), got, err)
		}
	}
	if _, err := ParseCommand("nope"); err == nil {
		t.Error("ParseCommand accepted an unknown name")
	}
	for m := CollisionInterrupt; m <= CollisionOff; m++ {
		got, err := ParseCollisionMode(m.String())
		if err != nil || got != m {
			t.Errorf("ParseCollisionMode(%q) = %v, %v", m.String(), got, err)
		}
	}
	if _, err := ParseCollisionMode("nope"); err == nil {
		t.Error("ParseCollisionMode accepted an unknown name")
	}
}
