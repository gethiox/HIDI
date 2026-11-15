package instrument

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

var errUnplugged = errors.New("unplugged")

// fakeDevice is an input device driven by the test.
type fakeDevice struct {
	info   control.DeviceInfo
	events chan control.Event

	mu     sync.Mutex
	err    error
	closed bool
}

func newFakeDevice(name string, typ control.DeviceType) *fakeDevice {
	return &fakeDevice{
		info:   control.DeviceInfo{Name: name, Type: typ},
		events: make(chan control.Event, 16),
	}
}

func (d *fakeDevice) Info() control.DeviceInfo     { return d.info }
func (d *fakeDevice) Events() <-chan control.Event { return d.events }
func (d *fakeDevice) Grab() error                  { return nil }
func (d *fakeDevice) Release() error               { return nil }

func (d *fakeDevice) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// stop ends the device for the given reason, once.
func (d *fakeDevice) stop(reason error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err == nil {
		d.err = reason
		close(d.events)
	}
}

func (d *fakeDevice) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.stop(errors.New("closed"))
	return nil
}

func (d *fakeDevice) wasClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func (d *fakeDevice) set(code control.Code, value float64) {
	d.events <- control.Event{Code: code, Value: value}
}

// fakeEndpoint records what an instrument sends.
type fakeEndpoint struct {
	name string

	mu      sync.Mutex
	sent    []string
	closed  bool
	sendErr error
}

func (e *fakeEndpoint) Send(msgs []midi.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sendErr != nil {
		return e.sendErr
	}
	for _, m := range msgs {
		e.sent = append(e.sent, m.String())
	}
	return nil
}

func (e *fakeEndpoint) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

func (e *fakeEndpoint) state() (sent string, closed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.sent, "; "), e.closed
}

// rig runs a manager against fake devices and endpoints.
type rig struct {
	t       *testing.T
	manager *Manager
	devices chan input.Device
	cancel  context.CancelFunc
	stopped chan struct{}

	mu        sync.Mutex
	endpoints map[string]*fakeEndpoint
	openErr   error
}

func newRig(t *testing.T, extra ...*config.Profile) *rig {
	t.Helper()
	profiles, err := config.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{
		t:         t,
		devices:   make(chan input.Device),
		stopped:   make(chan struct{}),
		endpoints: map[string]*fakeEndpoint{},
	}
	r.manager = NewManager(Options{
		Profiles: append(profiles, extra...),
		OpenEndpoint: func(name string) (output.Endpoint, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.openErr != nil {
				return nil, r.openErr
			}
			e := &fakeEndpoint{name: name}
			r.endpoints[name] = e
			return e, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		r.manager.Run(ctx, r.devices)
		close(r.stopped)
	}()
	t.Cleanup(r.stop)
	return r
}

// stop cancels the manager and waits for it to finish.
func (r *rig) stop() {
	r.cancel()
	select {
	case <-r.stopped:
	case <-time.After(2 * time.Second):
		r.t.Error("the manager did not stop")
	}
}

func (r *rig) endpoint(name string) *fakeEndpoint {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.endpoints[name]
}

// eventually waits for a condition that another goroutine brings about.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// plug connects a device and waits until its instrument is running.
func (r *rig) plug(d *fakeDevice) *fakeEndpoint {
	r.t.Helper()
	r.devices <- d
	name := EndpointPrefix + d.info.Name
	eventually(r.t, "the endpoint "+name, func() bool { return r.endpoint(name) != nil })
	return r.endpoint(name)
}

func (r *rig) expectSent(e *fakeEndpoint, want string) {
	r.t.Helper()
	var got string
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, _ = e.state(); got == want {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("endpoint %q received:\n %s\nwant:\n %s", e.name, got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

var (
	keyZ  = control.Key(44) // C0 in the built-in keyboard profile
	keyF2 = control.Key(60) // octave up
)

func TestInstrumentPlays(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)

	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64; NoteOff ch=1 note=C0 velocity=0")
}

// Unplugging a device while keys are held must not leave notes sounding,
// and must remove the MIDI device.
func TestUnplugReleasesNotes(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)

	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64")
	kbd.stop(errUnplugged)
	r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64; NoteOff ch=1 note=C0 velocity=0")
	eventually(t, "the endpoint to close", func() bool { _, closed := e.state(); return closed })
	eventually(t, "the instrument to be forgotten", func() bool { return len(r.manager.Status()) == 0 })
}

// Stopping the manager must silence and remove every instrument.
func TestStopSilencesEverything(t *testing.T) {
	r := newRig(t)
	a := newFakeDevice("Keyboard A", control.DeviceKeyboard)
	b := newFakeDevice("Keyboard B", control.DeviceKeyboard)
	ea, eb := r.plug(a), r.plug(b)
	a.set(keyZ, 1)
	b.set(keyZ, 1)
	r.expectSent(ea, "NoteOn ch=1 note=C0 velocity=64")
	r.expectSent(eb, "NoteOn ch=1 note=C0 velocity=64")

	r.stop()
	for _, e := range []*fakeEndpoint{ea, eb} {
		sent, closed := e.state()
		if sent != "NoteOn ch=1 note=C0 velocity=64; NoteOff ch=1 note=C0 velocity=0" || !closed {
			t.Errorf("endpoint %q: sent %q, closed %v; want the note released and the endpoint closed", e.name, sent, closed)
		}
	}
	if !a.wasClosed() || !b.wasClosed() {
		t.Error("the devices were not closed")
	}
}

// Every device is its own instrument with its own MIDI device.
func TestInstrumentsAreIndependent(t *testing.T) {
	r := newRig(t)
	a := newFakeDevice("Keyboard A", control.DeviceKeyboard)
	b := newFakeDevice("Keyboard B", control.DeviceKeyboard)
	ea, eb := r.plug(a), r.plug(b)

	a.set(keyF2, 1) // octave up on A only
	a.set(keyZ, 1)
	b.set(keyZ, 1)
	r.expectSent(ea, "NoteOn ch=1 note=C1 velocity=64")
	r.expectSent(eb, "NoteOn ch=1 note=C0 velocity=64")
}

func TestStatus(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.plug(kbd)

	status := r.manager.Status()
	if len(status) != 1 || status[0].Device.Name != "Test Keyboard" || status[0].Profile != "Default keyboard" {
		t.Fatalf("Status = %+v", status)
	}
	kbd.set(keyF2, 1)
	eventually(t, "the octave to change", func() bool { return r.manager.Status()[0].State.Octave == 1 })
}

func TestUnusableDevicesAreClosed(t *testing.T) {
	r := newRig(t)
	for _, typ := range []control.DeviceType{control.DeviceMouse, control.DeviceUnknown} {
		d := newFakeDevice("Other "+typ.String(), typ)
		r.devices <- d
		eventually(t, "the "+typ.String()+" device to be closed", d.wasClosed)
	}
	if got := r.manager.Status(); len(got) != 0 {
		t.Errorf("Status = %+v, want no instruments", got)
	}
}

// A device HIDI cannot classify becomes an instrument only when a profile
// names that particular hardware.
func TestSpecificProfileEnablesUnclassifiedDevice(t *testing.T) {
	specific, err := config.Parse([]byte(`
schema = 1
name = "Foot pedal"
[match]
vendor = 0x1234
product = 0x0001
[[layout]]
name = "Main"
[layout.bindings]
KEY_A = "C3"
`))
	if err != nil {
		t.Fatal(err)
	}
	// A profile that selects every device must not have this effect.
	general, err := config.Parse([]byte("schema = 1\nname = \"Anything\"\n[[layout]]\nname = \"Main\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, general, specific)

	pedal := newFakeDevice("Pedal", control.DeviceUnknown)
	pedal.info.Hardware = control.HardwareID{Vendor: 0x1234, Product: 0x0001}
	e := r.plug(pedal)
	pedal.set(control.Key(30), 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")

	other := newFakeDevice("Something", control.DeviceUnknown)
	r.devices <- other
	eventually(t, "the unrelated device to be closed", other.wasClosed)
}

func TestEndpointFailureClosesDevice(t *testing.T) {
	r := newRig(t)
	r.mu.Lock()
	r.openErr = errors.New("no MIDI")
	r.mu.Unlock()

	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.devices <- kbd
	eventually(t, "the device to be closed", kbd.wasClosed)

	// The manager keeps working for the next device.
	r.mu.Lock()
	r.openErr = nil
	r.mu.Unlock()
	second := newFakeDevice("Second Keyboard", control.DeviceKeyboard)
	e := r.plug(second)
	second.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64")
}

func TestSendFailureStopsInstrument(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()

	kbd.set(keyZ, 1)
	eventually(t, "the device to be closed", kbd.wasClosed)
	eventually(t, "the endpoint to close", func() bool { _, closed := e.state(); return closed })
	eventually(t, "the instrument to be forgotten", func() bool { return len(r.manager.Status()) == 0 })
}

// Run must also end when the source of devices is closed.
func TestRunEndsWhenDevicesClose(t *testing.T) {
	r := newRig(t)
	close(r.devices)
	select {
	case <-r.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the device source closed")
	}
}
