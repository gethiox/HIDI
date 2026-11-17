package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
)

func TestProfileDoc(t *testing.T) {
	files, err := config.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keyboard.toml")
	original := "# Changed by hand, and it has to stay.\n" + string(files["keyboard.toml"])
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.Layouts()[0]
	keyZ, keyF1, keyF24 := control.Key(44), control.Key(59), control.Key(194)

	// What the keys do now.
	if r := doc.Role(layout, keyZ); r.Kind != RoleNote || r.Section != config.Bindings(layout) {
		t.Errorf("Z: %+v", r)
	}
	if r := doc.Role(layout, keyF1); r.Kind != RoleCommand || r.Command != engine.CommandOctaveDown || r.Section != config.Global {
		t.Errorf("F1: %+v", r)
	}
	if r := doc.Role(layout, keyF24); r.Kind != RoleNone || r.Section != config.Bindings(layout) {
		t.Errorf("F24: %+v", r)
	}

	// Changes: a note moved, a new note, a command replaced, one cleared,
	// a default.
	was := doc.Role(layout, keyZ).Note
	if err := doc.SetNote(layout, keyZ, was+1); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetNote(layout, keyF24, 60); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetCommand(layout, keyF1, engine.CommandPanic); err != nil {
		t.Fatal(err)
	}
	if err := doc.Clear(layout, control.Key(60)); err != nil { // F2
		t.Fatal(err)
	}
	if err := doc.SetDefault("velocity", 99); err != nil {
		t.Fatal(err)
	}

	// The file has them, a fresh reading agrees, and the rest is as it was.
	again, err := OpenProfile(path)
	if err != nil {
		t.Fatalf("the changed file: %v", err)
	}
	if r := again.Role(layout, keyZ); r.Note != was+1 {
		t.Errorf("Z plays %v, want %v", r.Note, was+1)
	}
	if r := again.Role(layout, keyF24); r.Kind != RoleNote || r.Note != 60 {
		t.Errorf("F24: %+v", r)
	}
	if r := again.Role(layout, keyF1); r.Command != engine.CommandPanic || r.Section != config.Global {
		t.Errorf("F1: %+v", r)
	}
	if r := again.Role(layout, control.Key(60)); r.Kind != RoleNone {
		t.Errorf("F2 still does %+v", r)
	}
	if again.Profile.Engine.Defaults.Velocity != 99 {
		t.Errorf("velocity = %d", again.Profile.Engine.Defaults.Velocity)
	}
	text, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(text), "# Changed by hand, and it has to stay.\n") {
		t.Error("the note at the top of the file is gone")
	}
	before, after := strings.Count(original, "\n"), strings.Count(string(text), "\n")
	if after != before { // one line added, one removed
		t.Errorf("the file went from %d lines to %d", before, after)
	}
	// Windows has no such permissions to keep.
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("the file's permissions changed to %v", info.Mode().Perm())
	}

	// A change that cannot be made leaves the file alone.
	if err := doc.SetDefault("velocity", 500); err == nil {
		t.Error("a velocity of 500 was accepted")
	}
	if now, _ := os.ReadFile(path); string(now) != string(text) {
		t.Error("a refused change was written")
	}
}

// The axes of a game controller: what they send is read, changed and
// written as a person would write it.
func TestProfileDocAxes(t *testing.T) {
	files, err := config.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gamepad.toml")
	if err := os.WriteFile(path, files["gamepad.toml"], 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.Layouts()[0]
	absX, absY, absRX, absZ, absMisc := control.Axis(0), control.Axis(1), control.Axis(3), control.Axis(2), control.Axis(0x28)

	if r := doc.Role(layout, absX); r.Kind != RoleController || r.Controller != 0 || !r.Shape.Centered || r.Shape.Deadzone != 0.1 || r.Split {
		t.Errorf("ABS_X: %+v", r)
	}
	if r := doc.Role(layout, absY); r.Kind != RoleBend || !r.Shape.Invert {
		t.Errorf("ABS_Y: %+v", r)
	}
	// The halves of the right stick are bound apart.
	if r := doc.Role(layout, absRX); r.Kind != RoleNone || !r.Split {
		t.Errorf("ABS_RX: %+v", r)
	}
	if r := doc.Role(layout, absMisc); r.Kind != RoleNone || r.Split {
		t.Errorf("an unbound axis: %+v", r)
	}

	// A trigger becomes another controller, an unbound axis a pitch bend,
	// and the stick loses its dead zone.
	if err := doc.SetController(layout, absZ, 74, Shape{Invert: true}); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetBend(layout, absMisc, Shape{Centered: true, Deadzone: 0.05}); err != nil {
		t.Fatal(err)
	}
	shape := doc.Role(layout, absX).Shape
	shape.Deadzone = 0
	if err := doc.SetController(layout, absX, 0, shape); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetController(layout, absX, 128, shape); err == nil {
		t.Error("controller 128 was accepted")
	}

	again, err := OpenProfile(path)
	if err != nil {
		t.Fatalf("the changed file is not a profile any more: %v", err)
	}
	if r := again.Role(layout, absZ); r.Kind != RoleController || r.Controller != 74 || !r.Shape.Invert || r.Shape.Centered {
		t.Errorf("ABS_Z afterwards: %+v", r)
	}
	if r := again.Role(layout, absMisc); r.Kind != RoleBend || !r.Shape.Centered || r.Shape.Deadzone != 0.05 {
		t.Errorf("ABS_MISC afterwards: %+v", r)
	}
	if r := again.Role(layout, absX); r.Kind != RoleController || r.Shape.Deadzone != 0 || !r.Shape.Centered {
		t.Errorf("ABS_X afterwards: %+v", r)
	}
	text, _ := os.ReadFile(path)
	for _, want := range []string{"ABS_Z = { cc = 74, invert = true }", "ABS_MISC = { pitch_bend = true, centered = true, deadzone = 0.05 }", "ABS_X = { cc = 0, centered = true }"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the file does not contain %q", want)
		}
	}
	if err := again.Clear(layout, absZ); err != nil {
		t.Fatal(err)
	}
	if r := again.Role(layout, absZ); r.Kind != RoleNone {
		t.Errorf("ABS_Z after clearing: %+v", r)
	}
}
