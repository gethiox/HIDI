package xinput

import (
	"testing"
	"time"
	"unsafe"
)

// The structures must have the sizes XInput expects.
func TestStructSizes(t *testing.T) {
	for _, s := range []struct {
		name      string
		got, want uintptr
	}{
		// XINPUT_STATE is 16 bytes: the packet number and the gamepad.
		// Four more follow it here, for the function that is said to
		// write them.
		{"XINPUT_STATE.Gamepad", unsafe.Offsetof(xState{}.pad), 4},
		{"XINPUT_STATE and the room after it", unsafe.Sizeof(xState{}), 20},
		{"XINPUT_GAMEPAD", unsafe.Sizeof(xGamepad{}), 12},
		{"XINPUT_CAPABILITIES_EX", unsafe.Sizeof(xCapabilitiesEx{}), 32},
		{"XINPUT_CAPABILITIES_EX.vendor", unsafe.Offsetof(xCapabilitiesEx{}.vendor), 20},
	} {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

// XInput must load and answer, and the watcher must start and stop, also
// on a machine without a controller.
func TestWatchAndClose(t *testing.T) {
	lib, err := load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for slot := range slots {
		_, ok := lib.state(slot)
		vendor, product := lib.model(slot)
		t.Logf("controller %d: connected %v, model %04x:%04x", slot+1, ok, vendor, product)
	}

	w, err := Watch()
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	timeout := time.After(300 * time.Millisecond)
collect:
	for {
		select {
		case d := <-w.Devices():
			t.Logf("device: %+v", d.Info())
			d.Close()
			if d.Err() != ErrClosed {
				t.Errorf("Err after Close = %v", d.Err())
			}
			select {
			case <-drained(d):
			case <-time.After(2 * time.Second):
				t.Error("the events of a closed device did not end")
			}
		case <-timeout:
			break collect
		}
	}
	done := make(chan struct{})
	go func() {
		w.Close()
		w.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	for range w.Devices() {
	}
}

func drained(d *Device) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range d.Events() {
		}
		close(done)
	}()
	return done
}
