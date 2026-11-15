package main

import (
	"bytes"
	"context"
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
