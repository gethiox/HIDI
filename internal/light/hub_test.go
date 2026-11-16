package light

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/light/openrgb"
)

// fakeServer is an OpenRGB server with one keyboard and one mouse.
type fakeServer struct {
	mu      sync.Mutex
	custom  []int
	colours map[int][]openrgb.RGB
	sends   int
	fail    error
	closed  bool
}

func (s *fakeServer) Controllers() ([]openrgb.Controller, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	return []openrgb.Controller{
		{Index: 0, Type: 6, Name: "Mouse", Location: "mouse"},
		{Index: 1, Type: openrgb.TypeKeyboard, Name: "Keyboard", Location: "keyboard", LEDs: []string{"Key: A", "Key: S", "Logo", "Key: Escape"}},
	}, nil
}

func (s *fakeServer) SetCustomMode(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.custom = append(s.custom, index)
	return s.fail
}

func (s *fakeServer) SetLEDs(index int, colours []openrgb.RGB) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	if s.colours == nil {
		s.colours = map[int][]openrgb.RGB{}
	}
	s.colours[index] = slices.Clone(colours)
	s.sends++
	return nil
}

func (s *fakeServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeServer) lights() ([]openrgb.RGB, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.colours[1], s.sends
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHub(t *testing.T) {
	c := DefaultColours()
	var mu sync.Mutex
	var server *fakeServer
	available := false
	hub := NewHub(Options{
		Connect: func() (Server, error) {
			mu.Lock()
			defer mu.Unlock()
			if !available {
				return nil, errors.New("connection refused")
			}
			server = &fakeServer{}
			return server, nil
		},
		Matches: func(location string, info control.DeviceInfo) bool {
			return location == "keyboard" && info.Name == "Kbd"
		},
		Interval: time.Millisecond, Retry: 5 * time.Millisecond,
	})
	current := func() *fakeServer {
		mu.Lock()
		defer mu.Unlock()
		return server
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { hub.Run(ctx); close(done) }()

	// The lamp exists before the server does, and lights up when it comes.
	lamp := hub.Attach(control.DeviceInfo{Name: "Kbd"})
	other := hub.Attach(control.DeviceInfo{Name: "No lights"})
	keyA, keyS := control.Key(30), control.Key(31)
	scene := Scene{Active: true, Layouts: 1, Keys: []engine.KeyRole{{Code: keyA, Action: engine.Note{Note: 60}}}}
	lamp.Show(scene)
	other.Show(scene)
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	available = true
	mu.Unlock()

	rgb := func(colour RGB) openrgb.RGB { return openrgb.RGB(colour) }
	want := []openrgb.RGB{rgb(c.C), rgb(c.Unbound), {}, rgb(c.Unbound)}
	eventually(t, "the lights to show the scene", func() bool {
		s := current()
		if s == nil {
			return false
		}
		got, _ := s.lights()
		return slices.Equal(got, want)
	})
	if s := current(); !slices.Equal(s.custom, []int{1}) {
		t.Errorf("custom mode was set for %v, want the keyboard only", s.custom)
	}

	// The same scene again sends nothing; a changed one does.
	_, before := current().lights()
	lamp.Show(scene)
	time.Sleep(20 * time.Millisecond)
	if _, after := current().lights(); after != before {
		t.Errorf("an unchanged scene was sent %d more times", after-before)
	}
	scene.Keys = append(scene.Keys, engine.KeyRole{Code: keyS, Action: engine.CommandPanic})
	lamp.Show(scene)
	eventually(t, "the changed scene", func() bool {
		got, _ := current().lights()
		return len(got) == 4 && got[1] == rgb(red)
	})

	// The server goes away and comes back: the lamp lights up again.
	first := current()
	first.mu.Lock()
	first.fail = errors.New("broken pipe")
	first.mu.Unlock()
	scene.Active = false
	lamp.Show(scene)
	eventually(t, "a new connection with the lights set", func() bool {
		s := current()
		if s == first {
			return false
		}
		got, _ := s.lights()
		return len(got) == 4 && got[0] == rgb(c.Idle) && got[2] == (openrgb.RGB{})
	})
	if !first.closed {
		t.Error("the broken connection was not closed")
	}

	// A closed lamp is left alone; stopping lights the others evenly.
	scene.Active = true
	lamp.Show(scene)
	eventually(t, "the lights to come on again", func() bool {
		got, _ := current().lights()
		return got[1] == rgb(red)
	})
	other.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if got, _ := current().lights(); got[1] != rgb(c.Idle) {
		t.Errorf("after stopping, the lights are %v", got)
	}
}

func TestMatches(t *testing.T) {
	root := t.TempDir()
	old := hidrawRoot
	hidrawRoot = root
	defer func() { hidrawRoot = old }()
	dir := filepath.Join(root, "hidraw3", "device")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "uevent"), []byte("DRIVER=hid-generic\nHID_ID=0003:000005AC:0000024F\nHID_NAME=Keychron K8\nHID_PHYS=usb-0000:00:14.0-2/input1\n"), 0o644)

	keyboard := control.DeviceInfo{Hardware: control.HardwareID{Vendor: 0x05ac, Product: 0x024f}, Location: "usb-0000:00:14.0-2"}
	if !Matches("HID: /dev/hidraw3", keyboard) {
		t.Error("the keyboard does not match its raw HID device")
	}
	twin := keyboard
	twin.Location = "usb-0000:00:14.0-5"
	if Matches("HID: /dev/hidraw3", twin) {
		t.Error("a keyboard of the same model on another port matches")
	}
	otherModel := keyboard
	otherModel.Hardware.Product = 1
	if Matches("HID: /dev/hidraw3", otherModel) {
		t.Error("another model matches")
	}
	if Matches("HID: /dev/hidraw9", keyboard) {
		t.Error("a raw HID device that is not there matches")
	}

	// Windows names the device by a path with the model in it.
	path := `HID: \\?\hid#vid_05ac&pid_024f&mi_01#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	if !Matches(path, keyboard) || Matches(path, otherModel) {
		t.Error("matching by the model in a Windows path is wrong")
	}
	if Matches("I2C: /dev/i2c-1, address 0x49", keyboard) || Matches(path, control.DeviceInfo{}) {
		t.Error("a location without a model, or a device without one, matches")
	}
}
