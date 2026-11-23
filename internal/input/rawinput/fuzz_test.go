package rawinput

import (
	"testing"
	"time"
)

// FuzzSteamReader: whatever a controller reports, reading it does not
// fail, and what it reports stays within what an axis can be.
func FuzzSteamReader(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	f.Add([]byte{0x42, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, report []byte) {
		r := newSteamReader()
		r.connected = func() {}
		for range 2 {
			for _, ev := range r.read(report, time.Time{}) {
				if ev.Value < -0.0001 || ev.Value > 1.0001 {
					t.Fatalf("%v reports %v", ev.Code, ev.Value)
				}
			}
		}
		parsePath(string(report))
	})
}
