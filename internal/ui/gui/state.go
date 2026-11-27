package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is how the window was left, as far as it takes that up again the
// next time: nothing the user sets, and so no part of the settings. It is
// kept in a file of its own among what HIDI notes for itself.
type State struct {
	// ShowInput lists the instruments whose keys, buttons and axes are
	// drawn, and ShowOutput those whose notes and controllers are, each
	// by [EntryKey].
	ShowInput  []string `json:"show_input"`
	ShowOutput []string `json:"show_output"`
}

// LoadState reads how the window was left. A file that is missing or
// cannot be read gives a window as it first is: the file is a
// convenience.
func LoadState(path string) State {
	var s State
	if path == "" {
		return s
	}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &s) != nil {
			return State{}
		}
	}
	return s
}

// Save writes how the window is left, whole or not at all. An empty path
// keeps nothing.
func (s State) Save(path string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeWhole(path, append(data, '\n'))
}
