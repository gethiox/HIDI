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
		Watch(ctx, dir, 10*time.Millisecond, func() ([]*Profile, []Problem, error) { return Load(dir) }, func(profiles []*Profile, problems []Problem) {
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
