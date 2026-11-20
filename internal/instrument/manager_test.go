package instrument

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

var errUnplugged = errors.New("unplugged")

func init() { parkedPoll = 2 * time.Millisecond }

// fakeDevice is an input device driven by the test.
type fakeDevice struct {
	info   control.DeviceInfo
	events chan control.Event

	mu      sync.Mutex
	err     error
	closed  bool
	grabbed bool
	grabErr error
}

func newFakeDevice(name string, typ control.DeviceType) *fakeDevice {
	return &fakeDevice{
		info:   control.DeviceInfo{Name: name, Type: typ},
		events: make(chan control.Event, 16),
	}
}

func (d *fakeDevice) Info() control.DeviceInfo     { return d.info }
func (d *fakeDevice) Events() <-chan control.Event { return d.events }

func (d *fakeDevice) Grab() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.grabErr != nil {
		return d.grabErr
	}
	d.grabbed = true
	return nil
}

func (d *fakeDevice) Release() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.grabbed = false
	return nil
}

func (d *fakeDevice) isGrabbed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.grabbed
}

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

// drain waits until the instrument has taken every event sent so far.
func (d *fakeDevice) drain(t *testing.T) {
	t.Helper()
	eventually(t, "the events to be consumed", func() bool { return len(d.events) == 0 })
	time.Sleep(20 * time.Millisecond) // the last one may still be in hand
}

func (d *fakeDevice) set(code control.Code, value float64) {
	d.events <- control.Event{Code: code, Value: value}
}

// fakeEndpoint records what an instrument sends.
type fakeEndpoint struct {
	name string
	// incoming carries what another program sends to the instrument;
	// gone is closed with the endpoint.
	incoming chan []midi.Message
	gone     chan struct{}

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
	if !e.closed {
		close(e.gone)
	}
	e.closed = true
	return nil
}

// Receive implements output.Receiver.
func (e *fakeEndpoint) Receive(out []midi.Message) ([]midi.Message, error) {
	select {
	case msgs := <-e.incoming:
		return append(out, msgs...), nil
	case <-e.gone:
		return out, errors.New("closed")
	}
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
	failures  int // attempts to open an endpoint that failed
}

func (r *rig) openFailures() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures
}

func newRig(t *testing.T, extra ...*config.Profile) *rig {
	t.Helper()
	return newRigWith(t, Options{}, extra...)
}

// newRigWith starts a manager with the given options; the profiles and the
// endpoint factory are filled in.
func newRigWith(t *testing.T, opts Options, extra ...*config.Profile) *rig {
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
	opts.Profiles = append(profiles, extra...)
	opts.OpenEndpoint = func(name string) (output.Endpoint, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.openErr != nil {
			r.failures++
			return nil, r.openErr
		}
		e := &fakeEndpoint{name: name, incoming: make(chan []midi.Message, 8), gone: make(chan struct{})}
		r.endpoints[name] = e
		return e, nil
	}
	r.manager = NewManager(opts)
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
	eventually(r.t, "the instrument "+d.info.Name, func() bool {
		for _, s := range r.manager.Status() {
			if s.Device.Name == d.info.Name {
				return true
			}
		}
		return false
	})
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

// A device no profile selects is parked: it gets no MIDI device and is not
// grabbed, but stays open until HIDI stops or it is unplugged.
func TestUnusableDevicesAreParked(t *testing.T) {
	r := newRigWith(t, Options{Grab: func(control.DeviceInfo) bool { return true }})
	var parked []*fakeDevice
	for _, typ := range []control.DeviceType{control.DeviceMouse, control.DeviceUnknown} {
		d := newFakeDevice("Other "+typ.String(), typ)
		r.devices <- d
		d.set(keyZ, 1)
		parked = append(parked, d)
	}
	// A keyboard plugged in afterwards shows that the others were dealt
	// with.
	r.plug(newFakeDevice("Test Keyboard", control.DeviceKeyboard))
	time.Sleep(5 * parkedPoll)
	if got := r.manager.Status(); len(got) != 1 {
		t.Errorf("Status = %+v, want only the keyboard", got)
	}
	for _, d := range parked {
		if d.wasClosed() || d.isGrabbed() {
			t.Errorf("%s: closed %v, grabbed %v; want neither", d.info.Name, d.wasClosed(), d.isGrabbed())
		}
		if r.endpoint(EndpointPrefix+d.info.Name) != nil {
			t.Errorf("%s got a MIDI device", d.info.Name)
		}
	}

	// Unplugging a parked device is noticed, although nothing reads it
	// eagerly.
	parked[0].stop(errUnplugged)
	eventually(t, "the unplugged device to be forgotten", func() bool {
		r.manager.mu.Lock()
		defer r.manager.mu.Unlock()
		return len(r.manager.running) == 2
	})

	r.stop()
	if !parked[1].wasClosed() {
		t.Error("a parked device was not closed when the manager stopped")
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
	r.plug(newFakeDevice("Test Keyboard", control.DeviceKeyboard)) // other was dealt with
	if r.endpoint(EndpointPrefix+"Something") != nil || len(r.manager.Status()) != 2 {
		t.Errorf("the unrelated device became an instrument: %+v", r.manager.Status())
	}
}

func TestEndpointFailureLeavesNoInstrument(t *testing.T) {
	r := newRig(t)
	r.mu.Lock()
	r.openErr = errors.New("no MIDI")
	r.mu.Unlock()

	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.devices <- kbd
	eventually(t, "the attempt to create the MIDI device", func() bool { return r.openFailures() == 1 })

	// The manager keeps working for the next device.
	r.mu.Lock()
	r.openErr = nil
	r.mu.Unlock()
	second := newFakeDevice("Second Keyboard", control.DeviceKeyboard)
	e := r.plug(second)
	second.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64")
	if got := r.manager.Status(); len(got) != 1 || got[0].Device.Name != "Second Keyboard" {
		t.Errorf("Status = %+v, want only the second keyboard", got)
	}
	if kbd.isGrabbed() {
		t.Error("a device without a MIDI device was grabbed")
	}
}

// An instrument whose MIDI device breaks gives its keys back, tries for a
// new device and plays on with it.
func TestSendFailureRecreatesTheDevice(t *testing.T) {
	old := RetryOutput
	RetryOutput = 20 * time.Millisecond
	t.Cleanup(func() { RetryOutput = old })

	r := newRigWith(t, Options{Grab: GrabKeyboards})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	r.mu.Lock()
	r.openErr = errors.New("the service is away")
	r.mu.Unlock()
	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()

	kbd.set(keyZ, 1)
	eventually(t, "the endpoint to close", func() bool { _, closed := e.state(); return closed })
	eventually(t, "the keyboard to be given back", func() bool { return !kbd.isGrabbed() })
	eventually(t, "a new device to be tried for", func() bool { return r.openFailures() > 1 })
	if kbd.wasClosed() {
		t.Fatal("the keyboard was closed with its MIDI device")
	}
	kbd.set(keyZ, 0)
	kbd.drain(t)

	r.mu.Lock()
	r.openErr = nil
	r.mu.Unlock()
	eventually(t, "the new endpoint", func() bool { return r.endpoint(e.name) != e })
	eventually(t, "the keyboard to be grabbed again", kbd.isGrabbed)
	kbd.set(keyZ, 1)
	r.expectSent(r.endpoint(e.name), "NoteOn ch=1 note=C0 velocity=64")
}

// checkedEndpoint is an endpoint that can tell that it is of no use.
type checkedEndpoint struct {
	*fakeEndpoint
	lost *atomic.Bool
}

func (e checkedEndpoint) Check() error {
	if e.lost.Load() {
		return errors.New("lost")
	}
	return nil
}

// A MIDI device that says it is lost is replaced without anything being
// sent.
func TestLostDeviceIsRecreated(t *testing.T) {
	oldRetry, oldHealth := RetryOutput, healthPeriod
	RetryOutput, healthPeriod = 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { RetryOutput, healthPeriod = oldRetry, oldHealth })

	var (
		mu     sync.Mutex
		opened []checkedEndpoint
	)
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(opened)
	}
	profiles, err := config.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(Options{Profiles: profiles, OpenEndpoint: func(name string) (output.Endpoint, error) {
		e := checkedEndpoint{&fakeEndpoint{name: name, incoming: make(chan []midi.Message, 8), gone: make(chan struct{})}, new(atomic.Bool)}
		mu.Lock()
		opened = append(opened, e)
		mu.Unlock()
		return e, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	devices := make(chan input.Device)
	done := make(chan struct{})
	go func() { m.Run(ctx, devices); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	devices <- kbd
	eventually(t, "the first endpoint", func() bool { return count() == 1 })
	mu.Lock()
	first := opened[0]
	mu.Unlock()
	first.lost.Store(true)
	eventually(t, "the second endpoint", func() bool { return count() == 2 })
	eventually(t, "the first endpoint to close", func() bool { _, closed := first.state(); return closed })
	mu.Lock()
	second := opened[1]
	mu.Unlock()
	kbd.set(keyZ, 1)
	eventually(t, "the note on the new endpoint", func() bool {
		sent, _ := second.state()
		return sent == "NoteOn ch=1 note=C0 velocity=64"
	})
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

var (
	keyCtrl, keyAlt = control.Key(29), control.Key(56)
	toggleKeys      = []control.Code{keyCtrl, keyAlt}
)

const (
	noteOn  = "NoteOn ch=1 note=C0 velocity=64"
	noteOff = "NoteOff ch=1 note=C0 velocity=0"
)

// Keyboards are taken away from the system while HIDI is on; game
// controllers stay usable by other programs.
func TestGrabPolicy(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	pad := newFakeDevice("Test Pad", control.DeviceGamepad)
	r.plug(kbd)
	r.plug(pad)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)

	// The pad's instrument is running; give it the chance to grab wrongly.
	pad.set(control.Key(0x130), 1)
	eventually(t, "the pad to play", func() bool { sent, _ := r.endpoint(EndpointPrefix + "Test Pad").state(); return sent != "" })
	if pad.isGrabbed() {
		t.Error("the game controller was grabbed")
	}
}

func TestNoGrabByDefault(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)
	if kbd.isGrabbed() {
		t.Error("the device was grabbed without a grab policy")
	}
}

// The toggle combination turns HIDI off: notes are released, the device is
// given back, and its input is ignored until the combination turns HIDI on
// again.
func TestToggleCombination(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: toggleKeys})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)

	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)

	kbd.set(keyCtrl, 1)
	if !r.manager.Active() {
		t.Fatal("one key of the combination turned HIDI off")
	}
	kbd.set(keyAlt, 1)
	r.expectSent(e, noteOn+"; "+noteOff)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() })
	if r.manager.Active() {
		t.Error("Active() = true after the combination")
	}

	// While off, playing does nothing, and the MIDI device stays.
	kbd.set(keyAlt, 0)
	kbd.set(keyCtrl, 0)
	kbd.set(keyZ, 0)
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)

	kbd.set(keyAlt, 1)
	kbd.set(keyCtrl, 1) // the order of the keys does not matter
	eventually(t, "HIDI to turn on", r.manager.Active)
	eventually(t, "the keyboard to be grabbed again", kbd.isGrabbed)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn+"; "+noteOff+"; "+noteOn)
	if _, closed := e.state(); closed {
		t.Error("the MIDI device was removed while HIDI was off")
	}
}

// Holding the combination must toggle once, not repeatedly.
func TestToggleFiresOncePerPress(t *testing.T) {
	r := newRigWith(t, Options{Toggle: toggleKeys})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.plug(kbd)

	kbd.set(keyCtrl, 1)
	kbd.set(keyAlt, 1)
	eventually(t, "HIDI to turn off", func() bool { return !r.manager.Active() })
	kbd.set(keyAlt, 1) // a repeated report of a key that is already down
	kbd.set(keyCtrl, 1)
	kbd.set(keyZ, 1) // a marker event processed after the repeats
	kbd.set(keyZ, 0)
	time.Sleep(20 * time.Millisecond)
	if r.manager.Active() {
		t.Error("a repeated key report toggled HIDI again")
	}
}

// The combination on one device turns every instrument off.
func TestToggleIsGlobal(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: toggleKeys})
	a := newFakeDevice("Keyboard A", control.DeviceKeyboard)
	b := newFakeDevice("Keyboard B", control.DeviceKeyboard)
	r.plug(a)
	eb := r.plug(b)
	eventually(t, "keyboard B to be grabbed", b.isGrabbed)
	b.set(keyZ, 1)
	r.expectSent(eb, noteOn)

	// The keys must be down on the same device.
	a.set(keyCtrl, 1)
	b.set(keyAlt, 1)
	b.set(keyAlt, 0)
	a.set(keyAlt, 1)
	r.expectSent(eb, noteOn+"; "+noteOff)
	eventually(t, "keyboard B to be released", func() bool { return !b.isGrabbed() })
}

func TestStartInactive(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, StartInactive: true})
	if r.manager.Active() {
		t.Fatal("Active() = true with StartInactive")
	}
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	kbd.drain(t)

	r.manager.SetActive(true)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn) // nothing from before HIDI was turned on

	r.manager.SetActive(false)
	r.expectSent(e, noteOn+"; "+noteOff)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() })
}

// A device that cannot be grabbed still becomes an instrument.
func TestGrabFailureIsNotFatal(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	kbd.grabErr = errors.New("busy")
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)
}

// Unplugging while HIDI is off must not send anything more.
func TestUnplugWhileInactive(t *testing.T) {
	r := newRigWith(t, Options{Toggle: toggleKeys})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)
	r.manager.SetActive(false)
	r.expectSent(e, noteOn+"; "+noteOff)

	kbd.stop(errUnplugged)
	eventually(t, "the endpoint to close", func() bool { _, closed := e.state(); return closed })
	if sent, _ := e.state(); sent != noteOn+"; "+noteOff {
		t.Errorf("sent after unplugging while off: %s", sent)
	}
}

// profile parses a keyboard profile that plays the given note on Z.
func profile(t *testing.T, name, match, note string) *config.Profile {
	t.Helper()
	p, err := config.Parse([]byte("schema = 1\nname = \"" + name + "\"\n[match]\n" + match +
		"\n[[layout]]\nname = \"Main\"\n[layout.bindings]\nKEY_Z = \"" + note + "\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func builtin(t *testing.T) []*config.Profile {
	t.Helper()
	profiles, err := config.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return profiles
}

// A changed profile takes over a playing instrument: the held note is
// released, the MIDI device stays, and the next press plays the new note.
func TestSetProfilesReplacesProfile(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards}, profile(t, "Mine", `type = "keyboard"`, "C3"))
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")

	r.manager.SetProfiles(append(builtin(t), profile(t, "Mine", `type = "keyboard"`, "D3")))
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100; NoteOff ch=1 note=C3 velocity=0")
	kbd.set(keyZ, 0) // the release of the old press plays nothing
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100; NoteOff ch=1 note=C3 velocity=0; NoteOn ch=1 note=D3 velocity=100")

	if _, closed := e.state(); closed {
		t.Error("the MIDI device was removed for a profile change")
	}
	if !kbd.isGrabbed() || kbd.wasClosed() {
		t.Errorf("the device: grabbed %v, closed %v; want it kept grabbed", kbd.isGrabbed(), kbd.wasClosed())
	}
	if got := r.manager.Status(); len(got) != 1 || got[0].Profile != "Mine" {
		t.Errorf("Status = %+v", got)
	}
}

// Reloading profiles whose content did not change must not interrupt what
// is being played.
func TestSetProfilesLeavesUnchangedInstrumentsAlone(t *testing.T) {
	r := newRig(t, profile(t, "Mine", `type = "keyboard"`, "C3"))
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	pad := newFakeDevice("Pad", control.DeviceGamepad)
	e := r.plug(kbd)
	r.plug(pad)
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")

	// The same content, parsed anew, plus a change that concerns only
	// game controllers.
	r.manager.SetProfiles(append(builtin(t),
		profile(t, "Mine", `type = "keyboard"`, "C3"),
		profile(t, "Pad profile", `type = "gamepad"`, "E3")))
	eventually(t, "the game controller's new profile", func() bool {
		for _, s := range r.manager.Status() {
			if s.Profile == "Pad profile" {
				return true
			}
		}
		return false
	})
	kbd.set(keyZ, 0)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100; NoteOff ch=1 note=C3 velocity=0")
}

// A device nothing selected becomes an instrument when a profile for it
// appears, and stops being one when that profile goes away.
func TestSetProfilesStartsAndStopsInstruments(t *testing.T) {
	r := newRigWith(t, Options{Grab: func(control.DeviceInfo) bool { return true }})
	pedal := newFakeDevice("Pedal", control.DeviceUnknown)
	pedal.info.Hardware = control.HardwareID{Vendor: 0x1234, Product: 1}
	r.devices <- pedal
	r.plug(newFakeDevice("Test Keyboard", control.DeviceKeyboard))
	if len(r.manager.Status()) != 1 {
		t.Fatalf("Status = %+v, want the pedal parked", r.manager.Status())
	}

	r.manager.SetProfiles(append(builtin(t), profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "C3")))
	name := EndpointPrefix + "Pedal"
	eventually(t, "the pedal's MIDI device", func() bool { return r.endpoint(name) != nil })
	e := r.endpoint(name)
	eventually(t, "the pedal to be grabbed", pedal.isGrabbed)
	pedal.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")

	r.manager.SetProfiles(builtin(t))
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100; NoteOff ch=1 note=C3 velocity=0")
	eventually(t, "the pedal's MIDI device to close", func() bool { _, closed := e.state(); return closed })
	eventually(t, "the pedal to be given back", func() bool { return !pedal.isGrabbed() })
	eventually(t, "the pedal to leave the status", func() bool { return len(r.manager.Status()) == 1 })
	if pedal.wasClosed() {
		t.Error("the device was closed; it must stay available for a later profile")
	}

	// And back again, with a new MIDI device.
	r.mu.Lock()
	delete(r.endpoints, name)
	r.mu.Unlock()
	r.manager.SetProfiles(append(builtin(t), profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "D3")))
	eventually(t, "the pedal's second MIDI device", func() bool { return r.endpoint(name) != nil })
	pedal.set(keyZ, 0)
	pedal.set(keyZ, 1)
	r.expectSent(r.endpoint(name), "NoteOn ch=1 note=D3 velocity=100")
}

// While HIDI is off, a profile change is taken up silently and plays once
// HIDI is on again.
func TestSetProfilesWhileInactive(t *testing.T) {
	r := newRig(t, profile(t, "Mine", `type = "keyboard"`, "C3"))
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	r.manager.SetActive(false)
	r.manager.SetProfiles(append(builtin(t), profile(t, "Mine", `type = "keyboard"`, "D3")))
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	kbd.drain(t)
	r.expectSent(e, "")
	r.manager.SetActive(true)
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=D3 velocity=100")
}

// Profiles may change while instruments are being played and devices come
// and go.
func TestSetProfilesConcurrently(t *testing.T) {
	r := newRig(t)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.plug(kbd)
	sets := [][]*config.Profile{
		builtin(t),
		append(builtin(t), profile(t, "A", `type = "keyboard"`, "C3")),
		{}, // nothing selects the keyboard
		append(builtin(t), profile(t, "B", `type = "keyboard"`, "D3")),
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			r.manager.SetProfiles(sets[i%len(sets)])
		}
	})
	wg.Go(func() {
		for i := range 200 {
			kbd.set(keyZ, float64(i%2))
			r.manager.Status()
		}
	})
	wg.Wait()
	r.manager.SetProfiles(sets[1])
	eventually(t, "the last profile to be in use", func() bool {
		got := r.manager.Status()
		return len(got) == 1 && got[0].Profile == "A"
	})
}

// Changed must be called for everything Status and Active report, so that
// an interface can follow without asking again and again.
func TestChangedIsCalled(t *testing.T) {
	var calls atomic.Int64
	r := newRigWith(t, Options{Changed: func() { calls.Add(1) }})
	expectMore := func(what string, do func()) {
		t.Helper()
		before := calls.Load()
		do()
		eventually(t, "a change to be announced after "+what, func() bool { return calls.Load() > before })
	}

	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	var e *fakeEndpoint
	expectMore("an instrument started", func() { e = r.plug(kbd) })
	expectMore("a note", func() {
		kbd.set(keyZ, 1)
		r.expectSent(e, "NoteOn ch=1 note=C0 velocity=64")
	})
	expectMore("an octave change", func() { kbd.set(keyF2, 1) })
	expectMore("turning off", func() { r.manager.SetActive(false) })
	expectMore("unplugging", func() { kbd.stop(errUnplugged) })
}

// A fast-moving axis reaches the MIDI device thinned out, and always with
// its final position.
func TestAxisRateIsLimited(t *testing.T) {
	stick, err := config.Parse([]byte(`
schema = 1
name = "Stick"
[match]
type = "gamepad"
[defaults]
axis_rate = 10
[[layout]]
name = "Main"
[layout.bindings]
ABS_X = { pitch_bend = true, centered = true }
KEY_Z = "C3"
`))
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, stick)
	pad := newFakeDevice("Pad", control.DeviceGamepad)
	e := r.plug(pad)

	// Fifteen positions at once, a key in the middle, ending fully right.
	for i := 1; i <= 15; i++ {
		pad.set(control.Axis(0), 0.5+float64(i)/30)
		if i == 8 {
			pad.set(keyZ, 1)
		}
	}
	var sent string
	eventually(t, "the final position", func() bool {
		sent, _ = e.state()
		return strings.HasSuffix(sent, "PitchBend ch=1 bend=+8191")
	})
	if n := strings.Count(sent, "PitchBend"); n != 2 {
		t.Errorf("%d pitch bends were sent, want the first position and the last:\n%s", n, sent)
	}
	if !strings.Contains(sent, "NoteOn ch=1 note=C3") {
		t.Errorf("the key press did not get through at once:\n%s", sent)
	}

	// Turning off drops a position that is still held back.
	pad.set(control.Axis(0), 0.5)
	pad.set(control.Axis(0), 0.75)
	pad.drain(t)
	r.manager.SetActive(false)
	time.Sleep(250 * time.Millisecond)
	if after, _ := e.state(); strings.Contains(after, "bend=+4095") {
		t.Errorf("a held position arrived after HIDI was turned off:\n%s", after)
	}
}

// Changed settings reach running instruments: the grab follows at once,
// and the new combination is the one that toggles.
func TestSetInput(t *testing.T) {
	r := newRigWith(t, Options{Toggle: toggleKeys})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	pad := newFakeDevice("Test Pad", control.DeviceGamepad)
	e := r.plug(kbd)
	r.plug(pad)
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)
	if kbd.isGrabbed() {
		t.Fatal("grabbed without a grab policy")
	}

	// Grabbing is turned on while HIDI is on.
	r.manager.SetInput(toggleKeys, GrabKeyboards)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	if pad.isGrabbed() {
		t.Error("the game controller was grabbed")
	}
	// A device plugged in afterwards follows the new rules too.
	late := newFakeDevice("Late Keyboard", control.DeviceKeyboard)
	r.plug(late)
	eventually(t, "the late keyboard to be grabbed", late.isGrabbed)

	// And off again.
	r.manager.SetInput(toggleKeys, nil)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() && !late.isGrabbed() })

	// Another combination: the old one does nothing any more, also when
	// one of its keys was down during the change.
	kbd.set(keyCtrl, 1)
	r.manager.SetInput([]control.Code{{Kind: control.KindKey, Num: keyZ.Num + 1}, keyAlt}, nil)
	kbd.drain(t)
	kbd.set(keyAlt, 1)
	kbd.drain(t)
	if !r.manager.Active() {
		t.Fatal("the old combination still turned HIDI off")
	}
	kbd.set(control.Code{Kind: control.KindKey, Num: keyZ.Num + 1}, 1)
	eventually(t, "the new combination to turn HIDI off", func() bool { return !r.manager.Active() })
}

// fakeLight records the scenes it is shown.
type fakeLight struct {
	mu     sync.Mutex
	scene  light.Scene
	shown  int
	closed bool
}

func (l *fakeLight) Show(s light.Scene) {
	l.mu.Lock()
	l.scene = s
	l.shown++
	l.mu.Unlock()
}

func (l *fakeLight) Painted() (map[control.Code]light.RGB, light.RGB, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// What a lamp would tell: one colour for a key that is down while
	// HIDI is off, another for every other key.
	keys := map[control.Code]light.RGB{}
	for _, code := range l.scene.Pressed {
		keys[code] = light.RGB{R: 255}
	}
	return keys, light.RGB{B: 1}, l.shown > 0 && !l.closed
}

func (l *fakeLight) Close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}

func (l *fakeLight) last() (light.Scene, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scene, l.closed
}

// The lights of a keyboard follow its instrument: what the keys do, the
// notes played, the notes another program sends, and HIDI being off.
func TestLights(t *testing.T) {
	var mu sync.Mutex
	lamps := map[string]*fakeLight{}
	r := newRigWith(t, Options{Toggle: toggleKeys, Lights: func(info control.DeviceInfo) Light {
		mu.Lock()
		defer mu.Unlock()
		l := &fakeLight{}
		lamps[info.Name] = l
		return l
	}})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	pad := newFakeDevice("Test Pad", control.DeviceGamepad)
	e := r.plug(kbd)
	r.plug(pad)

	mu.Lock()
	lamp := lamps["Test Keyboard"]
	_, padLit := lamps["Test Pad"]
	mu.Unlock()
	if lamp == nil || padLit {
		t.Fatalf("lights: keyboard %v, game controller %v; want them for the keyboard only", lamp != nil, padLit)
	}
	scene := func() light.Scene { s, _ := lamp.last(); return s }
	has := func(set [2]uint64, note int) bool { return set[note/64]&(1<<(note%64)) != 0 }

	eventually(t, "the first scene", func() bool { s := scene(); return s.Active && len(s.Keys) > 20 && s.Layouts > 0 })

	// A note played here.
	kbd.set(keyZ, 1)
	r.expectSent(e, noteOn)
	eventually(t, "the played note to show", func() bool { return scene().State.Sounding == 1 })

	// Notes from another program, and their end.
	e.incoming <- []midi.Message{midi.NoteOn(2, 64, midi.From7(100)), midi.NoteOn(2, 67, midi.From7(100))}
	eventually(t, "the notes from outside to show", func() bool {
		s := scene()
		return has(s.External[2], 64) && has(s.External[2], 67)
	})
	e.incoming <- []midi.Message{midi.NoteOff(2, 64, 0)}
	eventually(t, "one of them to end", func() bool { s := scene(); return !has(s.External[2], 64) && has(s.External[2], 67) })
	e.incoming <- []midi.Message{midi.ControlChange(2, 123, 0)}
	eventually(t, "all notes off to end the other", func() bool { return scene().External[2] == [2]uint64{} })

	// A layout switch shows other keys.
	before := scene().Layout
	kbd.set(control.Key(88), 1) // F12: the next layout
	kbd.set(control.Key(88), 0)
	eventually(t, "the next layout to show", func() bool { return scene().Layout == before+1 })

	// HIDI turned off.
	r.manager.SetActive(false)
	eventually(t, "the lights to show that HIDI is off", func() bool { return !scene().Active })

	// Unplugged.
	kbd.stop(errors.New("unplugged"))
	eventually(t, "the lights to be let go", func() bool { _, closed := lamp.last(); return closed })
}

func TestExternalNotes(t *testing.T) {
	var x external
	steps := []struct {
		m    midi.Message
		want bool
	}{
		{midi.NoteOn(0, 60, midi.From7(1)), true},
		{midi.NoteOn(0, 60, midi.From7(90)), false},                        // on already
		{midi.Message{Kind: midi.KindNoteOn, Channel: 0, Index: 60}, true}, // no force: ends it
		{midi.NoteOff(0, 60, 0), false},
		{midi.NoteOn(5, 127, midi.From7(90)), true},
		{midi.ControlChange(5, 7, 0), false},
		{midi.ControlChange(5, 120, 0), true},
		{midi.PitchBend(5, midi.ValueCenter), false},
	}
	for i, step := range steps {
		if got := x.take(step.m); got != step.want {
			t.Errorf("step %d (%v): changed = %v, want %v", i, step.m, got, step.want)
		}
	}
	if x.snapshot() != ([16][2]uint64{}) {
		t.Errorf("notes left: %v", x.snapshot())
	}
}

// Two devices of one name get MIDI devices of different names, and a name
// that was given back can be used again.
func TestDistinctEndpointNames(t *testing.T) {
	r := newRig(t)
	first := newFakeDevice("Twin Keyboard", control.DeviceKeyboard)
	second := newFakeDevice("Twin Keyboard", control.DeviceKeyboard)
	r.devices <- first
	eventually(t, "the first instrument", func() bool { return r.endpoint(EndpointPrefix+"Twin Keyboard") != nil })
	r.devices <- second
	eventually(t, "the second instrument under a name of its own", func() bool {
		return r.endpoint(EndpointPrefix+"Twin Keyboard 2") != nil
	})

	second.set(keyZ, 1)
	r.expectSent(r.endpoint(EndpointPrefix+"Twin Keyboard 2"), noteOn)
	if sent, _ := r.endpoint(EndpointPrefix + "Twin Keyboard").state(); sent != "" {
		t.Errorf("the first twin played what the second was asked to: %s", sent)
	}

	// The first one leaves; a third device takes the free name.
	first.stop(errors.New("unplugged"))
	eventually(t, "the first instrument to stop", func() bool {
		_, closed := r.endpoint(EndpointPrefix + "Twin Keyboard").state()
		return closed
	})
	m := r.manager
	eventually(t, "the name to be free", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return !m.names[EndpointPrefix+"Twin Keyboard"]
	})
}

// A lever that wavers at rest sends once; moved clearly, it sends again.
func TestRestingAxisIsQuiet(t *testing.T) {
	lever, err := config.Parse([]byte(`
schema = 1
name = "Lever"
[match]
type = "gamepad"
[defaults]
axis_rate = 0
[[layout]]
name = "Main"
[layout.bindings]
ABS_Z = { cc = 7 }
`))
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, lever)
	pad := newFakeDevice("Throttle", control.DeviceGamepad)
	e := r.plug(pad)

	for i := range 200 {
		pad.set(control.Axis(2), 0.5+float64(i%2)*0.004)
	}
	pad.set(control.Axis(2), 0.75)
	var sent string
	eventually(t, "the clear movement", func() bool {
		sent, _ = e.state()
		return strings.HasSuffix(sent, "value=95")
	})
	if n := strings.Count(sent, "ControlChange"); n != 2 {
		t.Errorf("%d controller messages were sent, want the first position and the movement:\n%s", n, sent)
	}
}

// The status says how much an instrument did in the last period, and
// single MIDI messages reach the log only when asked for.
func TestRatesAndMIDILog(t *testing.T) {
	old := ratePeriod
	ratePeriod = 20 * time.Millisecond
	t.Cleanup(func() { ratePeriod = old })

	for _, logMIDI := range []bool{false, true} {
		var buf syncBuffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		r := newRigWith(t, Options{Logger: log, LogMIDI: logMIDI})
		kbd := newFakeDevice("Board", control.DeviceKeyboard)
		e := r.plug(kbd)
		kbd.set(keyZ, 1)
		r.expectSent(e, noteOn)
		eventually(t, "a rate in the status", func() bool {
			st := r.manager.Status()
			return len(st) == 1 && st[0].InputRate > 0 && st[0].MIDIRate > 0
		})
		// The trail has the step in which it happened, as a rate of that
		// step alone: one event in a tenth of the period is ten a period.
		eventually(t, "the event in the trail", func() bool {
			st := r.manager.Status()
			if len(st) != 1 || len(st[0].Trail.Input) != TrailLen || len(st[0].Trail.MIDI) != TrailLen || st[0].Trail.Step != ratePeriod/rateSteps {
				return false
			}
			return slices.Max(st[0].Trail.Input) == rateSteps && slices.Max(st[0].Trail.MIDI) == rateSteps
		})
		seq := r.manager.Status()[0].Trail.Seq
		eventually(t, "the rate to fall back to nothing", func() bool {
			st := r.manager.Status()
			return len(st) == 1 && st[0].InputRate == 0 && st[0].MIDIRate == 0
		})
		eventually(t, "the event to leave the trail, which goes on", func() bool {
			tr := r.manager.Status()[0].Trail
			return slices.Max(tr.Input) == 0 && slices.Max(tr.MIDI) == 0 && tr.Seq > seq+TrailLen
		})
		r.stop()
		if got := strings.Contains(buf.String(), "NoteOn"); got != logMIDI {
			t.Errorf("LogMIDI = %v: MIDI message in the log = %v\n%s", logMIDI, got, buf.String())
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// passingDevice is a fake device that can also hide keys from the system.
type passingDevice struct {
	*fakeDevice
	pmu      sync.Mutex
	passing  bool
	hidden   []control.Code
	open     []control.Code // the keys a grab lets through
	shares   int
	shareErr error
	typed    []string
	passErr  error
}

func (d *passingDevice) Pass(hidden []control.Code) error {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	if d.passErr != nil {
		return d.passErr
	}
	d.passing, d.hidden = true, hidden
	d.fakeDevice.Release()
	return nil
}

func (d *passingDevice) Share(open []control.Code) error {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	if d.shareErr != nil {
		return d.shareErr
	}
	d.passing, d.open = false, open
	d.shares++
	return d.fakeDevice.Grab()
}

func (d *passingDevice) openKeys() []control.Code {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	return d.open
}

func (d *passingDevice) Grab() error {
	d.pmu.Lock()
	d.passing, d.open = false, nil
	d.pmu.Unlock()
	return d.fakeDevice.Grab()
}

func (d *passingDevice) Release() error {
	d.pmu.Lock()
	d.passing, d.open = false, nil
	d.pmu.Unlock()
	return d.fakeDevice.Release()
}

func (d *passingDevice) Type(key control.Code, down bool) error {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	d.typed = append(d.typed, fmt.Sprintf("%v=%v", key, down))
	return nil
}

func (d *passingDevice) isPassing() bool {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	return d.passing
}

func (d *passingDevice) typedKeys() string {
	d.pmu.Lock()
	defer d.pmu.Unlock()
	return strings.Join(d.typed, " ")
}

// A toggle of one key that works by a tap: a short press toggles, a long
// one does not, and the key plays nothing.
func TestToggleTap(t *testing.T) {
	old := TapHold
	TapHold = 60 * time.Millisecond
	t.Cleanup(func() { TapHold = old })

	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: []control.Code{keyZ}, ToggleTap: true})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)

	kbd.set(keyZ, 1)
	kbd.drain(t)
	if !r.manager.Active() {
		t.Fatal("the press alone turned HIDI off")
	}
	kbd.set(keyZ, 0)
	eventually(t, "the tap to turn HIDI off", func() bool { return !r.manager.Active() })
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() })

	// Held for longer than a tap: nothing happens.
	kbd.set(keyZ, 1)
	time.Sleep(3 * TapHold)
	kbd.set(keyZ, 0)
	kbd.drain(t)
	if r.manager.Active() {
		t.Error("a long press turned HIDI on")
	}

	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	eventually(t, "the tap to turn HIDI on", r.manager.Active)
	if sent, _ := e.state(); sent != "" {
		t.Errorf("the toggle key played: %s", sent)
	}
}

// With the key hidden, the device is passed on while HIDI is off, and a
// long press reaches the system as a press and a release of the key.
func TestHiddenToggle(t *testing.T) {
	old := TapHold
	TapHold = 60 * time.Millisecond
	t.Cleanup(func() { TapHold = old })

	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: []control.Code{keyZ}, ToggleTap: true, HideToggle: true, StartInactive: true})
	kbd := &passingDevice{fakeDevice: newFakeDevice("Test Keyboard", control.DeviceKeyboard)}
	r.devices <- kbd
	eventually(t, "the keyboard to be passed on while HIDI is off", kbd.isPassing)
	if len(kbd.hidden) != 1 || kbd.hidden[0] != keyZ {
		t.Errorf("hidden keys: %v", kbd.hidden)
	}

	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	eventually(t, "the tap to turn HIDI on", r.manager.Active)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	if kbd.isPassing() {
		t.Error("still passing while grabbed")
	}
	if got := kbd.typedKeys(); got != "" {
		t.Errorf("a tap was passed on to the system: %s", got)
	}

	kbd.set(keyZ, 1)
	eventually(t, "the long press to be passed on", func() bool { return kbd.typedKeys() == keyZ.String()+"=true" })
	kbd.set(keyZ, 0)
	eventually(t, "its release to be passed on", func() bool {
		return kbd.typedKeys() == keyZ.String()+"=true "+keyZ.String()+"=false"
	})
	if !r.manager.Active() {
		t.Error("the long press turned HIDI off")
	}

	// Without the setting the device is simply released while off.
	r.manager.SetToggleTap(true, false)
	r.manager.SetActive(false)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() && !kbd.isPassing() })
}

// A device that cannot hide the key is released while HIDI is off.
func TestHiddenToggleFallsBack(t *testing.T) {
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: []control.Code{keyZ}, ToggleTap: true, HideToggle: true})
	kbd := &passingDevice{fakeDevice: newFakeDevice("Test Keyboard", control.DeviceKeyboard), passErr: errors.New("no uinput")}
	r.devices <- kbd
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)
	r.manager.SetActive(false)
	eventually(t, "the keyboard to be released", func() bool { return !kbd.isGrabbed() })
	if kbd.isPassing() {
		t.Error("passing although it failed")
	}
}
