package gui

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/ipc"
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

	if r := doc.Role(layout, absX); r.Kind != RoleController || r.Controller != 0 || !r.Shape.Centered || r.Shape.Deadzone != 0.1 || !r.Shape.OwnDeadzone || r.Split {
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
	if err := doc.SetBend(layout, absMisc, Shape{Centered: true, Deadzone: 0.05, OwnDeadzone: true}); err != nil {
		t.Fatal(err)
	}
	shape := doc.Role(layout, absX).Shape
	shape.Deadzone, shape.OwnDeadzone = 0, false
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
	// Start is the fourth button of this device and plays by its number
	// like the others: the profile names no button.
	if r := doc.Role(layout, start); r.Kind != RoleNote || r.Note != 39 || !r.Generated {
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
	if len(notes) != 3 || notes[btnA] != 38 || notes[btnB] != 72 || notes[start] != 39 {
		t.Errorf("the notes of the buttons as HIDI plays them: %v", notes)
	}
}

// openPad opens a copy of the game-controller profile HIDI comes with,
// shown for a stick with four buttons, two axes and a hat.
func openPad(t *testing.T) (doc *ProfileDoc, layout string) {
	t.Helper()
	files, err := config.BuiltinFiles()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gamepad.toml")
	if err := os.WriteFile(path, files["gamepad.toml"], 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err = OpenProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.ShowFor([]control.Input{
		{Code: control.Key(0x120)}, {Code: control.Key(0x121)}, {Code: control.Key(0x122)}, {Code: control.Key(0x123)},
		{Code: control.Axis(0)}, {Code: control.Axis(1)},
		{Code: control.Axis(0x10), Hat: true}, {Code: control.Axis(0x11), Hat: true},
	})
	return doc, doc.DefaultLayout()
}

// played returns what HIDI makes of the document for the device it is
// shown for: every binding of the first layout by its key, as its role's
// short word.
func played(t *testing.T, doc *ProfileDoc) map[string]string {
	t.Helper()
	p, err := config.Parse(doc.Text())
	if err != nil {
		t.Fatalf("what is shown is no profile: %v\n%s", err, doc.Text())
	}
	made := p.For(doc.controls)
	if err := made.Validate(); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, b := range made.Layouts[0].Bindings {
		var r Role
		r.take(b.Action)
		out[Spot{b.Source.Code, b.Source.Range}.Key()] = r.Short()
	}
	return out
}

// The two directions of a hat are buttons: each is read and changed by
// itself, and changing one leaves the other with what the rule gave it.
func TestProfileDocHalves(t *testing.T) {
	doc, layout := openPad(t)
	hat := control.Axis(0x10)
	down, up := Spot{hat, engine.RangeLower}, Spot{hat, engine.RangeUpper}
	if down.Key() != "ABS_HAT0X-" || up.Key() != "ABS_HAT0X+" || Whole(hat).Key() != "ABS_HAT0X" {
		t.Fatalf("keys: %q %q %q", down.Key(), up.Key(), Whole(hat).Key())
	}
	// Four buttons: the hat's notes are the four after theirs.
	if r := doc.RoleAt(layout, down); r.Kind != RoleNote || r.Note != 40 || !r.Generated {
		t.Errorf("the lower half: %+v", r)
	}
	if r := doc.RoleAt(layout, up); r.Kind != RoleNote || r.Note != 41 || !r.Generated {
		t.Errorf("the upper half: %+v", r)
	}
	if r := doc.Role(layout, hat); r.Kind != RoleNone || !r.Split || !doc.Bound(layout, hat) {
		t.Errorf("the hat as a whole: %+v", r)
	}

	// One direction runs a command. The other keeps its note, which a
	// line now says, since the rule no longer covers a control that is
	// named.
	if err := doc.SetCommandAt(layout, up, engine.CommandOctaveUp); err != nil {
		t.Fatal(err)
	}
	got := played(t, doc)
	if got["ABS_HAT0X+"] != "octave_up" || got["ABS_HAT0X-"] != "E1" {
		t.Errorf("after one half was changed: + %q, - %q", got["ABS_HAT0X+"], got["ABS_HAT0X-"])
	}
	if r := doc.RoleAt(layout, down); r.Generated || r.Note != 40 {
		t.Errorf("the half nobody touched: %+v", r)
	}
	// Emptied, a half says that it does nothing; the other still plays.
	if err := doc.ClearAt(layout, down); err != nil {
		t.Fatal(err)
	}
	if r := doc.RoleAt(layout, down); r.Kind != RoleNone || !r.Silent || r.Short() != "nothing" {
		t.Errorf("the emptied half: %+v", r)
	}
	if got := played(t, doc); got["ABS_HAT0X+"] != "octave_up" || got["ABS_HAT0X-"] != "nothing" {
		t.Errorf("after one half was emptied: %v", got)
	}
	// Carried: a button's note onto a direction of the other hat axis,
	// whose other direction keeps its own.
	other := control.Axis(0x11)
	if err := doc.CarryAt(layout, Whole(control.Key(0x120)), Spot{other, engine.RangeUpper}, false); err != nil {
		t.Fatal(err)
	}
	if got := played(t, doc); got["ABS_HAT0Y+"] != "C1" || got["ABS_HAT0Y-"] != "F#1" || got["BTN_TRIGGER"] != "C1" {
		t.Errorf("after a note was copied onto a direction: %v", got)
	}
	// An axis triggers a note where it is told to, and reversed.
	if err := doc.SetShapeAt(layout, up, Shape{Threshold: 0.3, Invert: true}); err != nil {
		t.Fatal(err)
	}
	if r := doc.RoleAt(layout, up); r.Shape.Threshold != 0.3 || !r.Shape.Invert || r.Kind != RoleCommand {
		t.Errorf("after its threshold was set: %+v", r)
	}
	if !strings.Contains(string(doc.Text()), `"ABS_HAT0X+" = { command = "octave_up", invert = true, threshold = 0.3 }`) {
		t.Errorf("the line of the half:\n%s", doc.Text())
	}
	// Everything can be taken back, to the letter.
	for doc.Undo() {
	}
	if doc.Dirty() {
		t.Error("taking everything back left a change")
	}
}

// An axis is split into its two halves and joined again.
func TestProfileDocSplitAndJoin(t *testing.T) {
	doc, layout := openPad(t)
	x, y := control.Axis(0), control.Axis(1)
	lower := Spot{x, engine.RangeLower}

	// The rule's controller becomes two, the upper half keeping the
	// number.
	if r := doc.Role(layout, x); r.Kind != RoleController || r.Controller != 16 || !r.Generated {
		t.Fatalf("ABS_X: %+v", r)
	}
	if err := doc.Split(layout, x); err != nil {
		t.Fatal(err)
	}
	if got := played(t, doc); got["ABS_X+"] != "cc16" || got["ABS_X-"] != "cc17" || got["ABS_X"] != "" {
		t.Errorf("after the split: + %q, - %q, whole %q", got["ABS_X+"], got["ABS_X-"], got["ABS_X"])
	}
	if r := doc.Role(layout, x); !r.Split || r.Kind != RoleNone {
		t.Errorf("the axis as a whole after the split: %+v", r)
	}
	// Splitting what is split changes nothing.
	before := string(doc.Text())
	if err := doc.Split(layout, x); err != nil || string(doc.Text()) != before {
		t.Errorf("a second split: %v", err)
	}
	// A half is changed like any control.
	if err := doc.SetControllerAt(layout, lower, 74, Shape{Deadzone: 0.1, OwnDeadzone: true, Centered: true}); err != nil {
		t.Fatal(err)
	}
	if r := doc.RoleAt(layout, lower); r.Controller != 74 || r.Shape.Deadzone != 0.1 || r.Shape.Centered {
		t.Errorf("the lower half: %+v", r)
	}
	// Joined: the axis as a whole does what its upper half did, with its
	// rest in the middle.
	if err := doc.Join(layout, x); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, x); r.Kind != RoleController || r.Controller != 16 || !r.Shape.Centered || r.Split || r.Generated {
		t.Errorf("after joining: %+v", r)
	}
	if got := played(t, doc); got["ABS_X+"] != "" || got["ABS_X-"] != "" || got["ABS_X"] != "cc16" {
		t.Errorf("after joining: %v", got)
	}

	// The pitch bend of the stick splits into up and down, the way the
	// stick is reversed.
	if err := doc.Split(layout, y); err != nil {
		t.Fatal(err)
	}
	up, down := doc.RoleAt(layout, Spot{y, engine.RangeUpper}), doc.RoleAt(layout, Spot{y, engine.RangeLower})
	if up.Kind != RoleBend || down.Kind != RoleBend || !up.Down || down.Down || up.Shape.Invert || down.Shape.OwnDeadzone {
		t.Errorf("the halves of the pitch bend: + %+v, - %+v", up, down)
	}
	if _, err := config.Parse(doc.Text()); err != nil {
		t.Fatal(err)
	}
	// An axis that does nothing splits into two halves that say so,
	// where the rule would otherwise give it back its part.
	if err := doc.Join(layout, y); err != nil {
		t.Fatal(err)
	}
	if err := doc.Clear(layout, y); err != nil {
		t.Fatal(err)
	}
	if err := doc.Split(layout, y); err != nil {
		t.Fatal(err)
	}
	if got := played(t, doc); got["ABS_Y+"] != "nothing" || got["ABS_Y-"] != "nothing" || got["ABS_Y"] != "" {
		t.Errorf("a silent axis split: %v", got)
	}
}

func TestRoleSendsAndLabels(t *testing.T) {
	x := control.Axis(0)
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.001 }
	for _, tt := range []struct {
		name     string
		role     Role
		at       Spot
		v        float64
		from, to float64
		held     bool
	}{
		{"a controller", Role{Kind: RoleController}, Whole(x), 0.25, 0, 0.25, false},
		{"reversed", Role{Kind: RoleController, Shape: Shape{Invert: true}}, Whole(x), 0.25, 0, 0.75, false},
		{"resting in the middle", Role{Kind: RoleController, Shape: Shape{Centered: true}}, Whole(x), 0.5, 0, 0.5, false},
		{"the upper half at rest", Role{Kind: RoleController}, Spot{x, engine.RangeUpper}, 0.5, 0, 0, false},
		{"the upper half pushed", Role{Kind: RoleController}, Spot{x, engine.RangeUpper}, 0.75, 0, 0.5, false},
		{"the lower half pushed", Role{Kind: RoleController}, Spot{x, engine.RangeLower}, 0, 0, 1, false},
		{"the pitch at rest", Role{Kind: RoleBend, Shape: Shape{Centered: true}}, Whole(x), 0.5, 0.5, 0.5, false},
		{"the pitch bent up", Role{Kind: RoleBend, Shape: Shape{Centered: true}}, Whole(x), 1, 0.5, 1, false},
		{"the pitch bent down by a half", Role{Kind: RoleBend, Down: true}, Spot{x, engine.RangeLower}, 0, 0, 0.5, false},
		{"a note below its point", Role{Kind: RoleNote, Shape: Shape{Threshold: 0.3}}, Whole(x), 0.2, 0, 0, false},
		{"a note past its point", Role{Kind: RoleNote, Shape: Shape{Threshold: 0.3}}, Whole(x), 0.4, 0, 0, true},
		{"a note at the usual half", Role{Kind: RoleNote}, Whole(x), 0.4, 0, 0, false},
		{"a key that is down", Role{Kind: RoleNote}, Whole(control.Key(30)), 1, 0, 0, true},
		{"nothing", Role{}, Whole(x), 1, 0, 0, false},
	} {
		from, to, held := tt.role.Sends(tt.at, tt.v)
		if !near(from, tt.from) || !near(to, tt.to) || held != tt.held {
			t.Errorf("%s: from %v to %v, held %v; want %v to %v, %v", tt.name, from, to, held, tt.from, tt.to, tt.held)
		}
	}
	for role, want := range map[Role]string{
		{Kind: RoleController, Controller: 16}: "cc16", {Kind: RoleBend}: "PB", {Kind: RoleBend, Down: true}: "PB↓",
		{Kind: RoleNote, Text: "C3"}: "C3", {Silent: true}: "nothing", {}: "",
	} {
		if got := role.Short(); got != want {
			t.Errorf("%+v in short: %q, want %q", role, got, want)
		}
	}

	// A control is shown by its number where the device gives it one;
	// the name is added where names are wanted, and stands alone where
	// there is no number.
	pad := &ipc.Instrument{Complete: true, Buttons: []ipc.Button{{Name: "BTN_TRIGGER"}, {Name: "BTN_THUMB"}}, Axes: []ipc.Axis{{Name: "ABS_X"}, {Name: "ABS_HAT0X", Hat: true}}}
	thumb, hat := control.Key(0x121), control.Axis(0x10)
	for _, tt := range []struct {
		device *ipc.Instrument
		at     Spot
		names  bool
		want   string
	}{
		{pad, Whole(thumb), false, "1"},
		{pad, Whole(thumb), true, "1 · BTN_THUMB"},
		{pad, Whole(control.Key(0x13b)), false, "BTN_START"},
		{nil, Whole(thumb), false, "BTN_THUMB"},
		{pad, Spot{hat, engine.RangeLower}, false, "1 −"},
		{pad, Spot{hat, engine.RangeUpper}, true, "1 · ABS_HAT0X +"},
		{&ipc.Instrument{}, Whole(control.Key(30)), false, "KEY_A"},
	} {
		if got := SpotLabel(tt.device, tt.at, tt.names); got != tt.want {
			t.Errorf("SpotLabel(%v, names %v) = %q, want %q", tt.at, tt.names, got, tt.want)
		}
	}
}

// The key that turns HIDI off and on is one for the whole profile: given
// to another key, it leaves the one that had it, and what the new key did
// in the layout goes.
func TestProfileDocToggle(t *testing.T) {
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
	capsLock, keyZ := control.Key(58), control.Key(44)
	if r := doc.Role(layout, capsLock); r.Kind != RoleCommand || r.Command != engine.CommandToggle || r.Section != config.Global {
		t.Fatalf("Caps Lock: %+v", r)
	}
	if r := doc.Role(layout, keyZ); r.Kind != RoleNote || r.Section.Layout != layout {
		t.Fatalf("Z: %+v", r)
	}
	if err := doc.SetCommand(layout, keyZ, engine.CommandToggle); err != nil {
		t.Fatal(err)
	}
	p, err := config.Parse(doc.Text())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Toggle, []control.Code{keyZ}) {
		t.Errorf("the toggle of the profile: %v", p.Toggle)
	}
	if r := doc.Role(layout, capsLock); r.Kind != RoleNone {
		t.Errorf("Caps Lock afterwards: %+v", r)
	}
	if r := doc.Role(layout, keyZ); r.Kind != RoleCommand || r.Command != engine.CommandToggle || r.Section != config.Global {
		t.Errorf("Z afterwards: %+v", r)
	}
	// One step back, and it is as it was.
	if !doc.Undo() || doc.Dirty() {
		t.Error("taking it back did not leave the profile as saved")
	}
	// An axis cannot be the toggle.
	if err := doc.SetCommandAt(layout, Whole(control.Axis(0)), engine.CommandToggle); err == nil {
		t.Error("an axis was made the toggle")
	}
}

// The dead zone of an axis is the profile's unless the axis has its own.
func TestProfileDocDeadzone(t *testing.T) {
	doc, layout := openPad(t)
	x, y := control.Axis(0), control.Axis(1)
	if doc.Profile.Deadzone != 0 {
		t.Fatalf("the dead zone of the profile HIDI comes with: %v", doc.Profile.Deadzone)
	}
	for _, code := range []control.Code{x, y} {
		if r := doc.Role(layout, code); r.Shape.Deadzone != 0 || r.Shape.OwnDeadzone {
			t.Errorf("%s before anything was set: %+v", code, r.Shape)
		}
	}
	// The profile's applies to what the rule binds and to what a line
	// names without saying more.
	if err := doc.SetDeadzone(0.05); err != nil {
		t.Fatal(err)
	}
	for _, code := range []control.Code{x, y} {
		if r := doc.Role(layout, code); r.Shape.Deadzone != 0.05 || r.Shape.OwnDeadzone {
			t.Errorf("%s with the profile's dead zone: %+v", code, r.Shape)
		}
	}
	// An axis with its own keeps it, also at zero, whatever the
	// profile's becomes.
	shape := doc.Role(layout, y).Shape
	shape.OwnDeadzone, shape.Deadzone = true, 0
	if err := doc.SetShapeAt(layout, Whole(y), shape); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetDeadzone(0.2); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, y); r.Shape.Deadzone != 0 || !r.Shape.OwnDeadzone {
		t.Errorf("an axis with a dead zone of its own: %+v", r.Shape)
	}
	if r := doc.Role(layout, x); r.Shape.Deadzone != 0.2 || r.Shape.OwnDeadzone || !r.Generated {
		t.Errorf("an axis without: %+v", r)
	}
	if !strings.Contains(string(doc.Text()), "deadzone = 0 }") || !strings.Contains(string(doc.Text()), "\ndeadzone = 0.2") {
		t.Errorf("the file:\n%s", doc.Text())
	}
	// And gives it up again.
	shape.OwnDeadzone = false
	if err := doc.SetShapeAt(layout, Whole(y), shape); err != nil {
		t.Fatal(err)
	}
	if r := doc.Role(layout, y); r.Shape.Deadzone != 0.2 || r.Shape.OwnDeadzone {
		t.Errorf("after giving up its own: %+v", r.Shape)
	}
	// What HIDI plays has the same.
	p, err := config.Parse(doc.Text())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range p.For(doc.controls).Layouts[0].Bindings {
		if b.Source.Code.Kind == control.KindAxis && b.Source.Deadzone != 0.2 {
			t.Errorf("%s plays with a dead zone of %v", b.Source.Code, b.Source.Deadzone)
		}
	}
}
