package tray

import (
	"testing"
	"time"
	"unsafe"
)

func TestStructSizes(t *testing.T) {
	for _, s := range []struct {
		name      string
		got, want uintptr
	}{
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MSG", unsafe.Sizeof(message{}), 48},
		{"NOTIFYICONDATAW", unsafe.Sizeof(notifyIconData{}), 976},
	} {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

// The host must start, take views and stop, also on a machine without a
// notification area, and make icons of the drawing.
func TestWindowsHost(t *testing.T) {
	if icon := newIcon(LookOn, 16); icon == 0 {
		t.Error("no icon was made")
	} else {
		procDestroyIcon.Call(icon)
	}

	host, err := NewHost()
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if _, err := NewHost(); err == nil {
		t.Error("a second icon was made")
	}
	for _, look := range []Look{LookOff, LookOn, LookStopped} {
		if err := host.Show(View{Look: look, Title: "HIDI"}); err != nil {
			t.Errorf("Show: %v", err)
		}
	}
	done := make(chan struct{})
	go func() {
		host.Close()
		host.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	again, err := NewHost()
	if err != nil {
		t.Fatalf("NewHost after Close: %v", err)
	}
	again.Close()
}
