package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSocketPath(t *testing.T) {
	run := filepath.Join(t.TempDir(), "run")
	t.Setenv("XDG_RUNTIME_DIR", run)
	if path, err := SocketPath(); err != nil || path != filepath.Join(run, "HIDI", "control.sock") {
		t.Errorf("SocketPath = %q, %v", path, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	state, _ := DefaultState()
	if path, err := SocketPath(); err != nil || path != filepath.Join(state, "control.sock") {
		t.Errorf("SocketPath without a runtime directory = %q, %v; want it in %s", path, err, state)
	}
}

// A state directory with only HIDI's files in it stays; one with anything
// else is moved aside, to the first free name.
func TestClaimState(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "HIDI")
	if to, err := ClaimState(dir); to != "" || err != nil {
		t.Fatalf("a directory that is not there: %q, %v", to, err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"written.json", "window.json", "crash.log", "crash-20261127-101010.log", ".tmp-123", "control.sock.lock"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if to, err := ClaimState(dir); to != "" || err != nil {
		t.Fatalf("HIDI's own files: moved to %q, %v", to, err)
	}
	// Taken already, as by a directory moved aside before.
	if err := os.Mkdir(filepath.Join(parent, "HIDI-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	to, err := ClaimState(dir)
	if err != nil || to != filepath.Join(parent, "HIDI-old_1") {
		t.Fatalf("a directory of another program: moved to %q, %v", to, err)
	}
	if _, err := os.Stat(filepath.Join(to, "other.dat")); err != nil {
		t.Errorf("what was in it is not where it went: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("%s is still there", dir)
	}
}
