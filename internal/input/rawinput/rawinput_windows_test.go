package rawinput

import (
	"testing"
	"time"
	"unsafe"
)

// The structures must have the sizes Windows expects on 64-bit systems.
func TestStructSizes(t *testing.T) {
	for _, s := range []struct {
		name      string
		got, want uintptr
	}{
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MSG", unsafe.Sizeof(message{}), 48},
		{"RAWINPUTDEVICE", unsafe.Sizeof(rawInputDevice{}), 16},
		{"RAWINPUTHEADER", unsafe.Sizeof(rawInputHeader{}), 24},
		{"RAWKEYBOARD", unsafe.Sizeof(rawKeyboard{}), 16},
		{"RAWINPUTDEVICELIST", unsafe.Sizeof(rawInputDeviceList{}), 16},
		{"KBDLLHOOKSTRUCT", unsafe.Sizeof(kbdLLHook{}), 24},
		{"RAWHID", unsafe.Sizeof(rawHID{}), 8},
		{"RID_DEVICE_INFO", unsafe.Sizeof(ridDeviceInfo{}), 32},
		{"HIDP_CAPS", unsafe.Sizeof(hidpCaps{}), 64},
		{"HIDP_VALUE_CAPS", unsafe.Sizeof(hidpValueCaps{}), 72},
		{"HIDP_VALUE_CAPS.logicalMin", unsafe.Offsetof(hidpValueCaps{}.logicalMin), 40},
		{"HIDP_VALUE_CAPS.usageMin", unsafe.Offsetof(hidpValueCaps{}.usageMin), 56},
		{"HIDP_CAPS.inputValueCaps", unsafe.Offsetof(hidpCaps{}.inputValueCaps), 48},
	} {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

// The watcher must start and stop cleanly, also on a machine without a
// keyboard, and only one may run at a time.
func TestWatchAndClose(t *testing.T) {
	w, err := Watch()
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if _, err := Watch(); err == nil {
		t.Error("a second watcher started")
	}

	// Whatever keyboards the machine has arrive at once; each can be
	// grabbed, released and closed.
	timeout := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case d := <-w.Devices():
			t.Logf("device: %+v", d.Info())
			if d.Info().Name == "" {
				t.Error("a device without a name")
			}
			if err := d.Grab(); err != nil {
				t.Errorf("Grab: %v", err)
			}
			if want := int32(1); !d.controller && w.gate.grabs.Load() != want {
				t.Errorf("%d grabs counted after one", w.gate.grabs.Load())
			}
			d.Release()
			d.Close()
			if d.Err() != ErrClosed {
				t.Errorf("Err after Close = %v", d.Err())
			}
			if err := d.Grab(); err == nil {
				t.Error("a closed device was grabbed")
			}
			if w.gate.grabs.Load() != 0 {
				t.Errorf("%d grabs left", w.gate.grabs.Load())
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
	if _, open := <-w.Devices(); open {
		// Devices that arrived late are fine; the channel must end.
		for range w.Devices() {
		}
	}
	// A new watcher can start once the old one is gone.
	again, err := Watch()
	if err != nil {
		t.Fatalf("Watch after Close: %v", err)
	}
	again.Close()
}
