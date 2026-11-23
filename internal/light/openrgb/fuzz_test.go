package openrgb

import "testing"

// FuzzParseController: whatever a server sends for a controller, reading
// it reports a controller or an error, and takes no more memory than
// what was sent can justify.
func FuzzParseController(f *testing.F) {
	f.Add([]byte{}, uint32(3))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, uint32(3))
	f.Add(make([]byte, 64), uint32(1))
	f.Fuzz(func(t *testing.T, data []byte, version uint32) {
		c, err := parseController(data, version%6)
		if err != nil {
			return
		}
		if len(c.LEDs) > len(data) {
			t.Fatalf("%d lights from %d bytes", len(c.LEDs), len(data))
		}
	})
}
