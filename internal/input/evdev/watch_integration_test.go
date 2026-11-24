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
	// The main part has a hat as well: an axis a step to either side of
	// its middle.
	withHat := map[uint16]axisRange{0: {min: 0, max: 255}, 0x10: {min: -1, max: 1}}
	pad := createVirtualDevice(t, virtualSpec{name: "HIDI test pad", phys: "hidi-merge/input0", keys: []uint16{0x130}, axes: withHat})
	touch := createVirtualDevice(t, virtualSpec{name: "HIDI test pad Touchpad", phys: "hidi-merge/input1", keys: []uint16{0x110}, axes: stick})
	w := startWatcher(t)

	d := awaitDevice(t, w, "HIDI test pad")
	if nodes := d.Nodes(); len(nodes) != 2 {
		t.Fatalf("the device has %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if got := d.Info().Type; got != control.DeviceGamepad {
		t.Errorf("Type = %v, want gamepad", got)
	}
	// The device says what it has, part by part, and which axis is a
	// hat's.
	has := map[control.Input]bool{}
	for _, c := range d.Controls() {
		has[c] = true
	}
	for _, want := range []control.Input{
		{Code: control.Key(0x130)}, {Code: control.Axis(0)}, {Code: control.Axis(0x10), Hat: true},
		{Code: control.Key(0x110), Part: "Touchpad"}, {Code: control.Axis(0), Part: "Touchpad"},
	} {
		if !has[want] {
			t.Errorf("the device does not list %+v; it lists %+v", want, d.Controls())
		}
	}
	if len(has) != 5 {
		t.Errorf("the device lists %d controls, want 5: %+v", len(has), d.Controls())
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

// A device that is passed on keeps one key from the system and types the
// others again on a keyboard of HIDI's own, which the watcher leaves alone.
func TestDevicePass(t *testing.T) {
	const keyA, keyB, keyCaps = 30, 48, 58
	board := createVirtualDevice(t, virtualSpec{name: "HIDI test pass", phys: "hidi-pass-test/input0", keys: []uint16{keyA, keyB, keyCaps}})
	w := startWatcher(t)
	d := awaitDevice(t, w, "HIDI test pass")
	direct := startReader(openNode(t, board.path))

	if err := d.Pass([]control.Code{control.Key(keyCaps)}); err != nil {
		t.Fatal(err)
	}
	d.pass.mu.Lock()
	through := startReader(openNode(t, d.pass.out.Path))
	d.pass.mu.Unlock()

	// An ordinary key reaches the system through HIDI's keyboard only.
	board.emit(evKey, keyA, 1)
	board.emit(evKey, keyA, 0)
	awaitEvent(t, d)
	awaitEvent(t, d)
	if got := through.next(t); got.Code != control.Key(keyA) || !got.Pressed() {
		t.Errorf("passed on %v, want KEY_A pressed", got)
	}
	if got := through.next(t); got.Code != control.Key(keyA) || got.Pressed() {
		t.Errorf("passed on %v, want KEY_A released", got)
	}
	if !direct.quiet() {
		t.Error("the device itself still delivers keys to others")
	}

	// The hidden key reaches HIDI and nobody else.
	board.emit(evKey, keyCaps, 1)
	board.emit(evKey, keyCaps, 0)
	if got := awaitEvent(t, d); got.Code != control.Key(keyCaps) {
		t.Errorf("HIDI got %v, want KEY_CAPSLOCK", got)
	}
	awaitEvent(t, d)
	if !through.quiet() || !direct.quiet() {
		t.Error("the hidden key reached the system")
	}

	// Typed by HIDI, it does.
	if err := d.Type(control.Key(keyCaps), true); err != nil {
		t.Fatal(err)
	}
	if got := through.next(t); got.Code != control.Key(keyCaps) || !got.Pressed() {
		t.Errorf("typed %v, want KEY_CAPSLOCK pressed", got)
	}
	d.Type(control.Key(keyCaps), false)
	through.next(t)

	// A key that is down when the device is grabbed for playing is
	// released for the system, and nothing is passed on after that.
	board.emit(evKey, keyB, 1)
	awaitEvent(t, d)
	through.next(t)
	if err := d.Grab(); err != nil {
		t.Fatal(err)
	}
	if got := through.next(t); got.Code != control.Key(keyB) || got.Pressed() {
		t.Errorf("on the grab the system got %v, want KEY_B released", got)
	}
	board.emit(evKey, keyB, 0)
	board.emit(evKey, keyA, 1)
	awaitEvent(t, d)
	awaitEvent(t, d)
	if !through.quiet() || !direct.quiet() {
		t.Error("keys reached the system while the device was grabbed")
	}
	board.emit(evKey, keyA, 0)
	awaitEvent(t, d)

	// Released, the system has the device itself again.
	if err := d.Release(); err != nil {
		t.Fatal(err)
	}
	board.emit(evKey, keyA, 1)
	if got := direct.next(t); got.Code != control.Key(keyA) {
		t.Errorf("after the release the device delivered %v", got)
	}

	// HIDI's own keyboard never became a device of the watcher.
	select {
	case other := <-w.Devices():
		if other.Info().Name == "HIDI test pass (through HIDI)" {
			t.Error("the watcher delivered the keyboard HIDI made to pass keys on")
		}
		other.Close()
	case <-time.After(300 * time.Millisecond):
	}
}

// A shared device is held from the system like a grabbed one, except for
// the open keys, which are typed again on a keyboard of HIDI's own.
func TestDeviceShare(t *testing.T) {
	const keyA, keyB, keySpace = 30, 48, 57
	board := createVirtualDevice(t, virtualSpec{name: "HIDI test share", phys: "hidi-share-test/input0", keys: []uint16{keyA, keyB, keySpace}})
	w := startWatcher(t)
	d := awaitDevice(t, w, "HIDI test share")
	direct := startReader(openNode(t, board.path))

	// A key the device does not have is no reason to fail.
	if err := d.Share([]control.Code{control.Key(keySpace), control.Key(200)}); err != nil {
		t.Fatal(err)
	}
	d.pass.mu.Lock()
	through := startReader(openNode(t, d.pass.out.Path))
	d.pass.mu.Unlock()

	// An ordinary key reaches HIDI and nobody else.
	board.emit(evKey, keyA, 1)
	board.emit(evKey, keyA, 0)
	awaitEvent(t, d)
	awaitEvent(t, d)
	if !through.quiet() || !direct.quiet() {
		t.Error("a key that is not open reached the system")
	}

	// The open key reaches both, the system through HIDI's keyboard.
	board.emit(evKey, keySpace, 1)
	if got := awaitEvent(t, d); got.Code != control.Key(keySpace) || !got.Pressed() {
		t.Errorf("HIDI got %v, want KEY_SPACE pressed", got)
	}
	if got := through.next(t); got.Code != control.Key(keySpace) || !got.Pressed() {
		t.Errorf("passed on %v, want KEY_SPACE pressed", got)
	}
	if !direct.quiet() {
		t.Error("the device itself still delivers keys to others")
	}

	// Shared anew without it while it is down: the system gets its
	// release, and nothing of it after that.
	if err := d.Share([]control.Code{control.Key(keyB)}); err != nil {
		t.Fatal(err)
	}
	if got := through.next(t); got.Code != control.Key(keySpace) || got.Pressed() {
		t.Errorf("on the change the system got %v, want KEY_SPACE released", got)
	}
	board.emit(evKey, keySpace, 0)
	awaitEvent(t, d)
	board.emit(evKey, keyB, 1)
	awaitEvent(t, d)
	if got := through.next(t); got.Code != control.Key(keyB) || !got.Pressed() {
		t.Errorf("passed on %v, want KEY_B pressed", got)
	}

	// A plain grab ends the sharing and lets go of what was passed on.
	if err := d.Grab(); err != nil {
		t.Fatal(err)
	}
	if got := through.next(t); got.Code != control.Key(keyB) || got.Pressed() {
		t.Errorf("on the grab the system got %v, want KEY_B released", got)
	}
	board.emit(evKey, keyB, 0)
	awaitEvent(t, d)
	if !through.quiet() || !direct.quiet() {
		t.Error("keys reached the system while the device was grabbed")
	}
}
