package output

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/midi"
)

type fakeConnector struct {
	mu         sync.Mutex
	connected  map[string]int // port ID to number of Connect calls
	connectErr error
	closed     bool
}

func (c *fakeConnector) Send([]midi.Message) error { return nil }

func (c *fakeConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeConnector) Connect(id string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connectErr != nil {
		return false, c.connectErr
	}
	if c.connected == nil {
		c.connected = map[string]int{}
	}
	c.connected[id]++
	return c.connected[id] > 1, nil
}

func (c *fakeConnector) calls(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected[id]
}

// plain is an endpoint that cannot be connected to ports.
type plain struct{}

func (plain) Send([]midi.Message) error { return nil }
func (plain) Close() error              { return nil }

type system struct {
	mu    sync.Mutex
	ports []Port
	err   error
}

func (s *system) list() ([]Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Port(nil), s.ports...), s.err
}

func (s *system) set(err error, ports ...Port) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ports, s.err = ports, err
}

var (
	usb   = Port{ID: "24:0", Name: "USB MIDI Interface: MIDI 1"}
	synth = Port{ID: "128:0", Name: "FluidSynth"}
)

func newTestRouter(s *system, patterns ...string) (*Router, *bytes.Buffer) {
	var log bytes.Buffer
	h := slog.NewTextHandler(&log, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}})
	return NewRouter(s.list, MatchPorts(patterns), slog.New(h)), &log
}

func TestMatchPorts(t *testing.T) {
	match := MatchPorts([]string{"usb midi", "128:0"})
	for port, want := range map[Port]bool{
		usb:                                  true, // by a part of its name, ignoring case
		synth:                                true, // by its ID
		{ID: "129:0", Name: "Other"}:         false,
		{ID: "1128:0", Name: "ID is longer"}: false,
	} {
		if match(port) != want {
			t.Errorf("match(%+v) = %v, want %v", port, !want, want)
		}
	}
	if MatchPorts(nil)(usb) {
		t.Error("no patterns match a port")
	}
}

func TestRouterConnectsChosenPorts(t *testing.T) {
	s := &system{ports: []Port{usb, synth}}
	r, log := newTestRouter(s, "usb")
	c := &fakeConnector{}
	e := r.Add("Keyboard", c)

	if c.calls(usb.ID) != 1 || c.calls(synth.ID) != 0 {
		t.Fatalf("after Add: usb %d, synth %d connections; want 1 and 0", c.calls(usb.ID), c.calls(synth.ID))
	}
	// Looking again connects again, which is harmless, and logs nothing new.
	r.Sync()
	r.Sync()
	if got := strings.Count(log.String(), "connected to MIDI port"); got != 1 {
		t.Errorf("the connection was logged %d times:\n%s", got, log)
	}

	if err := e.Close(); err != nil || !c.closed {
		t.Errorf("Close: %v, closed %v", err, c.closed)
	}
	before := c.calls(usb.ID)
	r.Sync()
	if c.calls(usb.ID) != before {
		t.Error("a closed endpoint is still being connected")
	}
}

func TestRouterFollowsPortsComingAndGoing(t *testing.T) {
	s := &system{}
	r, log := newTestRouter(s, "usb")
	c := &fakeConnector{}
	r.Add("Keyboard", c)
	if c.calls(usb.ID) != 0 {
		t.Fatal("connected to a port that is not there")
	}

	s.set(nil, usb)
	r.Sync()
	if c.calls(usb.ID) != 1 {
		t.Fatal("the port that appeared was not connected")
	}
	s.set(nil)
	r.Sync()
	if !strings.Contains(log.String(), "MIDI port is gone") {
		t.Errorf("the port going was not logged:\n%s", log)
	}
	s.set(nil, usb)
	r.Sync()
	if got := strings.Count(log.String(), "connected to MIDI port"); got != 2 {
		t.Errorf("%d connections logged, want one for each time the port appeared:\n%s", got, log)
	}
}

func TestRouterReportsFailuresOnce(t *testing.T) {
	s := &system{err: errors.New("no sequencer")}
	r, log := newTestRouter(s, "usb")
	c := &fakeConnector{connectErr: errors.New("refused")}
	r.Add("Keyboard", c)
	r.Sync()
	if got := strings.Count(log.String(), "cannot be listed"); got != 1 {
		t.Errorf("the listing failure was logged %d times", got)
	}

	s.set(nil, usb)
	r.Sync()
	r.Sync()
	if got := strings.Count(log.String(), "connecting to the MIDI port failed"); got != 1 {
		t.Errorf("the connection failure was logged %d times:\n%s", got, log)
	}

	// Once it works, it is connected and said so.
	c.mu.Lock()
	c.connectErr = nil
	c.mu.Unlock()
	r.Sync()
	if !strings.Contains(log.String(), "connected to MIDI port") {
		t.Errorf("no connection after the failure went away:\n%s", log)
	}
}

func TestRouterLeavesOtherEndpointsAlone(t *testing.T) {
	r, log := newTestRouter(&system{ports: []Port{usb}}, "usb")
	e := plain{}
	if got := r.Add("Keyboard", e); got != Endpoint(e) {
		t.Error("an endpoint without Connect was wrapped")
	}
	if !strings.Contains(log.String(), "cannot be connected") {
		t.Errorf("nothing was said about it:\n%s", log)
	}
}

func TestRouterRun(t *testing.T) {
	s := &system{}
	r, _ := newTestRouter(s, "usb")
	c := &fakeConnector{}
	r.Add("Keyboard", c)
	done := make(chan struct{})
	go func() {
		r.Run(t.Context(), time.Millisecond)
		close(done)
	}()
	s.set(nil, usb)
	deadline := time.Now().Add(2 * time.Second)
	for c.calls(usb.ID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the running router did not connect a port that appeared")
		}
		time.Sleep(time.Millisecond)
	}
}
