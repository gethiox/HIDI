package gui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWindowState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "window.json")
	// Nothing kept yet, and nowhere to keep anything.
	if s := LoadState(path); len(s.ShowInput) != 0 || len(s.ShowOutput) != 0 {
		t.Errorf("a state before any was kept: %+v", s)
	}
	if err := (State{ShowInput: []string{"x"}}).Save(""); err != nil {
		t.Errorf("keeping nothing: %v", err)
	}

	s := State{ShowInput: []string{"Pad | gamepad | usb-1"}, ShowOutput: []string{"Board | keyboard | "}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got := LoadState(path)
	if !slices.Equal(got.ShowInput, s.ShowInput) || !slices.Equal(got.ShowOutput, s.ShowOutput) {
		t.Errorf("read back: %+v", got)
	}
	// A file that is no state gives a window as it first is.
	os.WriteFile(path, []byte("{nonsense"), 0o644)
	if got := LoadState(path); len(got.ShowInput) != 0 {
		t.Errorf("read from nonsense: %+v", got)
	}
}
