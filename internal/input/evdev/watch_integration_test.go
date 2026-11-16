//go:build linux

package evdev

import (
	"errors"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input"
)

func startWatcher(t *testing.T) *Watcher {
	t.Helper()
	w, err := Watch(WatchOptions{
		Settle:  100 * time.Millisecond,
		Problem: func(path string, err error) { t.Logf("watcher: %s: %v", path, err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// awaitDevice returns the next device with the given name. The machine has
// other input devices; those are closed and skipped.
func awaitDevice(t *testing.T, w *Watcher, name string) *Device {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d, ok := <-w.Devices():
			if !ok {
				t.Fatalf("the watcher closed before delivering %q", name)
			}
			if d.Info().Name == name {
				t.Cleanup(func() { d.Close() })
				return d
			}
			d.Close()
		case <-timeout:
			t.Fatalf("device %q was not found", name)
		}
	}
}

func awaitEvent(t *testing.T, d *Device) control.Event {
	t.Helper()
	select {
	case ev, ok := <-d.Events():
		if !ok {
			t.Fatalf("the device stopped: %v", d.Err())
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
	return control.Event{}
}

// awaitStop waits for the device to stop and returns the reason.
func awaitStop(t *testing.T, d *Device) error {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-d.Events():
			if !ok {
				return d.Err()
			}
		case <-timeout:
			t.Fatal("the device did not stop")
		}
	}
}

// The watcher must report devices that are already connected and devices
// plugged in later.
func TestWatcherFindsPresentAndNewDevices(t *testing.T) {
	present := createVirtualDevice(t, virtualSpec{name: "HIDI test present", phys: "hidi-present/input0", keys: typingNums(40)})
	w := startWatcher(t)

	d := awaitDevice(t, w, "HIDI test present")
	info := d.Info()
	if info.Type != control.DeviceKeyboard || info.Location != "hidi-present" {
		t.Errorf("Info = %+v, want a keyboard at hidi-present", info)
	}
	present.emit(evKey, 30, 1)
	if ev := awaitEvent(t, d); ev.Code != control.Key(30) || ev.Value != 1 || ev.Part != "" {
		t.Errorf("event = %+v, want KEY_A pressed on the main part", ev)
	}

	plugged := createVirtualDevice(t, virtualSpec{name: "HIDI test plugged", phys: "hidi-plugged/input0", keys: typingNums(40)})
	d2 := awaitDevice(t, w, "HIDI test plugged")
	plugged.emit(evKey, 31, 1)
	if ev := awaitEvent(t, d2); ev.Code != control.Key(31) {
		t.Errorf("event from the plugged-in device = %+v, want KEY 31", ev)
	}
}

func typingNums(n uint16) []uint16 {
	nums := make([]uint16, n)
	for i := range nums {
		nums[i] = uint16(i) + 1
	}
	return nums
}

// The nodes of one piece of hardware must arrive as one device, and a node
// that repeats an axis must be a named part.
func TestWatcherMergesNodes(t *testing.T) {
	stick := map[uint16]axisRange{0: {min: 0, max: 255}}
	pad := createVirtualDevice(t, virtualSpec{name: "HIDI test pad", phys: "hidi-merge/input0", keys: []uint16{0x130}, axes: stick})
	touch := createVirtualDevice(t, virtualSpec{name: "HIDI test pad Touchpad", phys: "hidi-merge/input1", keys: []uint16{0x110}, axes: stick})
	w := startWatcher(t)

	d := awaitDevice(t, w, "HIDI test pad")
	if nodes := d.Nodes(); len(nodes) != 2 {
		t.Fatalf("the device has %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if got := d.Info().Type; got != control.DeviceGamepad {
		t.Errorf("Type = %v, want gamepad", got)
	}

	pad.emit(evAbs, 0, 255)
	if ev := awaitEvent(t, d); ev.Code != control.Axis(0) || ev.Value != 1 || ev.Part != "" {
		t.Errorf("stick event = %+v, want ABS_X = 1 on the main part", ev)
	}
	touch.emit(evAbs, 0, 255)
	if ev := awaitEvent(t, d); ev.Code != control.Axis(0) || ev.Part != "Touchpad" {
		t.Errorf("touchpad event = %+v, want ABS_X on part Touchpad", ev)
	}
}

// Unplugging must stop the device, and plugging it in again must deliver it
// as a new device.
func TestWatcherFollowsReplug(t *testing.T) {
	spec := virtualSpec{name: "HIDI test replug", phys: "hidi-replug/input0", keys: typingNums(40)}
	first := createVirtualDevice(t, spec)
	w := startWatcher(t)
	d := awaitDevice(t, w, spec.name)

	first.destroy()
	if err := awaitStop(t, d); !errors.Is(err, ErrDisconnected) {
		t.Errorf("after unplugging Err = %v, want ErrDisconnected", err)
	}

	second := createVirtualDevice(t, spec)
	d2 := awaitDevice(t, w, spec.name)
	second.emit(evKey, 30, 1)
	if ev := awaitEvent(t, d2); ev.Code != control.Key(30) {
		t.Errorf("event after plugging in again = %+v, want KEY_A", ev)
	}
}

func TestDeviceClose(t *testing.T) {
	spec := virtualSpec{name: "HIDI test close", phys: "hidi-close/input0", keys: typingNums(40)}
	createVirtualDevice(t, spec)
	d := awaitDevice(t, startWatcher(t), spec.name)

	if err := d.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := awaitStop(t, d); !errors.Is(err, ErrClosed) {
		t.Errorf("after Close Err = %v, want ErrClosed", err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Grabbing a device must grab every one of its nodes.
func TestDeviceGrab(t *testing.T) {
	stick := map[uint16]axisRange{0: {min: 0, max: 255}}
	pad := createVirtualDevice(t, virtualSpec{name: "HIDI test grab", phys: "hidi-grab/input0", keys: []uint16{0x130, 0x131}, axes: stick})
	touch := createVirtualDevice(t, virtualSpec{name: "HIDI test grab Touchpad", phys: "hidi-grab/input1", keys: []uint16{0x110, 0x111}, axes: stick})
	d := awaitDevice(t, startWatcher(t), "HIDI test grab")

	// Independent readers stand in for the rest of the system.
	others := []*reader{startReader(openNode(t, pad.path)), startReader(openNode(t, touch.path))}

	if err := d.Grab(); err != nil {
		t.Fatal(err)
	}
	pad.emit(evKey, 0x130, 1)
	touch.emit(evKey, 0x110, 1)
	awaitEvent(t, d)
	awaitEvent(t, d)
	for i, other := range others {
		if !other.quiet() {
			t.Errorf("node %d still delivers events to others while the device is grabbed", i+1)
		}
	}

	if err := d.Release(); err != nil {
		t.Fatal(err)
	}
	pad.emit(evKey, 0x131, 1)
	touch.emit(evKey, 0x111, 1)
	if got := others[0].next(t); got.Code != control.Key(0x131) {
		t.Errorf("after the release the first node delivered %v, want BTN 0x131", got)
	}
	if got := others[1].next(t); got.Code != control.Key(0x111) {
		t.Errorf("after the release the second node delivered %v, want BTN 0x111", got)
	}
}

func TestWatcherClose(t *testing.T) {
	spec := virtualSpec{name: "HIDI test watcher close", phys: "hidi-wclose/input0", keys: typingNums(40)}
	createVirtualDevice(t, spec)
	w := startWatcher(t)
	d := awaitDevice(t, w, spec.name)

	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Drain what was still in flight; the channel must then be closed.
	timeout := time.After(2 * time.Second)
	for open := true; open; {
		select {
		case other, ok := <-w.Devices():
			if ok {
				other.Close()
			}
			open = ok
		case <-timeout:
			t.Fatal("Devices was not closed")
		}
	}
	// A delivered device outlives the watcher.
	if err := d.Err(); err != nil {
		t.Errorf("the device stopped with the watcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// An input remapper takes a keyboard for itself, passes its keys on through
// a device of its own that copies the keyboard's identity, and makes further
// devices for what it types and clicks, which all report one identity and
// one made-up location. Each of these is a device of its own with the right
// kind, the keyboard that was taken says so when HIDI wants it, and the one
// that passes it on plays.
func TestWatcherBesideInputRemapper(t *testing.T) {
	keys := typingNums(100)
	source := createVirtualDevice(t, virtualSpec{name: "HIDI test remapped", phys: "hidi-remap/input0", keys: keys})
	taken, err := Open(source.path)
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	if err := taken.Grab(); err != nil {
		t.Fatalf("the stand-in for the remapper cannot take the keyboard: %v", err)
	}
	forwarded := createVirtualDevice(t, virtualSpec{name: "input-remapper HIDI test remapped forwarded", phys: "input-remapper/hidi-remap/input0", keys: keys})
	createVirtualDevice(t, virtualSpec{name: "input-remapper keyboard", phys: "input-remapper", keys: keys})
	createVirtualDevice(t, virtualSpec{name: "input-remapper gamepad", phys: "input-remapper",
		keys: []uint16{0x130, 0x131, 0x133, 0x134}, axes: map[uint16]axisRange{0: {min: -32768, max: 32767}, 1: {min: -32768, max: 32767}}})

	w := startWatcher(t)
	want := map[string]control.DeviceType{
		"HIDI test remapped":                          control.DeviceKeyboard,
		"input-remapper HIDI test remapped forwarded": control.DeviceKeyboard,
		"input-remapper keyboard":                     control.DeviceKeyboard,
		"input-remapper gamepad":                      control.DeviceGamepad,
	}
	found := map[string]*Device{}
	timeout := time.After(5 * time.Second)
	for len(found) < len(want) {
		select {
		case d, ok := <-w.Devices():
			if !ok {
				t.Fatal("the watcher closed")
			}
			if _, wanted := want[d.Info().Name]; !wanted || found[d.Info().Name] != nil {
				d.Close()
				continue
			}
			found[d.Info().Name] = d
			t.Cleanup(func() { d.Close() })
		case <-timeout:
			t.Fatalf("found %d of %d devices; devices of the remapper were taken for one", len(found), len(want))
		}
	}
	for name, kind := range want {
		d := found[name]
		if got := d.Info().Type; got != kind {
			t.Errorf("%s is a %v, want a %v", name, got, kind)
		}
		if n := len(d.Nodes()); n != 1 {
			t.Errorf("%s has %d nodes, want 1", name, n)
		}
	}

	// The keyboard the remapper has cannot be taken, and HIDI can tell why.
	if err := found["HIDI test remapped"].Grab(); !errors.Is(err, input.ErrGrabbedElsewhere) {
		t.Errorf("grabbing the taken keyboard: %v, want the error for a device another program has", err)
	}
	// The device that passes it on can, and plays.
	through := found["input-remapper HIDI test remapped forwarded"]
	if err := through.Grab(); err != nil {
		t.Errorf("grabbing the device that passes the keyboard on: %v", err)
	}
	forwarded.emit(evKey, 30, 1)
	if ev := awaitEvent(t, through); ev.Code != control.Key(30) || ev.Value != 1 {
		t.Errorf("event = %+v, want KEY_A pressed", ev)
	}
}
