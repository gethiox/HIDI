package instrument

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/light/openrgb"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

type rgbServer struct {
	mu      sync.Mutex
	colours []openrgb.RGB
}

func (s *rgbServer) Controllers() ([]openrgb.Controller, error) {
	return []openrgb.Controller{{Index: 0, Type: openrgb.TypeKeyboard, Name: "Keyboard", Location: "keyboard", LEDs: []string{"Key: Z", "Key: X"}}}, nil
}
func (s *rgbServer) SetCustomMode(int) error { return nil }
func (s *rgbServer) SetLEDs(_ int, c []openrgb.RGB) error {
	s.mu.Lock()
	s.colours = slices.Clone(c)
	s.mu.Unlock()
	return nil
}
func (s *rgbServer) Close() error { return nil }
func (s *rgbServer) get() []openrgb.RGB {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.colours)
}

// Found in review: on the way out the hub is to light every keyboard evenly, but the
// instruments close their lamps on the same signal.
func TestLightsAreLeftEvenOnExit(t *testing.T) {
	idle := openrgb.RGB(light.DefaultColours().Idle)
	left := 0
	const rounds = 300
	for range rounds {
		server := &rgbServer{}
		hub := light.NewHub(light.Options{
			Connect:  func() (light.Server, error) { return server, nil },
			Matches:  func(string, control.DeviceInfo) bool { return true },
			Interval: time.Millisecond,
		})
		ctx, cancel := context.WithCancel(context.Background())
		hubDone := make(chan struct{})
		go func() { hub.Run(ctx); close(hubDone) }()
		m := NewManager(Options{Profiles: builtin(t),
			OpenEndpoint: func(name string) (output.Endpoint, error) {
				return &fakeEndpoint{name: name, incoming: make(chan []midi.Message, 8), gone: make(chan struct{})}, nil
			},
			Lights: func(info control.DeviceInfo) Light { return hub.Attach(info) }})
		devices := make(chan input.Device)
		done := make(chan struct{})
		go func() { m.Run(ctx, devices); close(done) }()
		kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
		devices <- kbd
		eventually(t, "the keys to be lit as an instrument", func() bool {
			c := server.get()
			return len(c) == 2 && c[0] != idle && c[0] != (openrgb.RGB{})
		})
		cancel()
		<-done
		<-hubDone
		if c := server.get(); c[0] != idle {
			left++
		}
	}
	if left > 0 {
		t.Errorf("%d of %d exits left the keys in the colours of a running instrument", left, rounds)
	}
}
