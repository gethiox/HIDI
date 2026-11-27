package gui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/light"
)

func TestKeyColour(t *testing.T) {
	c := defaultCaps
	// A key that does nothing is barely red, as the keyboard's lights
	// show it: more red than the panel it is set into, and dark.
	if n := KeyColour("", 0, 0, 0, c); n != c.nothing || n.R <= colBar.R || n.R <= n.G || n.R > 0x30 {
		t.Errorf("a key that does nothing: %v", n)
	}
	// C3 is note 60: a C, and a C in every octave.
	if KeyColour("note", 60, 0, 0, c) != colCNote || KeyColour("note", 60, -5, 0, c) != colCNote {
		t.Error("a C is not shown as one")
	}
	// Transposed by one it is a black key, by two a white one, and a note
	// below zero still has a place in the octave.
	if KeyColour("note", 60, 0, 1, c) != colBlackNote || KeyColour("note", 60, 0, 2, c) != colWhiteNote {
		t.Error("the transposition does not show")
	}
	if KeyColour("note", 0, -1, 1, c) != colBlackNote || KeyColour("note", 0, -1, 0, c) != colCNote {
		t.Error("a note below zero has the wrong colour")
	}
	seen := map[RGB]string{}
	for _, role := range []string{"command", "passthrough", "controller"} {
		k := KeyColour(role, 0, 0, 0, c)
		if other, dup := seen[k]; dup || k == c.nothing {
			t.Errorf("%s has the colour of %q", role, other)
		}
		seen[k] = role
	}
	if KeyColour("bend", 0, 0, 0, c) != KeyColour("program", 0, 0, 0, c) {
		t.Error("the roles that send something else differ in colour")
	}
}

// The caps follow the colours of the lights: the hue of each, at one
// strength, whatever its brightness; a light that is black leaves the cap
// as the ground of the page.
func TestCapsOfLights(t *testing.T) {
	colours := light.DefaultColours()
	c := capsOf(colours)
	// The ground of a cap is not quite grey: channels within a few steps
	// count as alike.
	near := func(a, b uint8) bool { return a-b < 5 || b-a < 5 }
	// Commands are magenta: red and blue alike, far more than green.
	if !near(c.command.R, c.command.B) || c.command.G+50 > c.command.R {
		t.Errorf("command: %v", c.command)
	}
	// Left to the system is grey, quieter than a command, its light
	// being dim, and still more than a key that does nothing.
	if !near(c.pass.R, c.pass.G) || !near(c.pass.G, c.pass.B) || c.pass.R >= c.command.R || c.pass.G <= c.nothing.G+10 {
		t.Errorf("passthrough: %v, command %v", c.pass, c.command)
	}
	colours.Command = light.RGB{G: 10}
	colours.Passthrough = light.RGB{}
	c = capsOf(colours)
	if c.command.G <= c.command.R+20 || c.command.G <= c.command.B+20 {
		t.Errorf("a changed command colour: %v", c.command)
	}
	if c.pass != colBar {
		t.Errorf("a black light: %v", c.pass)
	}
	if RoleColour(Role{Kind: RoleCommand}, c) != c.command || RoleColour(Role{Kind: RoleBend}, c) != c.other || RoleColour(Role{}, c) != c.nothing {
		t.Error("the colours of the roles on the profile page")
	}
}

func TestParseColourAndGlow(t *testing.T) {
	if got := ParseColour("#0a1BfF"); got != (RGB{0x0a, 0x1b, 0xff}) {
		t.Errorf("ParseColour = %v", got)
	}
	for _, bad := range []string{"", "0a1bff", "#0a1bf", "#0a1bfg", "#0a1bff0"} {
		if got := ParseColour(bad); got != (RGB{}) {
			t.Errorf("ParseColour(%q) = %v, want black", bad, got)
		}
	}
	// A light that is off stays dark, one at full strength stays full,
	// and one in between is drawn brighter than its number.
	if Glow(RGB{}) != (RGB{}) || Glow(RGB{255, 255, 255}) != (RGB{255, 255, 255}) {
		t.Error("the ends of the range moved")
	}
	if g := Glow(RGB{0x55, 0, 0}); g.R <= 0x55 || g.R >= 0xc0 || g.G != 0 {
		t.Errorf("Glow of a third = %v", g)
	}
}

func TestFlip(t *testing.T) {
	list := []string{"a", "b"}
	if got := flip(list, "c"); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("adding: %v", got)
	}
	if got := flip(list, "a"); !slices.Equal(got, []string{"b"}) {
		t.Errorf("removing: %v", got)
	}
	if !slices.Equal(list, []string{"a", "b"}) {
		t.Errorf("the list itself was changed: %v", list)
	}
}

// A key is left to the system from the window, and taken back.
func TestProfileDocPassthrough(t *testing.T) {
	files, err := config.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keyboard.toml")
	if err := os.WriteFile(path, files["keyboard.toml"], 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.Layouts()[0]
	space, keyZ := control.Key(57), control.Key(44)
	for _, key := range []control.Code{space, keyZ} {
		if err := doc.SetPassthrough(layout, key); err != nil {
			t.Fatal(err)
		}
		if r := doc.Role(layout, key); r.Kind != RolePass {
			t.Errorf("%v after SetPassthrough: %+v", key, r)
		}
	}
	doc.Save()
	text, _ := os.ReadFile(path)
	if !strings.Contains(string(text), `KEY_SPACE = "passthrough"`) {
		t.Error("the file does not say that the space bar is passed through")
	}
	if _, err := config.Parse(text); err != nil {
		t.Errorf("the file cannot be read any more: %v", err)
	}
	if err := doc.SetNote(layout, keyZ, 60); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, keyZ); r.Kind != RoleNote {
		t.Errorf("Z after SetNote: %+v", r)
	}
	if err := doc.SetPassthrough(layout, control.Axis(0)); err == nil {
		t.Error("an axis was left to the system")
	}
}
