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

// A directory in another case is renamed where the system tells no cases
// apart, and left alone where it does, as here.
func TestOwnName(t *testing.T) {
	parent := t.TempDir()
	lower := filepath.Join(parent, "hidi")
	if err := os.Mkdir(lower, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := OwnName(filepath.Join(parent, "HIDI")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(parent)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	_, statErr := os.Stat(filepath.Join(parent, "HIDI"))
	switch {
	case statErr == nil && (len(names) != 1 || names[0] != "HIDI"):
		t.Errorf("one directory for both cases, still named %v", names)
	case statErr != nil && (len(names) != 1 || names[0] != "hidi"):
		t.Errorf("cases told apart, and the other directory was touched: %v", names)
	}
	if err := OwnName(filepath.Join(parent, "absent", "HIDI")); err != nil {
		t.Errorf("a parent that is not there: %v", err)
	}
}
