// Package instrument runs instruments: it joins an input device, the engine
// built from the device's profile, and a MIDI output, and keeps them
// together for as long as the device is connected.
//
// The package contains no platform code. It receives devices and opens
// outputs through the interfaces of the input and output packages.
package instrument

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

// EndpointPrefix starts the name of every MIDI device HIDI creates, so that
// they are easy to find in a list of MIDI devices.
const EndpointPrefix = "HIDI "

// Options configures a [Manager].
type Options struct {
	// Profiles are the available instrument profiles. Later entries take
	// precedence over earlier ones that describe a device equally well.
	Profiles []*config.Profile
	// OpenEndpoint creates the MIDI output for an instrument.
	OpenEndpoint func(name string) (output.Endpoint, error)
	// Logger receives what the manager reports. Nil discards it.
	Logger *slog.Logger

	// Grab decides whether a device is taken away from the rest of the
	// system while HIDI is active. Nil grabs nothing.
	Grab func(control.DeviceInfo) bool
	// Toggle is a set of keys that, pressed together on one device, turns
	// HIDI off and on again. Empty disables the combination.
	Toggle []control.Code
	// StartInactive makes the manager start turned off.
	StartInactive bool
}

// GrabKeyboards is a grab policy that takes keyboards and leaves every
// other device, game controllers in particular, usable by other programs.
func GrabKeyboards(d control.DeviceInfo) bool { return d.Type == control.DeviceKeyboard }

// Manager turns input devices into running instruments.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu      sync.Mutex
	running map[*runner]struct{}
	active  bool
	done    sync.WaitGroup
}

// NewManager returns a manager with no instruments.
func NewManager(opts Options) *Manager {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{opts: opts, log: log, running: map[*runner]struct{}{}, active: !opts.StartInactive}
}

// Active reports whether HIDI is turned on.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// SetActive turns HIDI on or off. While it is off, the instruments are
// silent, their devices are given back to the system, and their input is
// only watched for the toggle combination. The MIDI devices stay in place,
// so that connections made in other programs survive.
func (m *Manager) SetActive(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == active {
		return
	}
	m.active = active
	for r := range m.running {
		r.setWanted(active)
	}
	if active {
		m.log.Info("HIDI is on")
	} else {
		m.log.Info("HIDI is off; devices are released")
	}
}

// toggle flips between on and off.
func (m *Manager) toggle() {
	m.SetActive(!m.Active())
}

// Run starts an instrument for every usable device that arrives on devices.
// It returns when ctx is cancelled or devices is closed, after stopping
// every instrument: held notes are released and the MIDI devices are
// removed.
func (m *Manager) Run(ctx context.Context, devices <-chan input.Device) {
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case dev, ok := <-devices:
			if !ok {
				break loop
			}
			m.add(dev)
		}
	}

	m.mu.Lock()
	for r := range m.running {
		r.device.Close()
	}
	m.mu.Unlock()
	m.done.Wait()
}

// usable reports whether a device with this profile should become an
// instrument. Keyboards and game controllers always do. Anything else, a
// mouse or a device HIDI cannot classify, does only if the profile names
// that particular hardware, so that a general profile never takes over a
// device the user did not mean.
func usable(info control.DeviceInfo, p *config.Profile) bool {
	if info.Type == control.DeviceKeyboard || info.Type == control.DeviceGamepad {
		return true
	}
	return p.Match.Vendor != nil || p.Match.Product != nil || p.Match.Serial != ""
}

// add starts an instrument for the device, or closes the device if it is
// not going to be used.
func (m *Manager) add(dev input.Device) {
	info := dev.Info()
	log := m.log.With("device", info.Name)

	profile := config.Select(m.opts.Profiles, info)
	if profile == nil || !usable(info, profile) {
		log.Debug("device ignored", "type", info.Type.String(), "hardware", info.Hardware.String())
		dev.Close()
		return
	}
	eng, err := engine.New(profile.Engine)
	if err != nil {
		log.Error("profile cannot be used", "profile", profile.Name, "error", err)
		dev.Close()
		return
	}
	endpoint, err := m.opts.OpenEndpoint(EndpointPrefix + info.Name)
	if err != nil {
		log.Error("MIDI device cannot be created", "error", err)
		dev.Close()
		return
	}

	r := &runner{
		device: dev, engine: eng, endpoint: endpoint, log: log,
		status:   Status{Device: info, Profile: profile.Name, State: eng.State()},
		grab:     m.opts.Grab != nil && m.opts.Grab(info),
		toggle:   m.opts.Toggle,
		onToggle: m.toggle,
		wake:     make(chan struct{}, 1),
	}
	m.mu.Lock()
	m.running[r] = struct{}{}
	r.setWanted(m.active)
	m.mu.Unlock()
	log.Info("instrument started",
		"type", info.Type.String(), "hardware", info.Hardware.String(),
		"location", info.Location, "profile", profile.Name)

	m.done.Add(1)
	go func() {
		defer m.done.Done()
		r.run()
		m.mu.Lock()
		delete(m.running, r)
		m.mu.Unlock()
	}()
}

// Status describes a running instrument.
type Status struct {
	// Device is the input device.
	Device control.DeviceInfo
	// Profile is the name of the profile in use.
	Profile string
	// State is the current state of the instrument.
	State engine.State
}

// Status returns the running instruments, in no particular order.
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.running))
	for r := range m.running {
		out = append(out, r.snapshot())
	}
	return out
}

// runner is one running instrument.
type runner struct {
	device   input.Device
	engine   *engine.Engine
	endpoint output.Endpoint
	log      *slog.Logger

	// grab says whether the device is grabbed while the instrument is on.
	grab bool
	// toggle is the key combination that turns HIDI off and on, and
	// onToggle is what it calls.
	toggle   []control.Code
	onToggle func()

	// wanted is the state the manager asked for. The instrument's own
	// goroutine applies it, because the engine must not be used from two
	// goroutines; wake tells that goroutine to look.
	wanted atomic.Bool
	wake   chan struct{}

	mu     sync.Mutex
	status Status
}

func (r *runner) snapshot() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// setWanted asks the instrument to turn on or off.
func (r *runner) setWanted(active bool) {
	r.wanted.Store(active)
	select {
	case r.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// run feeds the device's input through the engine to the endpoint until the
// device stops, then leaves the instrument silent and removes its endpoint.
func (r *runner) run() {
	var out []midi.Message
	state := r.engine.State()
	active := false
	failed := false
	// held tracks which keys of the toggle combination are down.
	held := map[control.Code]bool{}

	// apply brings the instrument to the state the manager wants.
	apply := func() {
		want := r.wanted.Load()
		if want == active || failed {
			return
		}
		active = want
		if active {
			if r.grab {
				if err := r.device.Grab(); err != nil {
					r.log.Warn("the device cannot be grabbed; it stays usable by other programs", "error", err)
				}
			}
			return
		}
		// Turning off: nothing may keep sounding, and the device goes
		// back to the system.
		if !r.send(r.engine.Reset(out[:0])) {
			failed = true
			go r.device.Close()
		}
		if r.grab {
			if err := r.device.Release(); err != nil {
				r.log.Warn("the device cannot be released", "error", err)
			}
		}
		r.publish(&state)
	}

	apply()
	events := r.device.Events()
loop:
	for {
		select {
		case <-r.wake:
			apply()
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			// A request from the manager may be waiting behind this
			// event; it comes first.
			apply()
			if failed {
				continue // draining until the device has stopped
			}
			if r.toggled(held, ev) {
				r.onToggle()
				apply()
				continue
			}
			if !active {
				continue
			}
			out = r.engine.Handle(ev, out[:0])
			if !r.send(out) {
				// Without an output there is no instrument. Stopping
				// the device ends this loop.
				failed = true
				go r.device.Close()
				continue
			}
			r.publish(&state)
		}
	}

	if !failed && active {
		// Release whatever is still held, so that nothing keeps sounding.
		r.send(r.engine.Reset(out[:0]))
	}
	if err := r.endpoint.Close(); err != nil {
		r.log.Warn("closing the MIDI device failed", "error", err)
	}
	r.log.Info("instrument stopped", "reason", r.device.Err())
}

// toggled tracks the keys of the toggle combination and reports whether ev
// is the press that completes it.
func (r *runner) toggled(held map[control.Code]bool, ev control.Event) bool {
	if len(r.toggle) == 0 || !slices.Contains(r.toggle, ev.Code) {
		return false
	}
	if !ev.Pressed() {
		delete(held, ev.Code)
		return false
	}
	if held[ev.Code] {
		return false
	}
	held[ev.Code] = true
	return len(held) == len(r.toggle)
}

// publish records the engine's state for [Manager.Status] and logs a change
// the player made. last is the state published before.
func (r *runner) publish(last *engine.State) {
	now := r.engine.State()
	if now == *last {
		return
	}
	changed := now.Octave != last.Octave || now.Transpose != last.Transpose ||
		now.Channel != last.Channel || now.Velocity != last.Velocity || now.Layout != last.Layout
	*last = now
	r.mu.Lock()
	r.status.State = now
	r.mu.Unlock()
	if changed {
		r.log.Info("instrument changed",
			"octave", now.Octave, "transpose", now.Transpose,
			"channel", now.Channel+1, "velocity", now.Velocity, "layout", now.Layout)
	}
}

// send writes messages to the endpoint and reports whether it worked.
func (r *runner) send(msgs []midi.Message) bool {
	if len(msgs) == 0 {
		return true
	}
	if r.log.Enabled(context.Background(), slog.LevelDebug) {
		for _, m := range msgs {
			r.log.Debug("midi", "message", m.String())
		}
	}
	if err := r.endpoint.Send(msgs); err != nil {
		r.log.Error("sending MIDI failed; stopping the instrument", "error", err)
		return false
	}
	return true
}
