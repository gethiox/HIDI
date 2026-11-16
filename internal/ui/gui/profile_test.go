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
