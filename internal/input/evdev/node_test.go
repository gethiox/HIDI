//go:build linux

package evdev

import (
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// rawEvent builds a struct input_event the way the kernel lays it out on
// this architecture.
func rawEvent(typ, code uint16, value int32) []byte {
	raw := make([]byte, rawEventSize)
	body := raw[2*wordSize:]
	native.PutUint16(body[0:], typ)
	native.PutUint16(body[2:], code)
	native.PutUint32(body[4:], uint32(value))
	return raw
}

// testNode returns a node that is not backed by a device, for exercising
// the translation of raw events.
func testNode() *Node {
	return &Node{
		ranges:   map[uint16]axisRange{0: {min: -100, max: 100}, 2: {min: 0, max: 255}},
		absValue: map[uint16]int32{},
	}
}

func TestRawEventSize(t *testing.T) {
	// 24 bytes on 64-bit kernels, 16 bytes on 32-bit ones.
	if want := map[int]int{8: 24, 4: 16}[wordSize]; rawEventSize != want {
		t.Errorf("rawEventSize = %d, want %d", rawEventSize, want)
	}
}

// The request numbers must match the values the C macros in linux/input.h
// expand to.
func TestRequestNumbers(t *testing.T) {
	tests := []struct {
		name      string
		got, want uintptr
	}{
		{"EVIOCGID", reqID, 0x80084502},
		{"EVIOCGRAB", reqGrab, 0x40044590},
		{"EVIOCGNAME(256)", reqName(256), 0x81004506},
		{"EVIOCGPHYS(256)", reqPhys(256), 0x81004507},
		{"EVIOCGUNIQ(256)", reqUniq(256), 0x81004508},
		{"EVIOCGKEY(96)", reqKeyState(96), 0x80604518},
		{"EVIOCGBIT(0, 4)", reqBits(0, 4), 0x80044520},
		{"EVIOCGBIT(EV_KEY, 96)", reqBits(evKey, 96), 0x80604521},
		{"EVIOCGABS(5)", reqAbs(5), 0x80184545},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %#x, want %#x", tt.name, tt.got, tt.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		value int32
		r     axisRange
		want  float64
	}{
		{0, axisRange{0, 255}, 0},
		{255, axisRange{0, 255}, 1},
		{0, axisRange{-32768, 32767}, 32768.0 / 65535},
		{-100, axisRange{-100, 100}, 0},
		{0, axisRange{-100, 100}, 0.5},
		{100, axisRange{-100, 100}, 1},
		{-1, axisRange{-1, 1}, 0}, // a hat
		{0, axisRange{-1, 1}, 0.5},
		{1, axisRange{-1, 1}, 1},
		{500, axisRange{0, 255}, 1}, // clamped
		{-5, axisRange{0, 255}, 0},
		{7, axisRange{0, 0}, 0}, // empty range
		{0, axisRange{-2147483648, 2147483647}, 2147483648.0 / 4294967295},
	}
	for _, tt := range tests {
		if got := normalize(tt.value, tt.r); got != tt.want {
			t.Errorf("normalize(%d, %+v) = %v, want %v", tt.value, tt.r, got, tt.want)
		}
	}
}

func TestTranslate(t *testing.T) {
	n := testNode()
	feed := func(typ, code uint16, value int32) []control.Event {
		return n.translate(rawEvent(typ, code, value), nil)
	}
	one := func(events []control.Event, code control.Code, value float64) {
		t.Helper()
		if len(events) != 1 || events[0].Code != code || events[0].Value != value {
			t.Errorf("events = %+v, want one event %v = %v", events, code, value)
		}
	}
	none := func(events []control.Event) {
		t.Helper()
		if len(events) != 0 {
			t.Errorf("events = %+v, want none", events)
		}
	}

	one(feed(evKey, 30, 1), control.Key(30), 1)
	none(feed(evKey, 30, 2)) // key repeat
	none(feed(evKey, 30, 1)) // already down
	one(feed(evKey, 30, 0), control.Key(30), 0)
	none(feed(evKey, 30, 0)) // already up
	none(feed(evKey, keyCount, 1))

	one(feed(evAbs, 0, 100), control.Axis(0), 1)
	one(feed(evAbs, 2, 0), control.Axis(2), 0)
	none(feed(evAbs, 9, 5)) // an axis the device did not declare
	none(feed(evRel, 0, 5)) // relative movement is not reported
	none(feed(evSyn, synReport, 0))
}

// After the kernel reports dropped events, the rest of the packet must be
// discarded, and reading resumes with the next packet.
func TestTranslateDiscardsAfterDrop(t *testing.T) {
	n := testNode()
	var events []control.Event
	for _, raw := range [][]byte{
		rawEvent(evSyn, synDropped, 0),
		rawEvent(evKey, 30, 1), // part of the incomplete packet
		rawEvent(evAbs, 0, 100),
		rawEvent(evSyn, synReport, 0), // ends it; the state is read back here
		rawEvent(evKey, 31, 1),
	} {
		events = n.translate(raw, events)
	}
	// The test node has no device to read the state back from, so only the
	// event after the packet remains.
	if len(events) != 1 || events[0].Code != control.Key(31) {
		t.Errorf("events = %+v, want only KEY 31", events)
	}
	if n.dropped {
		t.Error("the node is still discarding after the packet ended")
	}
}

func TestEventTime(t *testing.T) {
	raw := make([]byte, rawEventSize)
	if wordSize == 8 {
		native.PutUint64(raw[0:], 1700000000)
		native.PutUint64(raw[8:], 250000)
	} else {
		native.PutUint32(raw[0:], 1700000000)
		native.PutUint32(raw[4:], 250000)
	}
	if got, want := eventTime(raw), time.Unix(1700000000, 250000000); !got.Equal(want) {
		t.Errorf("eventTime = %v, want %v", got, want)
	}
}
