//go:build linux

package evdev

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/control"
)

// Requests on /dev/uinput, from linux/uinput.h. The kernel's uinput driver
// lets a program create an input device, which is how these tests get a
// keyboard and a game controller to read from.
var (
	uiDevCreate  = ioc(0, 'U', 1, 0)
	uiDevDestroy = ioc(0, 'U', 2, 0)
	uiDevSetup   = ioc(iocWrite, 'U', 3, 92) // struct uinput_setup
	uiAbsSetup   = ioc(iocWrite, 'U', 4, 28) // struct uinput_abs_setup
	uiSetEvBit   = ioc(iocWrite, 'U', 100, 4)
	uiSetKeyBit  = ioc(iocWrite, 'U', 101, 4)
	uiSetAbsBit  = ioc(iocWrite, 'U', 103, 4)
	uiSetPhys    = ioc(iocWrite, 'U', 108, uintptr(wordSize))
	uiGetSysname = ioc(iocRead, 'U', 44, 64)
)

// virtualDevice is an input device created through uinput.
type virtualDevice struct {
	t    *testing.T
	fd   int
	path string // the /dev/input/event* node of the device
}

const (
	testName    = "HIDI test device"
	testPhys    = "hidi-test/input0"
	testVendor  = 0x1234
	testProduct = 0x5678
	testVersion = 0x0111
	busVirtual  = 0x06
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
	keys, axes := spec.keys, spec.axes
	fd, err := unix.Open("/dev/uinput", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		if os.Getenv("HIDI_REQUIRE_UINPUT") != "" {
			t.Fatalf("HIDI_REQUIRE_UINPUT is set but /dev/uinput cannot be opened: %v", err)
		}
		t.Skipf("uinput is not available: %v", err)
	}
	d := &virtualDevice{t: t, fd: fd}
	t.Cleanup(d.destroy)

	set := func(request uintptr, value uint16) {
		t.Helper()
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(value)); errno != 0 {
			t.Fatalf("uinput ioctl %#x(%d): %v", request, value, errno)
		}
	}
	ptr := func(request uintptr, arg unsafe.Pointer) {
		t.Helper()
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(arg)); errno != 0 {
			t.Fatalf("uinput ioctl %#x: %v", request, errno)
		}
	}

	set(uiSetEvBit, evKey)
	for _, key := range keys {
		set(uiSetKeyBit, key)
	}
	if len(axes) > 0 {
		set(uiSetEvBit, evAbs)
	}
	for axis, r := range axes {
		set(uiSetAbsBit, axis)
		// struct uinput_abs_setup: code, padding, then struct input_absinfo
		// (value, minimum, maximum, fuzz, flat, resolution).
		var setup [28]byte
		native.PutUint16(setup[0:], axis)
		native.PutUint32(setup[8:], uint32(r.min))
		native.PutUint32(setup[12:], uint32(r.max))
		ptr(uiAbsSetup, unsafe.Pointer(&setup))
	}

	phys := append([]byte(spec.phys), 0)
	ptr(uiSetPhys, unsafe.Pointer(&phys[0]))

	// struct uinput_setup: struct input_id, name[80], ff_effects_max.
	var setup [92]byte
	native.PutUint16(setup[0:], busVirtual)
	native.PutUint16(setup[2:], testVendor)
	native.PutUint16(setup[4:], testProduct)
	native.PutUint16(setup[6:], testVersion)
	copy(setup[8:87], spec.name)
	ptr(uiDevSetup, unsafe.Pointer(&setup))
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uiDevCreate, 0); errno != 0 {
		t.Fatalf("creating the uinput device: %v", errno)
	}

	var sysname [64]byte
	ptr(uiGetSysname, unsafe.Pointer(&sysname))
	pattern := filepath.Join("/sys/devices/virtual/input", unix.ByteSliceToString(sysname[:]), "event*")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if matches, _ := filepath.Glob(pattern); len(matches) == 1 {
			d.path = filepath.Join("/dev/input", filepath.Base(matches[0]))
			if _, err := os.Stat(d.path); err == nil {
				return d
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the event node for %s did not appear", pattern)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (d *virtualDevice) destroy() {
	if d.fd < 0 {
		return
	}
	unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), uiDevDestroy, 0)
	unix.Close(d.fd)
	d.fd = -1
}

// emit makes the device report one event, followed by the end of a packet.
func (d *virtualDevice) emit(typ, code uint16, value int32) {
	d.t.Helper()
	for _, raw := range [][]byte{rawEvent(typ, code, value), rawEvent(evSyn, synReport, 0)} {
		if _, err := unix.Write(d.fd, raw); err != nil {
			d.t.Fatalf("writing to uinput: %v", err)
		}
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
