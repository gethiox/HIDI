package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

const validProfile = `schema = 1
name = "Mine"

[[layout]]
name = "one"
[layout.bindings]
KEY_A = "C3"
`

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "b-good.toml", validProfile)
	bad := write(t, dir, "a-bad.toml", "schema = 1\nname = \"Bad\"\nvolume = 3\n\n[[layout]]\n[layout.bindings]\nKEY_A = \"H9\"\n")
	write(t, dir, "notes.txt", "not a profile")
	write(t, dir, ".swp.toml", "an editor's temporary file")
	if err := os.Mkdir(filepath.Join(dir, "sub.toml"), 0o755); err != nil {
		t.Fatal(err)
	}

	profiles, problems := LoadDir(dir)
	if len(profiles) != 1 || profiles[0].Name != "Mine" || profiles[0].Source != good {
		t.Fatalf("profiles: %+v", profiles)
	}
	if profiles[0].IsBuiltin() {
		t.Error("a user's profile counts as built-in")
	}
	if len(problems) != 1 || problems[0].File != bad {
		t.Fatalf("problems: %+v", problems)
	}
	// Every mistake of the file is reported, under the file's name.
	text := problems[0].Error()
	for _, want := range []string{bad + ":\n", "volume", "H9"} {
		if !strings.Contains(text, want) {
			t.Errorf("problem %q does not mention %q", text, want)
		}
	}
}

func TestLoadDirOrderIsByName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "20.toml", strings.Replace(validProfile, "Mine", "Second", 1))
	write(t, dir, "10.toml", strings.Replace(validProfile, "Mine", "First", 1))
	profiles, _ := LoadDir(dir)
	if len(profiles) != 2 || profiles[0].Name != "First" || profiles[1].Name != "Second" {
		t.Errorf("order: %+v", profiles)
	}
}

func TestLoadDirMissingAndUnreadable(t *testing.T) {
	profiles, problems := LoadDir(filepath.Join(t.TempDir(), "absent"))
	if profiles != nil || problems != nil {
		t.Errorf("a missing directory gave %v, %v", profiles, problems)
	}
	file := write(t, t.TempDir(), "file", "")
	if _, problems := LoadDir(file); len(problems) != 1 || problems[0].File != file {
		t.Errorf("a file in place of the directory gave %v", problems)
	}
}

// A profile of the user wins over a built-in one that describes the device
// equally well, and a built-in profile carries its origin.
func TestLoadGivesUserProfilesPrecedence(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mine.toml", strings.Replace(validProfile, "name = \"Mine\"", "name = \"Mine\"\n[match]\ntype = \"keyboard\"", 1))
	profiles, problems, err := Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("Load: %v, %v", err, problems)
	}
	got := Select(profiles, control.DeviceInfo{Type: control.DeviceKeyboard})
	if got == nil || got.Name != "Mine" {
		t.Errorf("selected %+v, want the user's profile", got)
	}
	pad := Select(profiles, control.DeviceInfo{Type: control.DeviceGamepad})
	if pad == nil || !pad.IsBuiltin() || pad.Source != BuiltinSource+"gamepad.toml" {
		t.Errorf("game controller got %+v", pad)
	}

	builtin, _, err := Load("")
	if err != nil || len(builtin) != len(profiles)-1 {
		t.Errorf("Load without a directory: %d profiles, %v", len(builtin), err)
	}
}

func TestDigestFollowsContent(t *testing.T) {
	a, err := Parse([]byte(validProfile))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Parse([]byte(validProfile))
	c, _ := Parse([]byte(strings.Replace(validProfile, "C3", "D3", 1)))
	if a.Digest != b.Digest {
		t.Error("the same content has two digests")
	}
	if a.Digest == c.Digest {
		t.Error("different content has one digest")
	}
}

func TestWriteBuiltin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hidi", "profiles")
	written, skipped, err := WriteBuiltin(dir)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("WriteBuiltin: %v, skipped %v", err, skipped)
	}
	builtin, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(builtin) {
		t.Fatalf("%d files written for %d built-in profiles", len(written), len(builtin))
	}
	// What was written loads again as the same profiles.
	profiles, problems := LoadDir(dir)
	if len(problems) != 0 || len(profiles) != len(builtin) {
		t.Fatalf("written files: %d profiles, problems %v", len(profiles), problems)
	}
	for i := range builtin {
		if profiles[i].Digest != builtin[i].Digest {
			t.Errorf("%s differs from the built-in profile", profiles[i].Source)
		}
	}

	// A second run must not overwrite the user's edits.
	edited := written[0]
	if err := os.WriteFile(edited, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(written[1]); err != nil {
		t.Fatal(err)
	}
	again, skipped, err := WriteBuiltin(dir)
	if err != nil || len(again) != 1 || again[0] != written[1] || len(skipped) != len(builtin)-1 {
		t.Fatalf("second run: wrote %v, skipped %v, %v", again, skipped, err)
	}
	if data, _ := os.ReadFile(edited); string(data) != "mine" {
		t.Error("an existing file was overwritten")
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mine.toml", validProfile)

	type result struct {
		names    []string
		problems int
	}
	var mu sync.Mutex
	var results []result
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, dir, 10*time.Millisecond, nil, func() ([]*Profile, []Problem, error) { return Load(dir) }, func(profiles []*Profile, problems []Problem) {
			var names []string
			for _, p := range profiles {
				if !p.IsBuiltin() {
					names = append(names, p.Name)
				}
			}
			mu.Lock()
			results = append(results, result{names, len(problems)})
			mu.Unlock()
		})
	}()
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(results)
	}
	wait := func(n int) result {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for count() < n {
			if time.Now().After(deadline) {
				t.Fatalf("no change number %d was reported", n)
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		return results[n-1]
	}

	time.Sleep(80 * time.Millisecond)
	if n := count(); n != 0 {
		t.Fatalf("%d changes reported although nothing changed", n)
	}

	write(t, dir, "second.toml", strings.Replace(validProfile, "Mine", "Second", 1))
	if r := wait(1); len(r.names) != 2 || r.problems != 0 {
		t.Errorf("after adding a file: %+v", r)
	}
	write(t, dir, "second.toml", "schema = 1\nbroken")
	if r := wait(2); len(r.names) != 1 || r.problems != 1 {
		t.Errorf("after breaking a file: %+v", r)
	}
	if err := os.Remove(filepath.Join(dir, "second.toml")); err != nil {
		t.Fatal(err)
	}
	if r := wait(3); len(r.names) != 1 || r.problems != 0 {
		t.Errorf("after removing the file: %+v", r)
	}
	write(t, dir, "notes.txt", "ignored")
	time.Sleep(80 * time.Millisecond)
	if n := count(); n != 3 {
		t.Errorf("a file that is not a profile caused a reload")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return after its context was cancelled")
	}
}

// A profile that is a link to a file elsewhere is followed too: the link
// does not change when the file does.
func TestFingerprintFollowsLinks(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	target := filepath.Join(elsewhere, "mine.toml")
	if err := os.WriteFile(target, []byte("schema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "mine.toml")); err != nil {
		t.Skip("no links here:", err)
	}
	before := fingerprint(dir)
	if err := os.WriteFile(target, []byte("schema = 1\n# changed, and longer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fingerprint(dir) == before {
		t.Error("a change to the file behind a link was not noticed")
	}
}

// The factory profiles and the user's come from two directories. A
// profile of the user's comes before a factory profile that fits as
// well, unless it says that it is not enabled.
func TestLoadKinds(t *testing.T) {
	root := t.TempDir()
	factory, user := filepath.Join(root, "factory"), filepath.Join(root, "user")
	os.MkdirAll(factory, 0o755)
	os.MkdirAll(user, 0o755)
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const rest = "[match]\ntype = \"keyboard\"\n[[layout]]\nname = \"L\"\n[layout.bindings]\nKEY_A = \"C3\"\n"
	write(factory, "keyboard.toml", "schema = 1\nname = \"Factory\"\nenabled = false\n"+rest)
	write(user, "keyboard.toml", "schema = 1\nname = \"Mine\"\n"+rest)
	before := fingerprint(root)

	all, problems := LoadKinds(factory, user)
	if len(problems) != 0 || len(all) != 2 || all[0].Kind != KindFactory || all[1].Kind != KindUser {
		t.Fatalf("loaded: %v, %v", all, problems)
	}
	keyboard := control.DeviceInfo{Type: control.DeviceKeyboard}
	// The factory profile is used whatever it says about being enabled.
	if got := Select(Enabled(all[:1]), keyboard); got == nil || got.Name != "Factory" {
		t.Errorf("the factory profile alone: %v", got)
	}
	if got := Select(Enabled(all), keyboard); got == nil || got.Name != "Mine" {
		t.Errorf("with the user's profile beside it: %v", got)
	}

	// Turned off, the user's profile steps back, and the change is seen.
	write(user, "keyboard.toml", "schema = 1\nname = \"Mine\"\nenabled = false\n"+rest)
	if fingerprint(root) == before {
		t.Error("a change one directory down is not seen")
	}
	all, _ = LoadKinds(factory, user)
	if len(all) != 2 || len(Enabled(all)) != 1 {
		t.Fatalf("with the user's profile off: %d loaded, %d enabled", len(all), len(Enabled(all)))
	}
	if got := Select(Enabled(all), keyboard); got == nil || got.Name != "Factory" {
		t.Errorf("with the user's profile off: %v", got)
	}
}

// Told to look, Watch takes what is there at once, where by itself it
// would wait for its next look and for the directory to settle.
func TestWatchLooksWhenTold(t *testing.T) {
	dir := t.TempDir()
	look := make(chan struct{}, 1)
	got := make(chan int, 4)
	go Watch(t.Context(), dir, time.Hour, look, func() ([]*Profile, []Problem, error) { return Load(dir) }, func(profiles []*Profile, _ []Problem) {
		got <- len(profiles)
	})
	time.Sleep(20 * time.Millisecond) // the watcher has seen the empty directory
	text := "schema = 1\nname = \"New\"\n[[layout]]\nname = \"L\"\n"
	if err := os.WriteFile(filepath.Join(dir, "new.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	look <- struct{}{}
	builtin, _ := Builtin()
	select {
	case n := <-got:
		if n != len(builtin)+1 {
			t.Errorf("%d profiles loaded, want the built-in ones and the new one", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("told to look, the watcher loaded nothing")
	}
	// Told again with nothing changed, it does nothing.
	look <- struct{}{}
	select {
	case n := <-got:
		t.Errorf("loaded again, %d profiles, though nothing changed", n)
	case <-time.After(50 * time.Millisecond):
	}
}
