package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// seenDevice is a device HIDI has met.
type seenDevice struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// seenDevices remembers the devices HIDI has met, in a file, so that the
// settings can offer a device that is not plugged in right now.
type seenDevices struct {
	path string // empty: nothing is kept beyond this run

	mu   sync.Mutex
	list []seenDevice
}

// maxSeen bounds the list; the devices met longest ago are forgotten.
const maxSeen = 64

// loadSeen reads the list. A file that is missing or cannot be read gives
// an empty one: the list is a convenience.
func loadSeen(path string) *seenDevices {
	s := &seenDevices{path: path}
	if path == "" {
		return s
	}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &s.list)
	}
	return s
}

// note remembers a device and reports whether it was new.
func (s *seenDevices) note(d seenDevice) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.list, d) {
		return false
	}
	s.list = append(s.list, d)
	if len(s.list) > maxSeen {
		s.list = slices.Delete(s.list, 0, len(s.list)-maxSeen)
	}
	if s.path != "" {
		// Whole or not at all, like every file HIDI writes.
		if data, err := json.MarshalIndent(s.list, "", "  "); err == nil {
			tmp := filepath.Join(filepath.Dir(s.path), ".tmp-devices")
			if os.WriteFile(tmp, append(data, '\n'), 0o644) == nil {
				os.Rename(tmp, s.path)
			}
		}
	}
	return true
}

func (s *seenDevices) all() []seenDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}
