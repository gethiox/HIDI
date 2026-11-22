package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/light"
)

func TestDefaultRoundTrip(t *testing.T) {
	got, err := Parse(Default().Text())
	if err != nil {
		t.Fatalf("the default file does not parse: %v", err)
	}
	want := Default()
	if !slices.Equal(got.Input.Toggle, want.Input.Toggle) || got.Input.GrabKeyboards != want.Input.GrabKeyboards ||
		got.Input.GrabGamepads || !got.Input.StartOn || len(got.Output.Connect) != 0 || got.Window != want.Window {
		t.Errorf("the default file reads as %+v", got)
	}
	names := []string{}
	for _, c := range want.Input.Toggle {
		names = append(names, c.String())
	}
	if strings.Join(names, " ") != "KEY_LEFTCTRL KEY_LEFTSHIFT KEY_LEFTALT" {
		t.Errorf("the default toggle is %v", names)
	}

	changed := Default()
	changed.Input.Toggle = []control.Code{control.Key(1)}
	changed.Input.GrabGamepads = true
	changed.Input.StartOn = false
	changed.Output.Connect = []string{"USB MIDI", `odd "name"`}
	changed.Window = Window{ShowAtStart: true, FPS: 120, BackgroundFPS: 30, LogHeight: 200}
	got, err = Parse(changed.Text())
	if err != nil {
		t.Fatalf("changed settings do not parse: %v", err)
	}
	if !slices.Equal(got.Input.Toggle, changed.Input.Toggle) || !got.Input.GrabGamepads || got.Input.StartOn ||
		!slices.Equal(got.Output.Connect, changed.Output.Connect) || got.Window != changed.Window {
		t.Errorf("changed settings read as %+v", got)
	}
}

func TestParse(t *testing.T) {
	// What is left out keeps its default.
	s, err := Parse([]byte("schema = 1\n[input]\ngrab_gamepads = true\n"))
	if err != nil || !s.Input.GrabGamepads || !s.Input.GrabKeyboards || len(s.Input.Toggle) != 3 {
		t.Errorf("partial file: %+v, %v", s, err)
	}
	// An empty list is not the same as no list.
	s, err = Parse([]byte("schema = 1\n[input]\ntoggle = []\ngrab_keyboards = false\n"))
	if err != nil || len(s.Input.Toggle) != 0 {
		t.Errorf("no toggle: %+v, %v", s, err)
	}

	for _, tt := range []struct{ name, text, want string }{
		{"no schema", "[input]\n", "schema"},
		{"future schema", "schema = 9\n", "schema 9"},
		{"unknown setting", "schema = 1\n[input]\ngrab = true\n", `unknown setting "input.grab"`},
		{"unknown table", "schema = 1\n[colours]\nx = 1\n", "unknown setting"},
		{"bad key", "schema = 1\n[input]\ntoggle = [\"KEY_NOPE\"]\n", `"KEY_NOPE" is not the name of a key`},
		{"axis as key", "schema = 1\n[input]\ntoggle = [\"ABS_X\"]\n", "not the name of a key"},
		{"grab without toggle", "schema = 1\n[input]\ntoggle = []\n", "needs a toggle combination"},
		{"off without toggle", "schema = 1\n[input]\ntoggle = []\ngrab_keyboards = false\nstart_on = false\n", "starting turned off"},
		{"negative", "schema = 1\n[window]\nlog_height = -1\n", "cannot be negative"},
		{"not TOML", "schema = \n", "toml"},
	} {
		s, err := Parse([]byte(tt.text))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want one about %q", tt.name, err, tt.want)
		}
		if !s.Input.GrabKeyboards || len(s.Input.Toggle) != 3 {
			t.Errorf("%s: a file with mistakes did not give the defaults: %+v", tt.name, s)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if s, err := Load(filepath.Join(dir, "none.toml")); err != nil || !s.Input.StartOn {
		t.Errorf("a missing file: %+v, %v", s, err)
	}
	file := filepath.Join(dir, "settings.toml")
	os.WriteFile(file, []byte("schema = 1\n[window]\nlog_height = 120\n"), 0o644)
	if s, err := Load(file); err != nil || s.Window.LogHeight != 120 {
		t.Errorf("Load: %+v, %v", s, err)
	}
}

var shipped = map[string][]byte{
	"keyboard.toml": []byte("schema = 1\nname = \"Keyboard\"\n"),
	"gamepad.toml":  []byte("schema = 1\nname = \"Gamepad\"\n"),
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPrepareFirstStart(t *testing.T) {
	p := PathsIn(filepath.Join(t.TempDir(), "sub", DirName))
	report, err := Prepare(p, shipped)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !report.Managed || !report.Created || len(report.Written) != 3 || len(report.Updated) != 0 {
		t.Errorf("report: %+v", report)
	}
	if read(t, filepath.Join(p.Factory, "keyboard.toml")) != string(shipped["keyboard.toml"]) {
		t.Error("the factory profile was not written as it is")
	}
	if _, err := Load(p.Settings); err != nil {
		t.Errorf("the written settings do not load: %v", err)
	}
	// The user's directory is there, and empty.
	if entries, err := os.ReadDir(p.User); err != nil || len(entries) != 0 {
		t.Errorf("the user's profiles: %v, %v", entries, err)
	}
	// A second start finds everything in place.
	report, err = Prepare(p, shipped)
	if err != nil || !report.Managed || report.Created || len(report.Written)+len(report.Updated)+len(report.Removed) != 0 {
		t.Errorf("second start: %+v, %v", report, err)
	}
	if entries, _ := os.ReadDir(p.Factory); len(entries) != 2 {
		t.Errorf("%d files among the factory profiles", len(entries))
	}
}

// The factory profiles are HIDI's: always as this HIDI has them, whoever
// changed them. The user's profiles and the settings are never touched.
func TestPrepareLater(t *testing.T) {
	p := PathsIn(filepath.Join(t.TempDir(), DirName))
	if _, err := Prepare(p, shipped); err != nil {
		t.Fatal(err)
	}
	keyboard, gamepad := filepath.Join(p.Factory, "keyboard.toml"), filepath.Join(p.Factory, "gamepad.toml")
	mine := filepath.Join(p.User, "keyboard.toml")

	// The user changes a factory profile, which is marked read-only,
	// removes another, keeps a profile of their own under the name of a
	// factory one, and changes the settings.
	os.Chmod(keyboard, 0o644)
	os.WriteFile(keyboard, []byte("schema = 1\nname = \"Changed in place\"\n"), 0o644)
	os.Chmod(gamepad, 0o644)
	os.Remove(gamepad)
	os.WriteFile(mine, []byte("schema = 1\nname = \"Mine\"\n"), 0o644)
	os.WriteFile(p.Settings, []byte("schema = 1\n[window]\nlog_height = 99\n"), 0o644)

	// A later HIDI comes with a changed profile, a new one, and without
	// one it had.
	later := map[string][]byte{
		"keyboard.toml": []byte("schema = 1\nname = \"Keyboard, better\"\n"),
		"wheel.toml":    []byte("schema = 1\nname = \"Wheel\"\n"),
	}
	report, err := Prepare(p, later)
	if err != nil || !report.Managed {
		t.Fatalf("Prepare: %+v, %v", report, err)
	}
	if !strings.Contains(read(t, keyboard), "Keyboard, better") {
		t.Error("a factory profile was not brought up to date")
	}
	if _, err := os.Stat(gamepad); err == nil {
		t.Error("a factory profile HIDI no longer has is there")
	}
	if !strings.Contains(read(t, mine), `"Mine"`) {
		t.Error("the user's own profile was touched")
	}
	if !strings.Contains(read(t, p.Settings), "log_height = 99") {
		t.Error("the settings were overwritten")
	}
	if len(report.Updated) != 1 || filepath.Base(report.Updated[0]) != "keyboard.toml" ||
		len(report.Written) != 1 || filepath.Base(report.Written[0]) != "wheel.toml" {
		t.Errorf("report: %+v", report)
	}

	// A factory profile HIDI drops goes; the same start twice changes
	// nothing more.
	if _, err := Prepare(p, shipped); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Factory, "wheel.toml")); err == nil {
		t.Error("a dropped factory profile stayed")
	}
	report, err = Prepare(p, shipped)
	if err != nil || len(report.Written)+len(report.Updated)+len(report.Removed)+len(report.Moved) != 0 {
		t.Errorf("a start with nothing to do: %+v, %v", report, err)
	}
	// Removed settings stay removed.
	os.Remove(p.Settings)
	if _, err := Prepare(p, shipped); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Settings); err == nil {
		t.Error("settings the user removed came back")
	}
}

// An earlier HIDI 2 kept its copies of the profiles, and the user's own,
// in one directory, with a record of which it wrote. Its unchanged copies
// go; what the user changed or added moves to the user's profiles.
func TestPrepareMovesTheEarlierLayout(t *testing.T) {
	p := PathsIn(filepath.Join(t.TempDir(), DirName))
	os.MkdirAll(p.Profiles, 0o755)
	untouched, changedByUser := shipped["keyboard.toml"], []byte("schema = 1\nname = \"My gamepad\"\n")
	os.WriteFile(filepath.Join(p.Profiles, "keyboard.toml"), untouched, 0o644)
	os.WriteFile(filepath.Join(p.Profiles, "gamepad.toml"), changedByUser, 0o644)
	os.WriteFile(filepath.Join(p.Profiles, "mine.toml"), []byte("schema = 1\nname = \"Mine\"\n"), 0o644)
	os.WriteFile(p.Settings, Default().Text(), 0o644)
	os.WriteFile(filepath.Join(p.Root, ".written.json"), []byte(`{"note":"x","files":{`+
		`"profiles/keyboard.toml":"`+fingerprint(untouched)+`",`+
		`"profiles/gamepad.toml":"`+fingerprint(shipped["gamepad.toml"])+`",`+
		`"settings.toml":"`+fingerprint(Default().Text())+`"}}`), 0o644)

	report, err := Prepare(p, shipped)
	if err != nil || !report.Managed {
		t.Fatalf("Prepare: %+v, %v", report, err)
	}
	if len(report.Removed) != 1 || len(report.Moved) != 2 {
		t.Errorf("report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(p.Profiles, "keyboard.toml")); err == nil {
		t.Error("the unchanged copy is still in the directory of profiles")
	}
	if got := read(t, filepath.Join(p.User, "gamepad.toml")); got != string(changedByUser) {
		t.Errorf("the profile the user changed: %q", got)
	}
	if !strings.Contains(read(t, filepath.Join(p.User, "mine.toml")), "Mine") {
		t.Error("the user's own profile did not move")
	}
	if read(t, filepath.Join(p.Factory, "gamepad.toml")) != string(shipped["gamepad.toml"]) {
		t.Error("the factory profile is not as HIDI has it")
	}
	// Nothing is moved twice.
	report, err = Prepare(p, shipped)
	if err != nil || len(report.Moved)+len(report.Removed)+len(report.Written)+len(report.Updated) != 0 {
		t.Errorf("second start: %+v, %v", report, err)
	}
}

func TestPrepareLeavesOtherDirectoriesAlone(t *testing.T) {
	for name, make := range map[string]func(root string){
		"a file of another program": func(root string) { os.WriteFile(filepath.Join(root, "config.ini"), []byte("x"), 0o644) },
		"settings of another kind":  func(root string) { os.WriteFile(filepath.Join(root, "settings.toml"), []byte("volume = 3\n"), 0o644) },
		"an unknown directory in profiles": func(root string) {
			os.MkdirAll(filepath.Join(root, "profiles", "old"), 0o755)
		},
	} {
		p := PathsIn(filepath.Join(t.TempDir(), DirName))
		os.MkdirAll(p.Root, 0o755)
		make(p.Root)
		report, err := Prepare(p, shipped)
		if err != nil || report.Managed || report.Unrecognised == "" {
			t.Errorf("%s: %+v, %v", name, report, err)
		}
		if _, err := os.Stat(p.Factory); err == nil {
			t.Errorf("%s: HIDI wrote into a directory that is not its own", name)
		}
		if Managed(p) {
			t.Errorf("%s: taken for HIDI's directory", name)
		}
	}
}

// A directory with settings and profiles of HIDI 2 and no record, as a
// first build of it left one or as someone copied it, is taken over.
func TestPrepareTakesOverEarlierDirectory(t *testing.T) {
	p := PathsIn(filepath.Join(t.TempDir(), DirName))
	os.MkdirAll(p.Profiles, 0o755)
	os.WriteFile(filepath.Join(p.Profiles, "mine.toml"), []byte("schema = 1\nname = \"Mine\"\n"), 0o644)
	os.WriteFile(p.Settings, []byte("schema = 1\n[window]\nlog_height = 50\n"), 0o644)
	if !Managed(p) {
		t.Fatal("not taken for HIDI's directory before the first start")
	}
	report, err := Prepare(p, shipped)
	if err != nil || !report.Managed || report.Created {
		t.Fatalf("Prepare: %+v, %v", report, err)
	}
	// Without a record every profile there is the user's.
	if !strings.Contains(read(t, filepath.Join(p.User, "mine.toml")), "Mine") || !strings.Contains(read(t, p.Settings), "log_height = 50") {
		t.Error("what was there was not kept")
	}
	if _, err := os.Stat(filepath.Join(p.Factory, "keyboard.toml")); err != nil {
		t.Errorf("the factory profiles were not written: %v", err)
	}
}

func TestPaths(t *testing.T) {
	root, err := DefaultRoot()
	if err != nil {
		t.Skipf("no configuration directory here: %v", err)
	}
	if filepath.Base(root) != "HIDI" {
		t.Errorf("DefaultRoot = %q", root)
	}
	p := PathsIn(root)
	if filepath.Dir(p.Settings) != root || filepath.Dir(p.Profiles) != root || filepath.Base(p.Profiles) != "profiles" {
		t.Errorf("paths: %+v", p)
	}
}

func TestWatch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.toml")
	os.WriteFile(file, Default().Text(), 0o644)
	type result struct {
		s   Settings
		err error
	}
	got := make(chan result, 8)
	go Watch(t.Context(), file, 10*time.Millisecond, func(s Settings, err error) { got <- result{s, err} })

	next := func(what string) result {
		t.Helper()
		select {
		case r := <-got:
			return r
		case <-time.After(5 * time.Second):
			t.Fatalf("no report of %s", what)
			return result{}
		}
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case r := <-got:
		t.Fatalf("a report without a change: %+v", r)
	default:
	}

	changed := Default()
	changed.Input.GrabGamepads = true
	if err := Save(file, changed); err != nil {
		t.Fatal(err)
	}
	if r := next("a change"); r.err != nil || !r.s.Input.GrabGamepads {
		t.Errorf("after a change: %+v", r)
	}
	os.WriteFile(file, []byte("schema = 1\n[input]\nvolume = 1\n"), 0o644)
	if r := next("a mistake"); r.err == nil {
		t.Errorf("a file with a mistake was reported as fine: %+v", r.s)
	}
	os.Remove(file)
	if r := next("the removal"); r.err != nil || !r.s.Input.GrabKeyboards {
		t.Errorf("after the removal: %+v", r)
	}
}

func TestLightsSettings(t *testing.T) {
	s, err := Parse([]byte("schema = 1\n[lights]\nopenrgb = true\n"))
	if err != nil || !s.Lights.OpenRGB || s.Lights.Address != DefaultOpenRGBAddress {
		t.Errorf("lights turned on: %+v, %v", s.Lights, err)
	}
	s, err = Parse([]byte("schema = 1\n[lights]\naddress = \"192.168.1.20:6742\"\n"))
	if err != nil || s.Lights.OpenRGB || s.Lights.Address != "192.168.1.20:6742" {
		t.Errorf("another address: %+v, %v", s.Lights, err)
	}
	if _, err := Parse([]byte("schema = 1\n[lights]\naddress = \"nowhere\"\n")); err == nil || !strings.Contains(err.Error(), "lights.address") {
		t.Errorf("an address without a port: %v", err)
	}
	changed := Default()
	changed.Lights = Lights{OpenRGB: true, Address: "[::1]:7000"}
	if got, err := Parse(changed.Text()); err != nil || got.Lights != changed.Lights {
		t.Errorf("written and read back: %+v, %v", got.Lights, err)
	}
}

func TestToggleTapSettings(t *testing.T) {
	s, err := Parse([]byte("schema = 1\n[input]\ntoggle = [\"KEY_CAPSLOCK\"]\ntoggle_tap = true\nalways_grab = true\n"))
	if err != nil || !s.Input.ToggleTap || !s.Input.AlwaysGrab {
		t.Fatalf("parsed %+v, %v", s.Input, err)
	}
	again, err := Parse(s.Text())
	if err != nil || !again.Input.ToggleTap || !again.Input.AlwaysGrab || len(again.Input.Toggle) != 1 {
		t.Errorf("written and read again: %+v, %v", again.Input, err)
	}
	// A tap needs exactly one key.
	if _, err := Parse([]byte("schema = 1\n[input]\ntoggle_tap = true\n")); err == nil || !strings.Contains(err.Error(), "toggle_tap") {
		t.Errorf("a tap with three keys: %v", err)
	}
}

// Whatever a MIDI port is called, the settings that name it can be read
// again.
func TestOddNamesRoundTrip(t *testing.T) {
	s := Default()
	s.Output.Connect = []string{"bell\a", `quote " and \ slash`, "tab\there", "ünïcödé ♯", "bad\xffutf8", "del\x7f"}
	got, err := Parse(s.Text())
	if err != nil {
		t.Fatalf("the settings written cannot be read: %v\n%s", err, s.Text())
	}
	want := slices.Clone(s.Output.Connect)
	want[4] = "bad�utf8" // what is no text cannot be kept as it is
	if !slices.Equal(got.Output.Connect, want) {
		t.Errorf("read back %q, want %q", got.Output.Connect, want)
	}
}

// A directory that is HIDI's, without the record of what HIDI wrote there,
// gets the record, so that it counts as in use before the next start has
// made it ready.
func TestPrepareWritesAMissingRecord(t *testing.T) {
	p := PathsIn(filepath.Join(t.TempDir(), "HIDI"))
	profiles := map[string][]byte{"keyboard.toml": []byte("schema = 1\n")}
	if _, err := Prepare(p, profiles); err != nil {
		t.Fatal(err)
	}
	if !Managed(p) {
		t.Fatal("not in use after the first start")
	}
	if err := os.Remove(p.record); err != nil {
		t.Fatal(err)
	}
	// And something a write left behind when HIDI was ended in it.
	if err := os.WriteFile(filepath.Join(p.Root, ".tmp-123"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Prepare(p, profiles)
	if err != nil || !report.Managed || report.Unrecognised != "" {
		t.Fatalf("Prepare without the record: %+v, %v", report, err)
	}
	if !Managed(p) {
		t.Error("the record was not written again")
	}
}

func TestDevicesAndColoursSettings(t *testing.T) {
	s := Default()
	if s.Devices.Mode != Blacklist || !s.Devices.Allowed("Any Keyboard") {
		t.Errorf("by default every device should play: %+v", s.Devices)
	}
	if s.Lights.Colours != light.DefaultColours() {
		t.Errorf("default colours: %+v", s.Lights.Colours)
	}

	s.Devices = Devices{Mode: Whitelist, List: []string{"My \"best\" Keyboard", "Pad"}}
	s.Lights.Colours.Passthrough = light.RGB{R: 0x12, G: 0xab, B: 0xff}
	s.Lights.Colours.Command = light.RGB{B: 0x80}
	s.Shown = Shown{Input: []string{"Pad"}, Output: []string{"My \"best\" Keyboard"}}
	got, err := Parse(s.Text())
	if err != nil {
		t.Fatalf("what was written is not read back: %v", err)
	}
	if got.Devices.Mode != Whitelist || !slices.Equal(got.Devices.List, s.Devices.List) ||
		got.Lights.Colours != s.Lights.Colours || !slices.Equal(got.Shown.Input, s.Shown.Input) || !slices.Equal(got.Shown.Output, s.Shown.Output) {
		t.Errorf("read back: %+v %+v %+v", got.Devices, got.Lights.Colours, got.Shown)
	}
	if got.Devices.Allowed("Other") || !got.Devices.Allowed("Pad") {
		t.Error("a whitelist allows exactly the listed devices")
	}
	got.Devices.Mode = Blacklist
	if !got.Devices.Allowed("Other") || got.Devices.Allowed("Pad") {
		t.Error("a blacklist allows exactly the devices that are not listed")
	}

	// One colour set, the others keep their defaults.
	one, err := Parse([]byte("schema = 1\n[lights.colours]\nwhite = \"#010203\"\n"))
	want := light.DefaultColours()
	want.White = light.RGB{R: 1, G: 2, B: 3}
	if err != nil || one.Lights.Colours != want {
		t.Errorf("one colour set: %+v, %v", one.Lights.Colours, err)
	}
	for text, mention := range map[string]string{
		"schema = 1\n[devices]\nmode = \"some\"\n":              "devices.mode",
		"schema = 1\n[lights.colours]\nwhite = \"green\"\n":     "#rrggbb",
		"schema = 1\n[lights.colours]\nwhite = \"#12345g\"\n":   "#rrggbb",
		"schema = 1\n[lights.colours]\nsparkle = \"#000000\"\n": "lights.colours.sparkle",
	} {
		if _, err := Parse([]byte(text)); err == nil || !strings.Contains(err.Error(), mention) {
			t.Errorf("%q: error = %v, want it to mention %q", text, err, mention)
		}
	}
}

func TestLearnThreshold(t *testing.T) {
	s := Default()
	if s.Input.LearnThreshold != 20 {
		t.Errorf("the default is %d", s.Input.LearnThreshold)
	}
	s.Input.LearnThreshold = 35
	if got, err := Parse(s.Text()); err != nil || got.Input.LearnThreshold != 35 {
		t.Errorf("written and read back: %d, %v", got.Input.LearnThreshold, err)
	}
	for _, bad := range []string{"0", "101", "-5"} {
		if _, err := Parse([]byte("schema = 1\n[input]\nlearn_threshold = " + bad + "\n")); err == nil || !strings.Contains(err.Error(), "learn_threshold") {
			t.Errorf("learn_threshold = %s: %v", bad, err)
		}
	}
}
