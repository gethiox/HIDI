package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no arguments", nil, 2, "", "Usage:"},
		{"version", []string{"version"}, 0, "hidi ", ""},
		{"help", []string{"help"}, 0, "Usage:", ""},
		{"unknown command", []string{"bogus"}, 2, "", `unknown command "bogus"`},
		{"run help", []string{"run", "-h"}, 0, "", "-v"},
		{"run with an unknown option", []string{"run", "-bogus"}, 2, "", "-bogus"},
		{"run with an argument", []string{"run", "extra"}, 2, "", `unexpected argument "extra"`},
		{"devices with an argument", []string{"devices", "extra"}, 2, "", `unexpected argument "extra"`},
		{"profiles with an argument", []string{"profiles", "extra"}, 2, "", `unexpected argument "extra"`},
		{"built-in profiles only", []string{"profiles", "-config", ""}, 0, `"Default keyboard"`, ""},
		{"export without a directory", []string{"profiles", "-config", "", "-export"}, 2, "", "-export needs a directory"},
		{"ports with an argument", []string{"ports", "extra"}, 2, "", `unexpected argument "extra"`},
		{"empty connect", []string{"run", "-connect", " "}, 2, "", "must not be empty"},
		{"status with an argument", []string{"status", "extra"}, 2, "", `unexpected argument "extra"`},
		{"doctor with an argument", []string{"doctor", "extra"}, 2, "", `unexpected argument "extra"`},
		{"bad toggle key", []string{"run", "-toggle", "KEY_NOPE"}, 2, "", "unknown control"},
		{"toggle with an axis", []string{"run", "-toggle", "ABS_X"}, 2, "", "not a key"},
		{"grab without toggle", []string{"run", "-toggle", ""}, 2, "", "needs a toggle combination"},
		{"off without toggle", []string{"run", "-toggle", "", "-no-grab", "-off"}, 2, "", "-off needs a toggle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) || (tt.wantStdout == "" && stdout.Len() != 0) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) || (tt.wantStderr == "" && stderr.Len() != 0) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestParseToggle(t *testing.T) {
	keys, err := parseToggle(defaultToggle)
	if err != nil || len(keys) != 3 {
		t.Fatalf("parseToggle(default) = %v, %v", keys, err)
	}
	if got := formatToggle(keys); got != defaultToggle {
		t.Errorf("formatToggle = %q, want %q", got, defaultToggle)
	}
	if keys, err := parseToggle(" key_a + KEY_B "); err != nil || len(keys) != 2 {
		t.Errorf("parseToggle with spaces and lower case = %v, %v", keys, err)
	}
	if keys, err := parseToggle("  "); err != nil || keys != nil {
		t.Errorf("parseToggle of blank = %v, %v; want no keys", keys, err)
	}
	if _, err := parseToggle("KEY_A+KEY_A"); err == nil {
		t.Error("parseToggle accepted a repeated key")
	}
}

func TestProfilesCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	hidi := func(args ...string) (code int, stdout, stderr string) {
		var out, errOut bytes.Buffer
		code = run(context.Background(), append([]string{"profiles", "-config", "", "-profiles", dir}, args...), &out, &errOut)
		return code, out.String(), errOut.String()
	}

	// A directory that does not exist yet is not an error.
	code, stdout, stderr := hidi()
	if code != 0 || stderr != "" || !strings.Contains(stdout, dir) || !strings.Contains(stdout, "built-in/keyboard.toml") {
		t.Fatalf("listing without a directory: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	code, stdout, stderr = hidi("-export")
	keyboard := filepath.Join(dir, "keyboard.toml")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "written  "+keyboard) {
		t.Fatalf("export: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, stdout, _ = hidi("-export"); code != 0 || !strings.Contains(stdout, "kept     "+keyboard) {
		t.Errorf("second export: code %d, stdout %q", code, stdout)
	}

	// The user's files are listed before the built-in ones they override.
	code, stdout, _ = hidi()
	mine, builtin := strings.Index(stdout, keyboard), strings.Index(stdout, "built-in/keyboard.toml")
	if code != 0 || mine < 0 || builtin < mine {
		t.Errorf("listing: code %d, stdout %q", code, stdout)
	}

	// A broken file is reported with everything wrong in it, fails the
	// command, and leaves the other profiles listed.
	broken := filepath.Join(dir, "broken.toml")
	if err := os.WriteFile(broken, []byte("schema = 1\nvolume = 3\n[[layout]]\nname = \"x\"\n[layout.bindings]\nKEY_A = \"H9\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = hidi()
	if code != 1 || !strings.Contains(stdout, keyboard) {
		t.Errorf("listing with a broken file: code %d, stdout %q", code, stdout)
	}
	for _, want := range []string{broken, "volume", "H9"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not mention %q", stderr, want)
		}
	}
}

func TestListFlag(t *testing.T) {
	var l listFlag
	if err := l.Set(" USB MIDI "); err != nil {
		t.Fatal(err)
	}
	if err := l.Set("128:0"); err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || l[0] != "USB MIDI" || l.String() != "USB MIDI, 128:0" {
		t.Errorf("listFlag = %q", l)
	}
}

// HIDI's own directory: not in use until "hidi run" fills it, and then the
// files in it are all the profiles there are.
func TestConfigDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "HIDI")
	hidi := func(args ...string) (code int, stdout, stderr string) {
		var out, errOut bytes.Buffer
		code = run(context.Background(), append(args, "-config", root), &out, &errOut)
		return code, out.String(), errOut.String()
	}

	code, stdout, stderr := hidi("config")
	if code != 0 || stderr != "" || !strings.Contains(stdout, root) || !strings.Contains(stdout, "not in use yet") {
		t.Fatalf("config before the first start: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("looking at the directory created it")
	}
	if code, stdout, _ = hidi("profiles"); code != 0 || !strings.Contains(stdout, "built-in/keyboard.toml") {
		t.Errorf("profiles before the first start: code %d, stdout %q", code, stdout)
	}

	// What "hidi run" does first.
	pl := findPlace(root, "", true)
	if !pl.managed || len(pl.problems) != 0 || len(pl.notes) != 1 || !strings.Contains(pl.notes[0], "first start") {
		t.Fatalf("first start: %+v", pl)
	}
	profiles, problems, err := pl.load()
	if err != nil || len(problems) != 0 || len(profiles) == 0 {
		t.Fatalf("loading after the first start: %d profiles, %v, %v", len(profiles), problems, err)
	}
	for _, p := range profiles {
		if p.IsBuiltin() {
			t.Errorf("%q is read from inside HIDI, not from its file", p.Name)
		}
	}

	code, stdout, _ = hidi("config")
	if code != 0 || !strings.Contains(stdout, "is in use") {
		t.Errorf("config afterwards: code %d, stdout %q", code, stdout)
	}
	keyboard := filepath.Join(root, "profiles", "keyboard.toml")
	if code, stdout, _ = hidi("profiles"); code != 0 || !strings.Contains(stdout, keyboard) || strings.Contains(stdout, "built-in/") {
		t.Errorf("profiles afterwards: code %d, stdout %q", code, stdout)
	}
	// A removed profile is gone, not replaced by the one inside HIDI.
	os.Remove(keyboard)
	if _, stdout, _ = hidi("profiles"); strings.Contains(stdout, "keyboard.toml") {
		t.Errorf("a removed profile is still listed: %q", stdout)
	}

	// Mistakes in the settings are told, and the defaults used.
	os.WriteFile(filepath.Join(root, "settings.toml"), []byte("schema = 1\n[input]\nvolume = 3\n"), 0o644)
	pl = findPlace(root, "", true)
	if len(pl.problems) != 1 || !strings.Contains(pl.problems[0], "input.volume") || !pl.settings.Input.GrabKeyboards {
		t.Errorf("settings with a mistake: %+v", pl)
	}

	// A directory of something else is left alone.
	other := filepath.Join(t.TempDir(), "HIDI")
	os.MkdirAll(filepath.Join(other, "factory"), 0o755)
	pl = findPlace(other, "", true)
	if pl.managed || len(pl.problems) != 1 || !strings.Contains(pl.problems[0], "left alone") {
		t.Errorf("a directory of something else: %+v", pl)
	}
	if entries, _ := os.ReadDir(other); len(entries) != 1 {
		t.Error("a directory of something else was written to")
	}
}
