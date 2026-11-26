package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/ipc"
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
	if !p.Dirty() {
		t.Error("a change does not count as one")
	}
	if len(p.disk.Devices.List) != 0 {
		t.Error("a change reached what the file has")
	}
	// The file changes elsewhere: what is not saved stays.
	disk.Input.StartOn = true
	p.load(disk)
	if !p.shown.Input.GrabGamepads || p.disk.Input.StartOn != true {
		t.Errorf("after the file changed: %+v", p.shown)
	}
	p.drop()
	if p.Dirty() || p.shown.Input.GrabGamepads || !p.shown.Input.StartOn {
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

func TestCarry(t *testing.T) {
	file, userDir, _ := factoryProfile(t)
	own, _, err := Customise(file, userDir)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(own)
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.DefaultLayout()
	keyZ, keyX, f24 := control.Key(44), control.Key(45), control.Key(194)
	was, other := doc.Role(layout, keyZ), doc.Role(layout, keyX)
	if was.Kind != RoleNote || other.Kind != RoleNote || was.Text == other.Text {
		t.Fatalf("the profile is not as expected: %+v, %+v", was, other)
	}

	// Moved: the target does what the source did, the source nothing.
	if err := doc.Carry(layout, keyZ, f24, true); err != nil {
		t.Fatal(err)
	}
	if got := doc.Role(layout, f24); got.Kind != RoleNote || got.Text != was.Text {
		t.Errorf("the target after a move: %+v", got)
	}
	if got := doc.Role(layout, keyZ); got.Kind != RoleNone {
		t.Errorf("the source after a move: %+v", got)
	}
	// One step back undoes both halves.
	if !doc.Undo() || doc.Role(layout, keyZ).Text != was.Text || doc.Role(layout, f24).Kind != RoleNone {
		t.Error("taking the move back did not put both controls back")
	}

	// Copied onto a control that does something already: both do the
	// same, and the target's old line is the one that was changed.
	if err := doc.Carry(layout, keyZ, keyX, false); err != nil {
		t.Fatal(err)
	}
	if a, b := doc.Role(layout, keyZ), doc.Role(layout, keyX); a.Text != was.Text || b.Text != was.Text {
		t.Errorf("after a copy: %+v, %+v", a, b)
	}
	if n := strings.Count(string(doc.Text()), "KEY_X"); n != strings.Count(string(doc.saved), "KEY_X") {
		t.Errorf("the copy added a line for the target: %d", n)
	}

	// Nothing to carry, and onto itself.
	before := string(doc.Text())
	if err := doc.Carry(layout, f24, keyZ, true); err == nil {
		t.Error("a control that does nothing was carried")
	}
	if err := doc.Carry(layout, keyZ, keyZ, true); err != nil || string(doc.Text()) != before {
		t.Errorf("a control carried onto itself: %v", err)
	}
	// The result is a profile.
	if _, err := config.Parse(doc.Text()); err != nil {
		t.Error(err)
	}
}

func TestCarriedAndPutBackIsNotAChange(t *testing.T) {
	file, userDir, _ := factoryProfile(t)
	own, _, err := Customise(file, userDir)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(own)
	if err != nil {
		t.Fatal(err)
	}
	layout := doc.DefaultLayout()
	keyZ, f24 := control.Key(44), control.Key(194)
	saved := string(doc.Text())

	// A binding carried away and back binds what it bound. Its line is
	// written elsewhere meanwhile; the profile is the saved one all the
	// same, and so is its text, to the letter.
	if err := doc.Carry(layout, keyZ, f24, true); err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty() {
		t.Fatal("a carried binding is no change")
	}
	if err := doc.Carry(layout, f24, keyZ, true); err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() || string(doc.Text()) != saved {
		t.Errorf("carried away and back, the profile still waits to be saved")
	}
	// The same by other means: a note changed and changed back.
	was := doc.Role(layout, keyZ)
	if err := doc.SetNote(layout, keyZ, midi.Note(100)); err != nil || !doc.Dirty() {
		t.Fatalf("a changed note: %v, dirty %v", err, doc.Dirty())
	}
	if err := doc.SetNote(layout, keyZ, midi.Note(was.Note)); err != nil || doc.Dirty() {
		t.Errorf("a note changed back: %v, dirty %v", err, doc.Dirty())
	}

	// Revert: the saved profile, and nothing to take back.
	doc.Carry(layout, keyZ, f24, true)
	doc.AddLayout("More")
	doc.Revert()
	if doc.Dirty() || doc.CanUndo() || doc.CanRedo() || string(doc.Text()) != saved {
		t.Errorf("after Revert: dirty %v, undo %v, redo %v", doc.Dirty(), doc.CanUndo(), doc.CanRedo())
	}
	// After saving, the saved profile is the new one.
	doc.Carry(layout, keyZ, f24, true)
	if err := doc.Save(); err != nil || doc.Dirty() {
		t.Fatalf("Save: %v, dirty %v", err, doc.Dirty())
	}
	doc.Carry(layout, f24, keyZ, true)
	if !doc.Dirty() {
		t.Error("back to what was saved before the last saving counts as saved")
	}
}

func TestCustomiseFor(t *testing.T) {
	files, err := config.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Join(t.TempDir(), "profiles", "user")
	stick := ipc.Instrument{Name: "Thrustmaster T.16000M", Type: "gamepad", Hardware: "0003:044f:b10a:0111"}
	own, made, err := CustomiseFor(files["gamepad.toml"], userDir, stick)
	if err != nil || !made || own != filepath.Join(userDir, "thrustmaster-t-16000m-044f-b10a.toml") {
		t.Fatalf("CustomiseFor: %q, %v, %v", own, made, err)
	}
	doc, err := OpenProfile(own)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Profile.Name != stick.Name || !OwnProfile(doc.Profile) || !doc.Profile.Generate.Any() {
		t.Errorf("the profile for the device: %q, for %s, rule %+v", doc.Profile.Name, doc.Profile.Match, doc.Profile.Generate)
	}
	// It can be changed, and asking again gives the same file as it is.
	if err := doc.SetNote(doc.DefaultLayout(), control.Key(0x130), midi.Note(60)); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	changed, _ := os.ReadFile(own)
	if again, made, err := CustomiseFor(files["gamepad.toml"], userDir, stick); err != nil || made || again != own {
		t.Errorf("CustomiseFor again: %q, %v, %v", again, made, err)
	}
	if now, _ := os.ReadFile(own); string(now) != string(changed) {
		t.Error("asking again wrote over the profile")
	}
	// A device that does not say what it is gets none.
	if _, _, err := CustomiseFor(files["gamepad.toml"], userDir, ipc.Instrument{Name: "Pad", Type: "gamepad", Hardware: "0000:0000:0000:0000"}); err == nil {
		t.Error("a profile was made for a device without vendor and product")
	}
	if _, _, err := CustomiseFor(files["gamepad.toml"], userDir, ipc.Instrument{Name: "Pad", Type: "gamepad"}); err == nil {
		t.Error("a profile was made for a device without a hardware ID")
	}
	general, _ := config.Parse(files["gamepad.toml"])
	if OwnProfile(general) {
		t.Error("the general profile counts as one for a product")
	}
}

// A profile of the user's that is for a kind of device, as a factory
// profile is, stands in for that one; a profile for one product does not.
func TestOverrides(t *testing.T) {
	profiles, err := config.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	var pad *config.Profile
	for _, p := range profiles {
		p.Kind = config.KindFactory
		if p.Match.Type == control.DeviceGamepad {
			pad = p
		}
	}
	copyOf := *pad
	copyOf.Kind = config.KindUser
	files, _ := config.BuiltinFiles()
	text, err := config.ForDevice(files["gamepad.toml"], "Stick", control.DeviceGamepad, control.HardwareID{Vendor: 0x231d, Product: 0x0200})
	if err != nil {
		t.Fatal(err)
	}
	stick, err := config.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	stick.Kind = config.KindUser
	all := append(profiles, &copyOf, stick)
	if !Overrides(&copyOf, all) {
		t.Error("a copy of the factory profile does not count as standing in for it")
	}
	if Overrides(stick, all) || Overrides(pad, all) {
		t.Error("a profile for one product, or a factory profile, counts as an override")
	}
	mouse, err := config.Parse([]byte("schema = 1\nname = \"M\"\n[match]\ntype = \"mouse\"\n[[layout]]\nname = \"L\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	mouse.Kind = config.KindUser
	if Overrides(mouse, append(all, mouse)) {
		t.Error("a profile for a kind HIDI has no factory profile for counts as an override")
	}
}
