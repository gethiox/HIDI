// Package instrument runs instruments: it joins an input device, the engine
// built from the device's profile, and a MIDI output, and keeps them
// together for as long as the device is connected.
//
// The package contains no platform code. It receives devices and opens
// outputs through the interfaces of the input and output packages.
package instrument

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/light"
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
	// [Manager.SetProfiles] replaces them while the manager runs.
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
	// ToggleTap makes a Toggle of one key work by tapping it: pressed and
	// let go within [TapHold], it turns HIDI off or on. The key plays
	// nothing. Held for longer it does not toggle.
	ToggleTap bool
	// HideToggle keeps the tapped key from the rest of the system, also
	// while HIDI is off, on devices that can do that (see [input.Passer]).
	// Held for longer than [TapHold], the key is passed on to the system,
	// so that it can still do what it normally does.
	HideToggle bool
	// StartInactive makes the manager start turned off.
	StartInactive bool
	// Lights, if set, returns the lights of a keyboard that became an
	// instrument, or nil if it has none to offer.
	Lights func(control.DeviceInfo) Light
	// LogMIDI writes every MIDI message an instrument sends to the log,
	// at debug level. It is for finding faults: an instrument sends
	// hundreds of messages a second.
	LogMIDI bool
	// Changed, if set, is called whenever what [Manager.Status] or
	// [Manager.Active] report has changed. It is called from several
	// goroutines and must return quickly.
	Changed func()
}

// GrabKeyboards is a grab policy that takes keyboards and leaves every
// other device, game controllers in particular, usable by other programs.
func GrabKeyboards(d control.DeviceInfo) bool { return d.Type == control.DeviceKeyboard }

// Manager turns input devices into running instruments.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu       sync.Mutex
	profiles []*config.Profile
	running  map[*runner]struct{} // every connected device, parked ones too
	names    map[string]bool      // the names of MIDI devices in use
	active   bool
	done     sync.WaitGroup
	quit     chan struct{} // closed when Run ends
}

// NewManager returns a manager with no instruments.
func NewManager(opts Options) *Manager {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		opts: opts, log: log, profiles: opts.Profiles,
		running: map[*runner]struct{}{}, active: !opts.StartInactive,
		quit: make(chan struct{}),
	}
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
	m.changed()
}

func (m *Manager) changed() {
	if m.opts.Changed != nil {
		m.opts.Changed()
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
	rates := time.NewTicker(ratePeriod)
	defer rates.Stop()
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
		case <-rates.C:
			m.measure()
		}
	}

	close(m.quit)
	m.mu.Lock()
	for r := range m.running {
		r.device.Close()
	}
	m.mu.Unlock()
	m.done.Wait()
}

// ratePeriod is the time over which the rates in a [Status] are counted.
var ratePeriod = time.Second

// measure works out how much every instrument did since the last time.
func (m *Manager) measure() {
	m.mu.Lock()
	changed := false
	for r := range m.running {
		if r.measure() {
			changed = true
		}
	}
	m.mu.Unlock()
	if changed {
		m.changed()
	}
}

// SetProfiles replaces the available profiles while the manager runs. Every
// connected device is matched again. An instrument whose profile is the
// same as before is not disturbed. One whose profile changed goes silent
// and continues with the new profile on the same MIDI device, so that
// connections made in other programs survive. A device that no profile
// selected before can become an instrument, and an instrument whose
// profile is gone stops.
func (m *Manager) SetProfiles(profiles []*config.Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles = profiles
	for r := range m.running {
		a := m.choose(r.info, r.log)
		if sameProfile(a.profile, r.assigned) {
			continue
		}
		r.assign(a)
	}
}

// sameProfile reports whether two profiles make the same instrument.
func sameProfile(a, b *config.Profile) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Digest == b.Digest
}

// assignment is what a device is to be: an instrument with a profile and
// the engine built from it, or, with both nil, nothing.
type assignment struct {
	profile *config.Profile
	engine  *engine.Engine
}

// choose decides what a device is to be with the current profiles. The
// caller holds m.mu.
func (m *Manager) choose(info control.DeviceInfo, log *slog.Logger) assignment {
	profile := config.Select(m.profiles, info)
	if profile == nil || !usable(info, profile) {
		return assignment{}
	}
	eng, err := engine.New(profile.Engine)
	if err != nil {
		log.Error("profile cannot be used", "profile", profile.Name, "source", profile.Source, "error", err)
		return assignment{}
	}
	return assignment{profile: profile, engine: eng}
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

// add takes a device in. It becomes an instrument if a profile selects it.
// Otherwise it is parked: kept open and unused, so that a profile added
// later can still turn it into an instrument.
func (m *Manager) add(dev input.Device) {
	info := dev.Info()
	r := &runner{
		device: dev, info: info, log: m.log.With("device", info.Name),
		open:     m.opts.OpenEndpoint,
		lights:   m.opts.Lights,
		onToggle: m.toggle,
		wake:     make(chan struct{}, 1),
		quit:     m.quit,
		changed:  m.changed,
		logMIDI:  m.opts.LogMIDI,
	}
	m.mu.Lock()
	m.running[r] = struct{}{}
	r.endpointName = m.takeName(EndpointPrefix + info.Name)
	r.rules.Store(m.rulesFor(info))
	r.wanted.Store(m.active)
	r.assign(m.choose(info, r.log))
	m.mu.Unlock()

	m.done.Add(1)
	go func() {
		defer m.done.Done()
		r.run()
		m.mu.Lock()
		delete(m.running, r)
		delete(m.names, r.endpointName)
		m.mu.Unlock()
	}()
}

// TapHold is how long the key of a tapped toggle may be held and still
// count as a tap.
var TapHold = 500 * time.Millisecond

// inputRules is what the settings say about one device.
type inputRules struct {
	grab   bool
	toggle []control.Code
	// tap says that the toggle is one key and works by a tap, and hide
	// that the key is kept from the system.
	tap, hide bool
}

func (m *Manager) rulesFor(info control.DeviceInfo) *inputRules {
	tap := m.opts.ToggleTap && len(m.opts.Toggle) == 1
	return &inputRules{
		grab: m.opts.Grab != nil && m.opts.Grab(info), toggle: m.opts.Toggle,
		tap: tap, hide: tap && m.opts.HideToggle,
	}
}

// SetToggleTap changes whether the toggle works by a tap and whether its
// key is hidden from the system, while the manager runs. See
// [Options.ToggleTap] and [Options.HideToggle].
func (m *Manager) SetToggleTap(tap, hide bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.ToggleTap, m.opts.HideToggle = tap, hide
	for r := range m.running {
		r.rules.Store(m.rulesFor(r.info))
		r.poke()
	}
}

// SetInput replaces the toggle combination and the grab policy while the
// manager runs. Devices that are grabbed and should not be any more are
// released at once, and the other way round.
func (m *Manager) SetInput(toggle []control.Code, grab func(control.DeviceInfo) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.Toggle, m.opts.Grab = toggle, grab
	for r := range m.running {
		r.rules.Store(m.rulesFor(r.info))
		r.poke()
	}
}

// takeName returns a name for a MIDI device that no other device of this
// HIDI has: the name wanted, or, for the second device of a model and
// those after it, the name with a number. Two devices of one name cannot
// be told apart in another program, and Windows wants every device to
// declare an identifier of its own. The caller holds m.mu.
func (m *Manager) takeName(wanted string) string {
	if m.names == nil {
		m.names = map[string]bool{}
	}
	name := wanted
	for n := 2; m.names[name]; n++ {
		name = fmt.Sprintf("%s %d", wanted, n)
	}
	m.names[name] = true
	return name
}

// Status describes a running instrument.
type Status struct {
	// Device is the input device.
	Device control.DeviceInfo
	// Profile is the name of the profile in use.
	Profile string
	// ProfileSource is where that profile comes from: its file, or a name
	// that begins with "built-in/".
	ProfileSource string
	// State is the current state of the instrument.
	State engine.State
	// Input is what the device itself reports, before any profile is
	// applied to it.
	Input Input
	// InputRate is how many events the device reported in the last
	// second, and MIDIRate how many MIDI messages the instrument sent.
	InputRate, MIDIRate int
}

// Status returns the running instruments, in no particular order.
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.running))
	for r := range m.running {
		if status, ok := r.snapshot(); ok {
			out = append(out, status)
		}
	}
	return out
}

// parkedPoll is how often a parked device is looked at. Its input is of no
// interest, so it is taken slowly: a mouse reports a thousand times a
// second, and following that for nothing would keep the processor busy.
// What the device sends faster than this is dropped by the kernel.
var parkedPoll = 500 * time.Millisecond

// runner looks after one connected device. While a profile selects the
// device it is an instrument, with an engine and a MIDI endpoint; otherwise
// it is parked.
type runner struct {
	device input.Device
	info   control.DeviceInfo
	log    *slog.Logger
	open   func(name string) (output.Endpoint, error)

	// rules says whether the device is grabbed while the instrument is on,
	// and which keys turn HIDI off and on. The manager replaces it when
	// the settings change.
	rules atomic.Pointer[inputRules]
	// endpointName is the name of the instrument's MIDI device.
	endpointName string
	// onToggle is what the toggle combination calls.
	onToggle func()
	// lights gives the lights of the device, and heard holds the notes
	// other programs send to the instrument, which the lights show.
	lights func(control.DeviceInfo) Light
	heard  external

	// assigned is the profile the manager last asked for; the manager
	// guards it with its own lock.
	assigned *config.Profile

	// wanted and pending are what the manager asked for: on or off, and a
	// new assignment. The runner's own goroutine applies them, because the
	// engine must not be used from two goroutines; wake tells that
	// goroutine to look.
	wanted  atomic.Bool
	pending atomic.Pointer[assignment]
	wake    chan struct{}
	// quit is closed when the manager stops.
	quit <-chan struct{}
	// changed tells the manager's owner that the status is different.
	changed func()

	// logMIDI says that every message sent goes to the log.
	logMIDI bool
	// seen and sent count the events of the device and the MIDI messages
	// of the instrument since the start; measured is where they stood
	// when the rates were last worked out.
	seen, sent atomic.Uint64
	measured   [2]uint64

	mu         sync.Mutex
	status     Status
	instrument bool // whether status describes a running instrument
	input      inputTracker
}

// measure works out the rates of the last period from the counters and
// reports whether they differ from those before.
func (r *runner) measure() bool {
	now := [2]uint64{r.seen.Load(), r.sent.Load()}
	in, out := int(now[0]-r.measured[0]), int(now[1]-r.measured[1])
	r.measured = now
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.instrument || (r.status.InputRate == in && r.status.MIDIRate == out) {
		return false
	}
	r.status.InputRate, r.status.MIDIRate = in, out
	return true
}

// snapshot returns the status of the instrument, if the device is one.
func (r *runner) snapshot() (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.status
	if r.instrument {
		status.Input = r.input.snapshot()
	}
	return status, r.instrument
}

// saw records an event of the device for the view of its raw input.
func (r *runner) saw(ev control.Event) {
	r.seen.Add(1)
	r.mu.Lock()
	changed := r.input.record(ev)
	r.mu.Unlock()
	if changed {
		r.changed()
	}
}

func (r *runner) setStatus(status Status, instrument bool) {
	r.mu.Lock()
	r.status, r.instrument = status, instrument
	r.mu.Unlock()
	r.changed()
}

// poke tells the runner's goroutine to look at what the manager asked for.
func (r *runner) poke() {
	select {
	case r.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// setWanted asks the instrument to turn on or off.
func (r *runner) setWanted(active bool) {
	r.wanted.Store(active)
	r.poke()
}

// assign asks the runner to become what a describes. The caller holds the
// manager's lock.
func (r *runner) assign(a assignment) {
	r.assigned = a.profile
	r.pending.Store(&a)
	r.poke()
}

// run feeds the device's input through the engine to the endpoint until the
// device stops, then leaves the instrument silent and removes its endpoint.
func (r *runner) run() {
	var (
		eng      *engine.Engine  // nil while parked
		endpoint output.Endpoint // nil while parked
		profile  *config.Profile
		state    engine.State
		out      []midi.Message
		active   = false // the instrument is on, and grabbed if it should be
		failed   = false // the endpoint broke; waiting for the device to stop

		// limit thins out axis input; due collects what it releases, and
		// release fires when it next has something.
		limit = newLimiter(0)
		// steady drops the trembling of resting axes.
		steady  = newSteadier(0)
		due     []control.Event
		release <-chan time.Time
	)
	// held tracks which keys of the toggle combination are down.
	held := map[control.Code]bool{}

	fail := func() {
		// Without an output there is no instrument. Stopping the device
		// ends the loop below.
		failed = true
		go r.device.Close()
	}
	// rules are the input rules in force, and hold says how the device is
	// held now. regrab makes that fit the rules and the state.
	rules := r.rules.Load()
	const (
		holdFree    = iota // the system has the device
		holdGrabbed        // only HIDI has it
		holdPassing        // the system has it, except for the toggle key
	)
	hold := holdFree
	passer, _ := r.device.(input.Passer)
	cannotPass := false // passing was tried and failed; it is not tried again
	regrab := func() {
		want := holdFree
		switch {
		case eng == nil || !rules.grab:
		case active:
			want = holdGrabbed
		case rules.hide && passer != nil && !cannotPass:
			want = holdPassing
		}
		if want == hold {
			return
		}
		if want == holdPassing {
			if err := passer.Pass(rules.toggle); err != nil {
				cannotPass = true
				r.log.Warn("the toggle key cannot be hidden from the system on this device", "error", err)
				want = holdFree
				if hold == holdFree {
					return
				}
			}
		}
		switch want {
		case holdGrabbed:
			if err := r.device.Grab(); errors.Is(err, input.ErrGrabbedElsewhere) {
				// Not a fault: the user runs a program that takes the
				// device and passes its input on through one of its own.
				r.log.Info("another program has this device to itself, such as an input remapper; its input reaches HIDI through that program's device", "error", err)
			} else if err != nil {
				r.log.Warn("the device cannot be grabbed; it stays usable by other programs", "error", err)
			}
		case holdFree:
			if err := r.device.Release(); err != nil {
				r.log.Warn("the device cannot be released", "error", err)
			}
		}
		hold = want
	}
	ungrab := func() {
		active = false
		regrab()
	}

	// A tapped toggle: tapDown says its key is down, tapTimer fires when
	// it has been down too long for a tap, and typed says that the key
	// was then passed on to the system, which still awaits its release.
	var (
		tapDown, tapLong, typed bool
		tapTimer                <-chan time.Time
	)
	endTap := func() {
		if typed && passer != nil {
			passer.Type(rules.toggle[0], false)
		}
		tapDown, tapLong, typed, tapTimer = false, false, false, nil
	}
	// tap handles an event of the toggle key and reports whether it was a
	// tap that toggles.
	tap := func(ev control.Event) bool {
		switch {
		case ev.Pressed() && !tapDown:
			tapDown, tapLong, tapTimer = true, false, time.After(TapHold)
		case !ev.Pressed() && tapDown:
			toggles := !tapLong
			endTap()
			return toggles
		}
		return false
	}

	// lamp is the lights of the device while it is an instrument. show
	// makes them fit the instrument; what every key does is worked out
	// anew only for another engine or layout.
	var (
		lamp        Light
		shownFor    *engine.Engine
		shownName   string
		keys        []engine.KeyRole
		at, layouts int
	)
	show := func() {
		if lamp == nil || eng == nil {
			return
		}
		if shownFor != eng || shownName != state.Layout {
			keys, at, layouts = eng.Keys()
			shownFor, shownName = eng, state.Layout
		}
		lamp.Show(light.Scene{Active: active, Keys: keys, Layout: at, Layouts: layouts, State: state, External: r.heard.snapshot()})
	}
	dark := func() {
		if lamp != nil {
			lamp.Close()
			lamp = nil
		}
		r.heard.clear()
	}

	// reassign makes the device what the manager last asked for.
	reassign := func() {
		a := r.pending.Swap(nil)
		if a == nil || failed {
			return
		}
		if eng != nil && active && !r.send(endpoint, eng.Reset(out[:0])) {
			fail() // the old profile's notes could not be released
			return
		}
		if a.engine == nil {
			if eng == nil {
				r.log.Debug("device not used", "type", r.info.Type.String(), "hardware", r.info.Hardware.String())
				return
			}
			if active {
				ungrab()
			}
			limit.drop()
			if err := endpoint.Close(); err != nil {
				r.log.Warn("closing the MIDI device failed", "error", err)
			}
			dark()
			eng, endpoint, profile = nil, nil, nil
			clear(held)
			r.setStatus(Status{}, false)
			r.log.Info("instrument stopped", "reason", "no profile selects the device any more")
			return
		}
		if endpoint == nil {
			e, err := r.open(r.endpointName)
			if err != nil {
				r.log.Error("MIDI device cannot be created", "error", err)
				return
			}
			endpoint = e
			if r.lights != nil && r.info.Type == control.DeviceKeyboard {
				lamp = r.lights(r.info)
			}
			if receiver, ok := e.(output.Receiver); ok {
				go r.listen(receiver)
			}
			r.log.Info("instrument started",
				"type", r.info.Type.String(), "hardware", r.info.Hardware.String(),
				"location", r.info.Location, "profile", a.profile.Name, "source", a.profile.Source)
		} else {
			r.log.Info("profile changed", "profile", a.profile.Name, "source", a.profile.Source, "was", profile.Name)
		}
		eng, profile = a.engine, a.profile
		limit = newLimiter(profile.AxisRate)
		steady = newSteadier(profile.AxisThreshold)
		state = eng.State()
		r.setStatus(Status{Device: r.info, Profile: profile.Name, ProfileSource: profile.Source, State: state}, true)
	}

	// apply brings the device to the state the manager wants.
	apply := func() {
		defer show() // whatever changed, the lights follow
		reassign()
		if now := r.rules.Load(); now != rules {
			// Changed settings: another combination starts with no key
			// of it held, and the grab follows at once.
			if tapDown {
				endTap()
			}
			rules = now
			clear(held)
			cannotPass = false
			if hold == holdPassing {
				// The keys to hide may be others now.
				if err := r.device.Release(); err != nil {
					r.log.Warn("the device cannot be released", "error", err)
				}
				hold = holdFree
			}
			regrab()
		}
		want := r.wanted.Load()
		if eng == nil || want == active || failed {
			regrab() // a new instrument, or one that stopped being one
			return
		}
		active = want
		if active {
			regrab()
			return
		}
		// Turning off: nothing may keep sounding, and the device goes
		// back to the system.
		limit.drop() // a position from before must not arrive after
		if !r.send(endpoint, eng.Reset(out[:0])) {
			fail()
		}
		regrab()
		r.publish(eng, &state)
	}

	// play passes one event through the engine to the endpoint.
	play := func(ev control.Event) {
		out = eng.Handle(ev, out[:0])
		if !r.send(endpoint, out) {
			fail()
			return
		}
		r.publish(eng, &state)
		show()
	}
	// flush plays the axis events the limiter held back long enough, and
	// arranges to be called again for those it still holds.
	flush := func() {
		var wait time.Duration
		due, wait = limit.due(time.Now(), due[:0])
		for _, ev := range due {
			if failed {
				break
			}
			play(ev)
		}
		release = nil
		if wait > 0 {
			release = time.After(wait)
		}
	}

	apply()
	events := r.device.Events()
loop:
	for {
		if eng == nil && !failed {
			// Parked: wait for a profile, and notice the device going.
			select {
			case <-r.wake:
				apply()
			case <-r.quit:
				break loop
			case <-time.After(parkedPoll):
				select {
				case _, ok := <-events:
					if !ok {
						break loop
					}
				default:
				}
			}
			continue
		}
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
			if failed || eng == nil {
				continue // draining, or parked a moment ago
			}
			r.saw(ev)
			if rules.tap {
				if ev.Code == rules.toggle[0] {
					if tap(ev) {
						r.onToggle()
						apply()
					}
					continue // the key of a tapped toggle plays nothing
				}
			} else if toggled(rules.toggle, held, ev) {
				r.onToggle()
				apply()
				continue
			}
			if !active {
				continue
			}
			now := time.Now()
			if !steady.admit(ev, now) {
				continue
			}
			if !limit.admit(ev, now) {
				if release == nil {
					flush()
				}
				continue
			}
			play(ev)
		case <-tapTimer:
			// Held too long for a tap: the key does what it normally
			// does, which takes passing it on where it is hidden.
			tapTimer = nil
			if tapDown {
				tapLong = true
				if rules.hide && passer != nil && hold != holdFree {
					typed = passer.Type(rules.toggle[0], true) == nil
				}
			}
		case <-release:
			release = nil
			apply()
			if eng != nil && active && !failed {
				flush()
			}
		}
	}

	if tapDown {
		endTap()
	}
	if eng == nil {
		r.log.Debug("unused device gone", "reason", r.device.Err())
		return
	}
	if !failed && active {
		// Release whatever is still held, so that nothing keeps sounding.
		r.send(endpoint, eng.Reset(out[:0]))
	}
	if err := endpoint.Close(); err != nil {
		r.log.Warn("closing the MIDI device failed", "error", err)
	}
	dark()
	r.setStatus(Status{}, false)
	r.log.Info("instrument stopped", "reason", r.device.Err())
}

// listen takes in what other programs send to the instrument, until its
// MIDI device is closed. The notes show on the lights.
func (r *runner) listen(from output.Receiver) {
	var buf []midi.Message
	for {
		msgs, err := from.Receive(buf[:0])
		if err != nil {
			return
		}
		changed := false
		for _, m := range msgs {
			if r.heard.take(m) {
				changed = true
			}
		}
		if changed {
			r.poke()
		}
		buf = msgs
	}
}

// toggled tracks the keys of the toggle combination and reports whether ev
// is the press that completes it.
func toggled(toggle []control.Code, held map[control.Code]bool, ev control.Event) bool {
	if len(toggle) == 0 || !slices.Contains(toggle, ev.Code) {
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
	return len(held) == len(toggle)
}

// publish records the engine's state for [Manager.Status] and logs a change
// the player made. last is the state published before.
func (r *runner) publish(eng *engine.Engine, last *engine.State) {
	now := eng.State()
	if now == *last {
		return
	}
	changed := now.Octave != last.Octave || now.Transpose != last.Transpose ||
		now.Channel != last.Channel || now.Velocity != last.Velocity || now.Layout != last.Layout
	chord := now.Chord != last.Chord
	*last = now
	r.mu.Lock()
	r.status.State = now
	r.mu.Unlock()
	r.changed()
	if changed {
		r.log.Info("instrument changed",
			"octave", now.Octave, "transpose", now.Transpose,
			"channel", now.Channel+1, "velocity", now.Velocity, "layout", now.Layout)
	}
	switch {
	case chord && now.Chord == "":
		r.log.Info("chord forgotten")
	case chord:
		r.log.Info("chord remembered", "intervals", now.Chord)
	}
}

// send writes messages to the endpoint and reports whether it worked.
func (r *runner) send(endpoint output.Endpoint, msgs []midi.Message) bool {
	if len(msgs) == 0 {
		return true
	}
	if r.logMIDI {
		for _, m := range msgs {
			r.log.Debug("midi", "message", m.String())
		}
	}
	if err := endpoint.Send(msgs); err != nil {
		r.log.Error("sending MIDI failed; stopping the instrument", "error", err)
		return false
	}
	r.sent.Add(uint64(len(msgs)))
	return true
}
