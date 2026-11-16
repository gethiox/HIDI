//go:build linux

package evdev

import (
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input/evdev/uinput"
)

// virtualDevice is an input device created through uinput.
type virtualDevice struct {
	t    *testing.T
	dev  *uinput.Device
	path string // the /dev/input/event* node of the device
}

const (
	testName    = "HIDI test device"
	testPhys    = "hidi-test/input0"
	testVendor  = 0x1234
	testProduct = 0x5678
	testVersion = 0x0111
	busVirtual  = uinput.BusVirtual
)

// virtualSpec describes a device to create through uinput.
type virtualSpec struct {
	name, phys string
	keys       []uint16
	axes       map[uint16]axisRange
}

// newVirtualDevice creates a device with the given keys and axes and the
// default name and location.
func newVirtualDevice(t *testing.T, keys []uint16, axes map[uint16]axisRange) *virtualDevice {
	t.Helper()
	return createVirtualDevice(t, virtualSpec{name: testName, phys: testPhys, keys: keys, axes: axes})
}

// createVirtualDevice creates a device. Tests are skipped where uinput is
// unavailable, unless HIDI_REQUIRE_UINPUT is set, as it is in CI, where a
// skip would hide that nothing was tested.
func createVirtualDevice(t *testing.T, spec virtualSpec) *virtualDevice {
	t.Helper()
	axes := map[uint16]uinput.Range{}
	for axis, r := range spec.axes {
		axes[axis] = uinput.Range{Min: r.min, Max: r.max}
	}
	dev, err := uinput.Create(uinput.Spec{
		Name: spec.name, Phys: spec.phys,
		Vendor: testVendor, Product: testProduct, Version: testVersion,
		Keys: spec.keys, Axes: axes,
	})
	if errors.Is(err, uinput.ErrUnavailable) {
		if os.Getenv("HIDI_REQUIRE_UINPUT") != "" {
			t.Fatalf("HIDI_REQUIRE_UINPUT is set, but %v", err)
		}
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	d := &virtualDevice{t: t, dev: dev, path: dev.Path}
	t.Cleanup(d.destroy)
	return d
}

func (d *virtualDevice) destroy() { d.dev.Close() }

// emit makes the device report one event, followed by the end of a packet.
func (d *virtualDevice) emit(typ, code uint16, value int32) {
	d.t.Helper()
	if err := d.dev.Emit(typ, code, value); err != nil {
		d.t.Fatal(err)
	}
}

func openNode(t *testing.T, path string) *Node {
	t.Helper()
	n, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

// reader pumps a node into a channel so that tests can wait with a timeout.
type reader struct {
	events chan control.Event
	err    chan error
}

func startReader(n *Node) *reader {
	r := &reader{events: make(chan control.Event, 1<<17), err: make(chan error, 1)}
	go func() {
		var buf []control.Event
		for {
			var err error
			buf, err = n.Read(buf[:0])
			for _, ev := range buf {
				r.events <- ev
			}
			if err != nil {
				r.err <- err
				return
			}
		}
	}()
	return r
}

// next returns the next event, failing the test if none arrives.
func (r *reader) next(t *testing.T) control.Event {
	t.Helper()
	select {
	case ev := <-r.events:
		return ev
	case err := <-r.err:
		t.Fatalf("Read failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
	return control.Event{}
}

// quiet reports whether no event arrives for a short while.
func (r *reader) quiet() bool {
	select {
	case <-r.events:
		return false
	case <-time.After(200 * time.Millisecond):
		return true
	}
}

var (
	testKeys = []uint16{30, 31, 32}
	testAxes = map[uint16]axisRange{0: {min: -100, max: 100}, 2: {min: 0, max: 255}}
)

func TestNodeDescribesDevice(t *testing.T) {
	d := newVirtualDevice(t, testKeys, testAxes)
	info := openNode(t, d.path).Info()

	if info.Path != d.path || info.Name != testName || info.Phys != testPhys {
		t.Errorf("Path = %q, Name = %q, Phys = %q", info.Path, info.Name, info.Phys)
	}
	want := control.HardwareID{Bus: busVirtual, Vendor: testVendor, Product: testProduct, Version: testVersion}
	if info.Hardware != want {
		t.Errorf("Hardware = %v, want %v", info.Hardware, want)
	}
	if want := []control.Code{control.Key(30), control.Key(31), control.Key(32)}; !slices.Equal(info.Keys, want) {
		t.Errorf("Keys = %v, want %v", info.Keys, want)
	}
	if want := []control.Code{control.Axis(0), control.Axis(2)}; !slices.Equal(info.Axes, want) {
		t.Errorf("Axes = %v, want %v", info.Axes, want)
	}
	if info.Relative {
		t.Error("Relative = true for a device without relative axes")
	}
}

func TestNodeReadsEvents(t *testing.T) {
	d := newVirtualDevice(t, testKeys, testAxes)
	r := startReader(openNode(t, d.path))
	before := time.Now().Add(-time.Second)

	steps := []struct {
		typ, code uint16
		raw       int32
		want      control.Event
	}{
		{evKey, 30, 1, control.Event{Code: control.Key(30), Value: 1}},
		{evKey, 30, 0, control.Event{Code: control.Key(30), Value: 0}},
		{evAbs, 0, 100, control.Event{Code: control.Axis(0), Value: 1}},
		{evAbs, 0, -100, control.Event{Code: control.Axis(0), Value: 0}},
		{evAbs, 0, 0, control.Event{Code: control.Axis(0), Value: 0.5}},
		{evAbs, 2, 255, control.Event{Code: control.Axis(2), Value: 1}},
	}
	for _, s := range steps {
		d.emit(s.typ, s.code, s.raw)
		got := r.next(t)
		if got.Code != s.want.Code || got.Value != s.want.Value {
			t.Errorf("after %v = %d: got %v = %v, want %v = %v", s.want.Code, s.raw, got.Code, got.Value, s.want.Code, s.want.Value)
		}
		if got.Time.Before(before) || got.Time.After(time.Now().Add(time.Second)) {
			t.Errorf("event time %v is not the current time", got.Time)
		}
	}
}

// While one reader has grabbed the device, no other reader sees its events.
func TestNodeGrab(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	grabber, other := openNode(t, d.path), openNode(t, d.path)
	grabbed, others := startReader(grabber), startReader(other)

	if err := grabber.Grab(); err != nil {
		t.Fatal(err)
	}
	d.emit(evKey, 30, 1)
	if got := grabbed.next(t); got.Code != control.Key(30) {
		t.Errorf("the grabbing reader received %v", got)
	}
	if !others.quiet() {
		t.Error("another reader received an event from a grabbed device")
	}

	if err := grabber.Release(); err != nil {
		t.Fatal(err)
	}
	d.emit(evKey, 31, 1)
	if got := others.next(t); got.Code != control.Key(31) {
		t.Errorf("after the release the other reader received %v, want KEY 31", got)
	}
}

func TestNodeReportsDisconnect(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	r := startReader(openNode(t, d.path))
	d.destroy()
	select {
	case err := <-r.err:
		if !errors.Is(err, ErrDisconnected) {
			t.Errorf("Read error = %v, want ErrDisconnected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after the device was removed")
	}
}

func TestNodeCloseInterruptsRead(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	n := openNode(t, d.path)
	r := startReader(n)
	time.Sleep(50 * time.Millisecond) // let the reader block
	n.Close()
	select {
	case err := <-r.err:
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("Read error = %v, want os.ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after Close")
	}
}

// When the kernel's buffer overflows, events are lost. The node must then
// read the real state back, so that no key is left stuck.
func TestNodeRecoversFromDroppedEvents(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	n := openNode(t, d.path)

	// Nothing is reading yet. Hold one key, then flood the buffer with
	// another key until the first press is certain to have been dropped.
	const toggles = 20000
	d.emit(evKey, 30, 1)
	for i := 0; i < toggles; i++ {
		d.emit(evKey, 31, 1)
		d.emit(evKey, 31, 0)
	}
	d.emit(evKey, 32, 1)

	r := startReader(n)
	down := map[control.Code]bool{}
	received := 0
	for {
		select {
		case ev := <-r.events:
			down[ev.Code] = ev.Value == 1
			received++
			continue
		case err := <-r.err:
			t.Fatalf("Read failed: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		break
	}

	if received >= 2*toggles {
		t.Fatalf("received %d events; the buffer did not overflow, so the test proved nothing", received)
	}
	want := map[control.Code]bool{control.Key(30): true, control.Key(31): false, control.Key(32): true}
	for code, wantDown := range want {
		if down[code] != wantDown {
			t.Errorf("%v: down = %v after recovery, want %v (received %d events)", code, down[code], wantDown, received)
		}
	}
}

// Grabbing while a key is down must wait for the key to be released, so that
// the rest of the system sees the release and does not believe the key is
// stuck.
func TestNodeGrabWaitsForKeyRelease(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	grabber, other := openNode(t, d.path), openNode(t, d.path)
	grabbed, others := startReader(grabber), startReader(other)

	d.emit(evKey, 30, 1)
	grabbed.next(t)
	others.next(t)

	if err := grabber.Grab(); err != nil {
		t.Fatal(err)
	}
	d.emit(evKey, 30, 0)
	grabbed.next(t)
	if got := others.next(t); got.Code != control.Key(30) || got.Value != 0 {
		t.Errorf("the other reader received %v, want the release of KEY 30", got)
	}

	// With no key down any more, the grab is now in place.
	d.emit(evKey, 31, 1)
	if got := grabbed.next(t); got.Code != control.Key(31) {
		t.Errorf("the grabbing reader received %v, want KEY 31", got)
	}
	if !others.quiet() {
		t.Error("another reader received an event after the grab took effect")
	}
}

// Releasing before a waiting grab took effect must cancel it.
func TestNodeReleaseCancelsWaitingGrab(t *testing.T) {
	d := newVirtualDevice(t, testKeys, nil)
	grabber, other := openNode(t, d.path), openNode(t, d.path)
	grabbed, others := startReader(grabber), startReader(other)

	d.emit(evKey, 30, 1)
	grabbed.next(t)
	others.next(t)
	if err := grabber.Grab(); err != nil {
		t.Fatal(err)
	}
	if err := grabber.Release(); err != nil {
		t.Fatal(err)
	}
	d.emit(evKey, 30, 0)
	d.emit(evKey, 31, 1)
	others.next(t)
	if got := others.next(t); got.Code != control.Key(31) {
		t.Errorf("the other reader received %v, want KEY 31: the cancelled grab took effect", got)
	}
}
