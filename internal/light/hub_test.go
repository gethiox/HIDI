package light

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
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
	syncs   int
	slow    time.Duration
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

func (s *fakeServer) Sync(index int) error {
	s.mu.Lock()
	slow := s.slow
	s.mu.Unlock()
	time.Sleep(slow) // the keyboard is being written
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs++
	return s.fail
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
		Interval: time.Millisecond, Retry: 5 * time.Millisecond, Repeats: []time.Duration{},
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
		return len(got) == 4 && got[1] == rgb(c.Panic)
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
		return got[1] == rgb(c.Panic)
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

// startHub runs a hub with one keyboard on a server that is there.
func startHub(t *testing.T, opts Options) (*fakeServer, *Lamp, *Hub) {
	t.Helper()
	server := &fakeServer{}
	opts.Connect = func() (Server, error) { return server, nil }
	opts.Matches = func(location string, info control.DeviceInfo) bool { return location == "keyboard" }
	opts.Interval, opts.Retry = time.Millisecond, 5*time.Millisecond
	hub := NewHub(opts)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { hub.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return server, hub.Attach(control.DeviceInfo{Name: "Kbd"}), hub
}

// What was sent last is sent again a few times, for servers that lose an
// update which arrives while the keyboard is written.
func TestHubRepeatsTheLastColours(t *testing.T) {
	server, lamp, _ := startHub(t, Options{Repeats: []time.Duration{5 * time.Millisecond, 15 * time.Millisecond}})
	lamp.Show(Scene{Active: true})
	eventually(t, "the scene and its two repeats", func() bool {
		_, sends := server.lights()
		return sends == 3
	})
	time.Sleep(40 * time.Millisecond)
	if _, sends := server.lights(); sends != 3 {
		t.Errorf("%d sends, want the scene and two repeats", sends)
	}
	server.mu.Lock()
	syncs := server.syncs
	server.mu.Unlock()
	if syncs != 3 {
		t.Errorf("%d waits for the server, want one after every send", syncs)
	}
}

// A keyboard that is slow to write does not get every scene, and never an
// old one late: while it is busy the scenes replace each other, and the
// newest is sent when it is done.
func TestHubKeepsToThePaceOfTheKeyboard(t *testing.T) {
	c := DefaultColours()
	server, lamp, _ := startHub(t, Options{Repeats: []time.Duration{}})
	keyA := control.Key(30)
	lamp.Show(Scene{Active: true})
	eventually(t, "the first scene", func() bool { _, sends := server.lights(); return sends == 1 })
	server.mu.Lock()
	server.slow = 30 * time.Millisecond
	server.mu.Unlock()

	// Two hundred changes in far less time than the keyboard takes for a
	// few; the last one is a held key.
	for i := range 200 {
		s := Scene{Active: false}
		if i%2 == 1 {
			s.Pressed = []control.Code{keyA}
		}
		lamp.Show(s)
		time.Sleep(200 * time.Microsecond)
	}
	eventually(t, "the last scene on the keyboard", func() bool {
		got, _ := server.lights()
		return len(got) == 4 && got[0] == openrgb.RGB(c.Pressed)
	})
	if _, sends := server.lights(); sends > 12 {
		t.Errorf("%d scenes were sent to a keyboard that takes 30 ms for each", sends)
	}
}

func TestHubTellsWhatIsShown(t *testing.T) {
	c := DefaultColours()
	var changes atomic.Int32
	server, lamp, hub := startHub(t, Options{Repeats: []time.Duration{}, Changed: func() { changes.Add(1) }})
	keyA := control.Key(30)
	if _, _, lit := lamp.Painted(); lit {
		t.Error("a lamp is lit before anything was sent")
	}
	lamp.Show(Scene{Active: true, Layouts: 1, Keys: []engine.KeyRole{{Code: keyA, Action: engine.Passthrough{}}}})
	eventually(t, "the lamp to tell what it shows", func() bool {
		keys, rest, lit := lamp.Painted()
		return lit && keys[keyA] == c.Passthrough && rest == c.Unbound
	})
	if changes.Load() == 0 {
		t.Error("nobody was told that the lamp shows something")
	}

	// Other colours reach the keyboard without a new scene.
	c.Passthrough = RGB{9, 8, 7}
	hub.SetColours(c)
	eventually(t, "the new colour", func() bool {
		got, _ := server.lights()
		keys, _, _ := lamp.Painted()
		return len(got) == 4 && got[0] == openrgb.RGB{R: 9, G: 8, B: 7} && keys[keyA] == c.Passthrough
	})

	lamp.Close()
	eventually(t, "the closed lamp to be dark for the window", func() bool {
		_, _, lit := lamp.Painted()
		return !lit
	})
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

// The hub is turned off and on while it runs: off, the keyboard is left
// evenly lit and the server let go of; on again, the lamp shows its scene.
func TestHubTurnedOffAndOn(t *testing.T) {
	c := DefaultColours()
	var mu sync.Mutex
	var servers []*fakeServer
	opts := Options{Repeats: []time.Duration{}, Disabled: true, Interval: time.Millisecond, Retry: 5 * time.Millisecond}
	opts.Connect = func() (Server, error) {
		mu.Lock()
		defer mu.Unlock()
		servers = append(servers, &fakeServer{})
		return servers[len(servers)-1], nil
	}
	opts.Matches = func(location string, info control.DeviceInfo) bool { return location == "keyboard" }
	hub := NewHub(opts)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { hub.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	connected := func() int { mu.Lock(); defer mu.Unlock(); return len(servers) }
	last := func() *fakeServer { mu.Lock(); defer mu.Unlock(); return servers[len(servers)-1] }

	keyA := control.Key(30)
	lamp := hub.Attach(control.DeviceInfo{Name: "Kbd"})
	lamp.Show(Scene{Active: true, Layouts: 1, Keys: []engine.KeyRole{{Code: keyA, Action: engine.Note{Note: 60}}}})
	time.Sleep(30 * time.Millisecond)
	if connected() != 0 {
		t.Fatal("a hub that is turned off reached for the server")
	}

	hub.SetEnabled(true)
	eventually(t, "the scene on the keyboard", func() bool {
		if connected() != 1 {
			return false
		}
		got, _ := last().lights()
		_, _, lit := lamp.Painted()
		return len(got) == 4 && got[0] == openrgb.RGB(c.C) && lit
	})

	hub.SetEnabled(false)
	first := last()
	eventually(t, "the keyboard left evenly lit and the server let go of", func() bool {
		got, _ := first.lights()
		first.mu.Lock()
		closed := first.closed
		first.mu.Unlock()
		_, _, lit := lamp.Painted()
		return closed && !lit && len(got) == 4 && got[0] == openrgb.RGB(c.Idle)
	})

	// On again, to a new connection, with the scene the lamp had.
	hub.SetEnabled(true)
	eventually(t, "the scene on the keyboard again", func() bool {
		if connected() != 2 {
			return false
		}
		got, _ := last().lights()
		return len(got) == 4 && got[0] == openrgb.RGB(c.C)
	})
	// Another address: the connection is made anew.
	hub.Reconnect()
	eventually(t, "a third connection with the scene", func() bool {
		if connected() != 3 {
			return false
		}
		got, _ := last().lights()
		return len(got) == 4 && got[0] == openrgb.RGB(c.C)
	})
}

func TestPreviewWithoutServer(t *testing.T) {
	// A lamp of a hub that is turned off still knows what it would show.
	hub := NewHub(Options{Disabled: true})
	lamp := hub.Attach(control.DeviceInfo{Name: "Some Keyboard", Type: control.DeviceKeyboard})
	keys, rest := lamp.Preview()
	if len(keys) != 0 || rest != DefaultColours().Idle {
		t.Errorf("before a scene: %v, %v", keys, rest)
	}
	lamp.Show(Scene{Active: false, Pressed: []control.Code{control.Key(30)}})
	keys, rest = lamp.Preview()
	if keys[control.Key(30)] != DefaultColours().Pressed || rest != DefaultColours().Idle {
		t.Errorf("a key held while off: %v, rest %v", keys, rest)
	}
	lamp.Show(Scene{Active: true})
	if _, rest = lamp.Preview(); rest != DefaultColours().Unbound {
		t.Errorf("while on, the other keys are %v", rest)
	}
	if _, _, lit := lamp.Painted(); lit {
		t.Error("the lamp claims to be lit")
	}
}
