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
		{"built-in profiles only", []string{"profiles", "-profiles", ""}, 0, `"Default keyboard"`, ""},
		{"export without a directory", []string{"profiles", "-profiles", "", "-export"}, 2, "", "-export needs a directory"},
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
		code = run(context.Background(), append([]string{"profiles", "-profiles", dir}, args...), &out, &errOut)
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
