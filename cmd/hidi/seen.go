package main

import (
	"slices"
	"sync"
)

// seenDevice is a device HIDI has met.
type seenDevice struct {
	Name string
	Type string
}

// seenDevices remembers the devices HIDI has met since it started, so
// that the settings can offer a device that was unplugged meanwhile.
// Nothing of it is kept beyond the run: what HIDI sees while it runs is
// enough to choose from.
type seenDevices struct {
	mu   sync.Mutex
	list []seenDevice
}

// maxSeen bounds the list; the devices met longest ago are forgotten.
const maxSeen = 64

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
	return true
}

func (s *seenDevices) all() []seenDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}
