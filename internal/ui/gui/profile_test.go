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
	"github.com/gethiox/HIDI/internal/midi"
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
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
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
	doc.Save()
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

// padProfile is a profile that names the axes of a pad, the halves of the
// right stick apart.
const padProfile = `schema = 1
name = "Pad"

[match]
type = "gamepad"

[[layout]]
name = "Default"

[layout.bindings]
BTN_A = 0
ABS_X = { cc = 0, centered = true, deadzone = 0.1 }
ABS_Y = { pitch_bend = true, centered = true, invert = true, deadzone = 0.1 }
"ABS_RX+" = { cc = 1, deadzone = 0.1 }
"ABS_RX-" = { cc = 2, deadzone = 0.1 }
ABS_Z = { cc = 5 }
`

// The axes of a game controller: what they send is read, changed and
// written as a person would write it.
func TestProfileDocAxes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pad.toml")
	if err := os.WriteFile(path, []byte(padProfile), 0o600); err != nil {
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

	if err := doc.Save(); err != nil {
		t.Fatal(err)
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
	doc.Save()
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

const optionsProfile = `schema = 1
name = "Options"
[match]
type = "gamepad"
[defaults]
layout = "L"
[[layout]]
name = "L"
[layout.bindings]
KEY_A = { note = "C3", velocity = 90, channel_offset = 2 }
BTN_A = "D3"
ABS_X = { cc = 1, channel_offset = 3, centered = true }
ABS_Y = { pitch_bend = "down", deadzone = 0.1 }
`

func openText(t *testing.T, text string) *ProfileDoc {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// What a line says that the window does not show stays when the window
// writes the line anew.
func TestProfileDocKeepsOptions(t *testing.T) {
	doc := openText(t, optionsProfile)
	keyA, absX, absY := control.Key(30), control.Axis(0), control.Axis(1)

	if err := doc.SetNote("L", keyA, 61); err != nil {
		t.Fatal(err)
	}
	x := doc.Role("L", absX)
	if err := doc.SetController("L", absX, 2, x.Shape); err != nil {
		t.Fatal(err)
	}
	y := doc.Role("L", absY)
	y.Shape.Invert = true
	if err := doc.SetBend("L", absY, y.Shape); err != nil {
		t.Fatal(err)
	}
	doc.Save()
	text, _ := os.ReadFile(doc.Path)
	for _, want := range []string{
		`KEY_A = { note = "C#3", velocity = 90, channel_offset = 2 }`,
		`ABS_X = { cc = 2, channel_offset = 3, centered = true }`,
		`ABS_Y = { pitch_bend = "down", invert = true, deadzone = 0.1 }`,
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the file does not contain %s\n%s", want, text)
		}
	}
}

// A control the file names by another spelling is changed and cleared in
// its own line.
func TestProfileDocOtherSpelling(t *testing.T) {
	doc := openText(t, optionsProfile)
	south := control.Key(0x130) // BTN_SOUTH, which the file calls BTN_A
	if err := doc.SetNote("L", south, 64); err != nil {
		t.Fatal(err)
	}
	if text := doc.Text(); !strings.Contains(string(text), `BTN_A = "E3"`) {
		t.Errorf("the line of BTN_A was not changed:\n%s", text)
	}
	if err := doc.Clear("L", south); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role("L", south); r.Kind != RoleNone {
		t.Errorf("still bound after clearing: %+v", r)
	}
}

// A change made to the file in an editor while the page is open and has
// nothing unsaved is kept by the next change made in the window, and
// shown.
func TestProfileDocKeepsHandEdits(t *testing.T) {
	doc := openText(t, optionsProfile)
	byHand := optionsProfile + "KEY_Z = \"C1\" # by hand\n"
	if err := os.WriteFile(doc.Path, []byte(byHand), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetDefault("octave", 1); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(doc.Path)
	if !strings.Contains(string(text), "KEY_Z = \"C1\" # by hand") || !strings.Contains(string(text), "octave = 1") {
		t.Errorf("the file lost a change:\n%s", text)
	}
	if r := doc.Role("L", control.Key(44)); r.Kind != RoleNote {
		t.Errorf("the key added by hand is not shown: %+v", r)
	}

	// A file that was broken meanwhile is not taken over, and nothing is
	// changed on top of it.
	if err := os.WriteFile(doc.Path, []byte("schema = 1\nnonsense = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetDefault("octave", 2); err == nil {
		t.Error("a change was made on top of a file with mistakes")
	}
	if doc.Dirty() {
		t.Error("the refused change waits to be saved")
	}
}

// Changes wait to be saved, can be taken back and made again, and the
// file is written only when asked.
func TestProfileDocUndoAndSave(t *testing.T) {
	doc := openText(t, optionsProfile)
	onDisk := func() string { text, _ := os.ReadFile(doc.Path); return string(text) }
	if doc.Dirty() || doc.CanUndo() || doc.CanRedo() || doc.Undo() || doc.Redo() {
		t.Fatal("a file that was just opened has changes")
	}
	keyQ := control.Key(16)
	if err := doc.SetNote("L", keyQ, 60); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetNote("L", keyQ, 62); err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty() || onDisk() != optionsProfile {
		t.Fatal("a change was written without being saved, or does not count as unsaved")
	}
	// Back one step, back two, forward one.
	if !doc.Undo() || doc.Role("L", keyQ).Note != 60 {
		t.Errorf("after one step back: %+v", doc.Role("L", keyQ))
	}
	if !doc.Undo() || doc.Role("L", keyQ).Kind != RoleNone || doc.Dirty() {
		t.Errorf("after two steps back: %+v, unsaved %v", doc.Role("L", keyQ), doc.Dirty())
	}
	if !doc.Redo() || doc.Role("L", keyQ).Note != 60 || !doc.CanRedo() {
		t.Errorf("after one step forward: %+v", doc.Role("L", keyQ))
	}
	// A new change ends what could be made again.
	if err := doc.SetNote("L", keyQ, 64); err != nil {
		t.Fatal(err)
	}
	if doc.CanRedo() {
		t.Error("a change that was taken back can be made again after a new one")
	}
	// A change that changes nothing is none.
	steps := len(doc.undo)
	if err := doc.SetNote("L", keyQ, 64); err != nil || len(doc.undo) != steps {
		t.Errorf("the same note again: %v, %d steps, want %d", err, len(doc.undo), steps)
	}

	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() || onDisk() != string(doc.Text()) || !strings.Contains(onDisk(), `KEY_Q = "E3"`) {
		t.Errorf("after saving: unsaved %v\n%s", doc.Dirty(), onDisk())
	}
	// Saved is not the end of taking back.
	if !doc.Undo() || !doc.Dirty() {
		t.Error("nothing to take back after saving")
	}
	doc.Revert()
	if doc.Dirty() || doc.Role("L", keyQ).Note != 64 {
		t.Errorf("after dropping what is not saved: %+v", doc.Role("L", keyQ))
	}

	// Other hands change the file while something waits here: what is
	// shown stays, and the page knows.
	if err := doc.SetNote("L", keyQ, 65); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Path, []byte(optionsProfile+"KEY_Z = \"C1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := doc.Reload(); changed || err != nil || !doc.Elsewhere || doc.Role("L", keyQ).Note != 65 {
		t.Errorf("Reload with unsaved changes: %v %v, elsewhere %v", changed, err, doc.Elsewhere)
	}
	if err := doc.Save(); err != nil || doc.Elsewhere || strings.Contains(onDisk(), "KEY_Z") {
		t.Errorf("saving over the other change: %v, elsewhere %v", err, doc.Elsewhere)
	}
}

// Layouts are added, removed, put in another order, and one is chosen to
// start with.
func TestProfileDocLayouts(t *testing.T) {
	doc := openText(t, optionsProfile)
	if doc.DefaultLayout() != "L" {
		t.Fatalf("the profile starts with %q", doc.DefaultLayout())
	}
	if err := doc.AddLayout("Second"); err != nil {
		t.Fatal(err)
	}
	if err := doc.AddLayout("Third"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(doc.Layouts(), ","); got != "L,Second,Third" {
		t.Errorf("layouts: %s", got)
	}
	if err := doc.SetNote("Third", control.Key(16), 60); err != nil {
		t.Errorf("a note in a new layout: %v", err)
	}
	if err := doc.MoveLayout("Third", -1); err != nil || strings.Join(doc.Layouts(), ",") != "L,Third,Second" {
		t.Errorf("moved: %v, %v", doc.Layouts(), err)
	}
	if err := doc.SetDefaultLayout("Third"); err != nil || doc.DefaultLayout() != "Third" {
		t.Errorf("default: %q, %v", doc.DefaultLayout(), err)
	}
	// Removing the layout the profile starts with leaves it starting
	// with the first.
	if err := doc.RemoveLayout("Third"); err != nil || doc.DefaultLayout() != "L" {
		t.Errorf("removed: %v, starts with %q, %v", doc.Layouts(), doc.DefaultLayout(), err)
	}
	if err := doc.RemoveLayout("Second"); err != nil {
		t.Fatal(err)
	}
	if err := doc.RemoveLayout("L"); err == nil {
		t.Error("the last layout was removed")
	}
	// Every step can be taken back.
	for doc.Undo() {
	}
	if string(doc.Text()) != optionsProfile {
		t.Errorf("taking everything back does not give the file as it was:\n%s", doc.Text())
	}
}

// A profile with a rule for what it does not name, shown for a device:
// the controls of the device have the parts the rule gives them, and
// changing one writes a line for it.
func TestProfileDocGenerated(t *testing.T) {
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
	layout := doc.DefaultLayout()
	btnA, btnB, btnX, start := control.Key(0x130), control.Key(0x131), control.Key(0x133), control.Key(0x13b)
	absX, absY, hatX, hatY := control.Axis(0), control.Axis(1), control.Axis(0x10), control.Axis(0x11)

	// With no device to ask, only what the file names has a part.
	if r := doc.Role(layout, btnA); r.Kind != RoleNone || r.Generated {
		t.Errorf("BTN_A with no device: %+v", r)
	}
	doc.ShowFor([]control.Input{
		{Code: btnA}, {Code: btnB}, {Code: btnX}, {Code: start},
		{Code: absX}, {Code: absY}, {Code: hatX, Hat: true}, {Code: hatY, Hat: true},
	})
	if r := doc.Role(layout, btnA); r.Kind != RoleNote || r.Note != 36 || !r.Generated {
		t.Errorf("BTN_A: %+v", r)
	}
	if r := doc.Role(layout, btnX); r.Kind != RoleNote || r.Note != 38 || !r.Generated {
		t.Errorf("BTN_X: %+v", r)
	}
	if r := doc.Role(layout, absX); r.Kind != RoleController || r.Controller != 16 || !r.Generated {
		t.Errorf("ABS_X: %+v", r)
	}
	// What the file names is as the file says.
	if r := doc.Role(layout, start); r.Kind != RoleCommand || r.Generated {
		t.Errorf("BTN_START: %+v", r)
	}
	if r := doc.Role(layout, absY); r.Kind != RoleBend || r.Generated {
		t.Errorf("ABS_Y: %+v", r)
	}
	// The hat has no line either: its directions are the notes after
	// those of the four buttons.
	if r := doc.Role(layout, hatX); !r.Split || !r.Generated || r.Lower != "E1" || r.Upper != "F1" {
		t.Errorf("the hat: %+v", r)
	}

	// Emptied: the control is told to do nothing, or the rule would
	// give it its part back.
	if err := doc.Clear(layout, btnA); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, btnA); r.Kind != RoleNone || !r.Silent || r.Generated {
		t.Errorf("BTN_A emptied: %+v", r)
	}
	if !strings.Contains(string(doc.Text()), `BTN_SOUTH = "nothing"`) && !strings.Contains(string(doc.Text()), `BTN_A = "nothing"`) {
		t.Errorf("no line says that the button does nothing:\n%s", doc.Text())
	}
	// Emptied again is nothing new, and no mistake.
	before := string(doc.Text())
	if err := doc.Clear(layout, btnA); err != nil || string(doc.Text()) != before {
		t.Errorf("emptying an emptied control: %v", err)
	}
	// A named control that the rule covers is silenced the same way,
	// where its line is.
	if err := doc.Clear(layout, absY); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, absY); r.Kind != RoleNone || !r.Silent {
		t.Errorf("ABS_Y emptied: %+v", r)
	}

	// Carried: what the rule gives X goes to A as a line, and X is
	// silent after a move.
	if err := doc.Carry(layout, btnX, btnA, true); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, btnA); r.Kind != RoleNote || r.Note != 38 || r.Generated {
		t.Errorf("BTN_A after X was carried to it: %+v", r)
	}
	if r := doc.Role(layout, btnX); r.Kind != RoleNone || !r.Silent {
		t.Errorf("BTN_X after it was carried away: %+v", r)
	}
	// A new part for a generated control is a line of the layout.
	if err := doc.SetNote(layout, btnB, midi.Note(72)); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, btnB); r.Kind != RoleNote || r.Note != 72 || r.Generated || r.Section.Layout != layout {
		t.Errorf("BTN_B with a note of its own: %+v", r)
	}

	// What is written is a profile, and HIDI makes of it what is shown.
	p, err := config.Parse(doc.Text())
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil || doc.Dirty() {
		t.Fatalf("Save: %v, dirty %v", err, doc.Dirty())
	}
	made := p.For(doc.controls)
	if err := made.Validate(); err != nil {
		t.Fatal(err)
	}
	notes := map[control.Code]midi.Note{}
	for _, b := range made.Layouts[0].Bindings {
		if n, ok := b.Action.(engine.Note); ok && b.Source.Range == engine.RangeWhole {
			notes[b.Source.Code] = n.Note
		}
	}
	if len(notes) != 2 || notes[btnA] != 38 || notes[btnB] != 72 {
		t.Errorf("the notes of the buttons as HIDI plays them: %v", notes)
	}
}
