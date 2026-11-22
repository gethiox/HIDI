package gui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/settings"
)

// factoryProfile writes a built-in profile the way HIDI keeps it: in a
// directory of its own, not to be written.
func factoryProfile(t *testing.T) (file, userDir string, p settings.Paths) {
	t.Helper()
	root := t.TempDir()
	p = settings.Paths{Root: root, Factory: filepath.Join(root, "profiles", "factory"), User: filepath.Join(root, "profiles", "user")}
	if err := os.MkdirAll(p.Factory, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, _, err := config.WriteBuiltin(dir); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "keyboard.toml"))
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(p.Factory, "keyboard.toml")
	if err := os.WriteFile(file, text, 0o444); err != nil {
		t.Fatal(err)
	}
	return file, p.User, p
}

func TestFactoryProfileIsNotChanged(t *testing.T) {
	file, userDir, paths := factoryProfile(t)
	if !IsFactory(file, paths) || IsFactory(filepath.Join(userDir, "keyboard.toml"), paths) {
		t.Fatal("the kind of a profile file is not told by its directory")
	}
	doc, err := OpenProfile(file)
	if err != nil {
		t.Fatal(err)
	}
	doc.ReadOnly = true
	layout := doc.DefaultLayout()
	if err := doc.SetNote(layout, control.Key(44), midi.Note(61)); !errors.Is(err, ErrFactory) {
		t.Errorf("a change to a factory profile: %v", err)
	}
	if err := doc.AddLayout("More"); !errors.Is(err, ErrFactory) {
		t.Errorf("a layout added to a factory profile: %v", err)
	}
	if doc.Dirty() || doc.CanUndo() {
		t.Error("the refused changes left something behind")
	}

	// A copy of the user's own can be changed, and is made once.
	own, made, err := Customise(file, userDir)
	if err != nil || !made || own != filepath.Join(userDir, "keyboard.toml") {
		t.Fatalf("Customise: %q, %v, %v", own, made, err)
	}
	if info, err := os.Stat(own); err != nil || info.Mode().Perm()&0o200 == 0 {
		t.Errorf("the copy cannot be written: %v, %v", info, err)
	}
	copyDoc, err := OpenProfile(own)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyDoc.SetNote(copyDoc.DefaultLayout(), control.Key(44), midi.Note(61)); err != nil {
		t.Fatal(err)
	}
	if err := copyDoc.Save(); err != nil {
		t.Fatal(err)
	}
	changed, _ := os.ReadFile(own)
	if again, made, err := Customise(file, userDir); err != nil || made || again != own {
		t.Errorf("Customise again: %q, %v, %v", again, made, err)
	}
	if now, _ := os.ReadFile(own); string(now) != string(changed) {
		t.Error("customising again wrote over the user's profile")
	}

	// Turned off and on in the file.
	for _, on := range []bool{false, true} {
		if err := SetProfileEnabled(own, on); err != nil {
			t.Fatal(err)
		}
		text, _ := os.ReadFile(own)
		p, err := config.Parse(text)
		if err != nil || p.Enabled != on {
			t.Errorf("enabled set to %v: %v, %v", on, p.Enabled, err)
		}
	}
}

func TestPendingSettings(t *testing.T) {
	var p pending
	disk := settings.Default()
	p.load(disk)
	if p.Dirty() {
		t.Fatal("settings as the file has them count as changed")
	}
	p.shown.Input.GrabGamepads = true
	p.shown.Devices.List = append(p.shown.Devices.List, "Some Keyboard")
	if !p.Dirty() || p.restart() {
		t.Errorf("after a change: dirty %v, restart %v", p.Dirty(), p.restart())
	}
	if len(p.disk.Devices.List) != 0 {
		t.Error("a change reached what the file has")
	}
	// The file changes elsewhere: what is not saved stays, and what the
	// window keeps for itself follows.
	disk.Shown.Input = []string{"Some Keyboard"}
	disk.Input.StartOn = false
	p.load(disk)
	if !p.shown.Input.GrabGamepads || len(p.shown.Shown.Input) != 1 {
		t.Errorf("after the file changed: %+v", p.shown)
	}
	if !p.restart() {
		t.Error("starting on or off is said to be followed at once")
	}
	p.drop()
	if p.Dirty() || p.shown.Input.GrabGamepads || p.shown.Input.StartOn {
		t.Error("dropping left changes")
	}
	// Changed and changed back is not changed.
	p.shown.Devices.List = append(p.shown.Devices.List, "x")
	p.shown.Devices.List = p.shown.Devices.List[:0]
	if p.Dirty() {
		t.Error("an emptied list counts as a change")
	}
}

func TestDeviceRows(t *testing.T) {
	rows := deviceRows([]seenDevice{
		{"Old Pad", "gamepad", false},
		{"Keyboard", "keyboard", true},
		{"Keyboard", "keyboard", false},
		{"Stick", "joystick", true},
	}, []string{"Gone Forever", "Stick"})
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	want := []string{"Keyboard", "Stick", "Old Pad", "Gone Forever"}
	if len(names) != len(want) {
		t.Fatalf("rows: %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("rows: %v, want %v", names, want)
		}
	}
	if !rows[0].There || rows[2].There || rows[2].About != "gamepad, not connected" || rows[3].About != "not connected" {
		t.Errorf("rows: %+v", rows)
	}
}

func TestProfileLine(t *testing.T) {
	profiles, err := config.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.Name == "Default keyboard" {
			p.Source = "/somewhere/profiles/factory/keyboard.toml"
			if got := profileLine(p); got != "keyboard.toml · for type=keyboard" {
				t.Errorf("profileLine: %q", got)
			}
			return
		}
	}
	t.Fatal("no default keyboard profile")
}
