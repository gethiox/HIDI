// Package instrument runs instruments: it joins an input device, the engine
// built from the device's profile, and a MIDI output, and keeps them
// together for as long as the device is connected.
//
// The package contains no platform code. It receives devices and opens
// outputs through the interfaces of the input and output packages.
package instrument

import (
	"context"
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
	// HIDI off and on again, whatever the device and its profile. Empty
	// leaves it to the profiles: each device then goes by the toggle of
	// its own ([config.Profile.Toggle]), and one whose profile has none
	// cannot turn HIDI off or on.
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
	// OwnToggle keeps a Toggle of one key from the rest of the system
	// altogether, tapped or not, on devices that can do that: the key is
	// HIDI's alone. Caps Lock as the toggle then never changes the
	// keyboard's state.
	OwnToggle bool
	// StartInactive makes the manager start turned off.
	StartInactive bool
	// Allow decides whether a device may become an instrument at all.
	// Nil allows every device. [Manager.SetAllow] replaces it while the
	// manager runs.
	Allow func(control.DeviceInfo) bool
	// Hysteresis is the hysteresis of axes that act as buttons, as a
	// part of their travel; see [engine.Engine.SetHysteresis]. Rest is
	// what turning HIDI off returns to rest beside the notes: the pitch
	// bend, the controllers, both or neither. [Manager.SetPlaying]
	// changes them.
	Hysteresis float64
	Rest       engine.Rest
	// LearnThreshold is how far an axis has to move to get through in
	// learning mode, as a part of its range. Zero means
	// [engine.DefaultLearnThreshold]. [Manager.SetLearnThreshold] changes
	// it while the manager runs.
	LearnThreshold float64
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
	learning bool
	// simulated are the keyboards whose lights are to be shown though
	// HIDI does not control them, by lightKey.
	simulated map[string]bool
	done      sync.WaitGroup
	quit      chan struct{} // closed when Run ends
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
		m.log.Debug("HIDI is on")
	} else {
		m.log.Debug("HIDI is off; devices are released")
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

// Learning reports whether learning mode is on: see
// [engine.Engine.SetLearning].
func (m *Manager) Learning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.learning
}

// SetLearning turns learning mode on or off, for all instruments at once.
func (m *Manager) SetLearning(on bool) {
	m.mu.Lock()
	if m.learning == on {
		m.mu.Unlock()
		return
	}
	m.learning = on
	for r := range m.running {
		r.setLearning(on, m.opts.LearnThreshold)
	}
	m.mu.Unlock()
	if on {
		m.log.Debug("learning mode is on: axes stand still until they are moved far")
	} else {
		m.log.Debug("learning mode is off")
	}
	m.changed()
}

// SetLearnThreshold changes how far an axis has to move to get through
// in learning mode, as a part of its range.
func (m *Manager) SetLearnThreshold(threshold float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.LearnThreshold = threshold
	for r := range m.running {
		r.setLearning(m.learning, threshold)
	}
}

// toggleLearning is what the learn command of an instrument calls.
func (m *Manager) toggleLearning() {
	m.SetLearning(!m.Learning())
}

// Run starts an instrument for every usable device that arrives on devices.
// It returns when ctx is cancelled or devices is closed, after stopping
// every instrument: held notes are released and the MIDI devices are
// removed.
func (m *Manager) Run(ctx context.Context, devices <-chan input.Device) {
	rates := time.NewTicker(ratePeriod / rateSteps)
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
// They are worked out anew rateSteps times in that time, each time over
// the whole period that just ended, so that they move evenly and can be
// drawn as a curve.
var ratePeriod = time.Second

const rateSteps = 10

// TrailLen is how many steps of the past a [Trail] holds.
const TrailLen = 122

// Trail is how busy an instrument was over the last while, step by step:
// one value for every tenth of [ratePeriod], the newest last, each the
// number of events of that step alone, as a rate per period. Nothing is
// averaged; that is what the rates in a [Status] are for.
type Trail struct {
	// Seq counts the steps since the instrument's device was connected:
	// it tells whoever draws the trail when a step was added.
	Seq uint64
	// Step is the time one value covers, and Age how long ago the newest
	// was added: whoever draws the trail moving evenly from step to step
	// needs to know where between two steps the time is, and that is
	// better told than guessed from when a step was noticed.
	Step, Age time.Duration
	// Input and MIDI are what came in from the device and what went out.
	Input, MIDI []int
}

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
		a := m.choose(r.info, r.has, r.log)
		if sameProfile(a.profile, r.assigned) {
			continue
		}
		m.assign(r, a)
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

// SetAllow replaces what decides whether a device may become an
// instrument; nil allows every device. Instruments of devices that may
// not be one any more stop, and devices that may now become instruments.
func (m *Manager) SetAllow(allow func(control.DeviceInfo) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.Allow = allow
	for r := range m.running {
		a := m.choose(r.info, r.has, r.log)
		if sameProfile(a.profile, r.assigned) {
			continue
		}
		m.assign(r, a)
	}
}

// Device is a connected device that a profile selects.
type Device struct {
	Info control.DeviceInfo
	// Allowed says whether the device may be an instrument (see
	// [Options.Allow]).
	Allowed bool
}

// Devices returns the connected devices that are instruments or would be
// if they were allowed to, in no particular order.
func (m *Manager) Devices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Device, 0, len(m.running))
	for r := range m.running {
		if profile := config.Select(m.profiles, r.info); profile == nil || !usable(r.info, profile) {
			continue
		}
		out = append(out, Device{Info: r.info, Allowed: m.opts.Allow == nil || m.opts.Allow(r.info)})
	}
	return out
}

// choose decides what a device with these controls is to be with the
// current profiles. The caller holds m.mu.
func (m *Manager) choose(info control.DeviceInfo, has []control.Input, log *slog.Logger) assignment {
	profile := config.Select(m.profiles, info)
	if profile == nil || !usable(info, profile) {
		return assignment{}
	}
	if m.opts.Allow != nil && !m.opts.Allow(info) {
		return assignment{}
	}
	if profile.Generate.Any() && len(has) == 0 {
		log.Debug("the device does not say what it has; only what the profile names plays", "profile", profile.Name)
	}
	eng, err := engine.New(profile.For(has))
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
	// What a game controller says it has is listed from the start. A
	// keyboard's keys are known by what it is.
	var has []control.Input
	if d, ok := dev.(input.Describer); ok && info.Type != control.DeviceKeyboard {
		has = d.Controls()
	}
	r := &runner{
		device: dev, info: info, has: has, log: m.log.With("device", info.Name),
		open:     m.opts.OpenEndpoint,
		lights:   m.opts.Lights,
		onToggle: m.toggle,
		onLearn:  m.toggleLearning,
		wake:     make(chan struct{}, 1),
		quit:     m.quit,
		changed:  m.changed,
		logMIDI:  m.opts.LogMIDI,
	}
	r.input.seed(has)
	m.mu.Lock()
	m.running[r] = struct{}{}
	r.endpointName = m.takeName(EndpointPrefix + info.Name)
	r.wanted.Store(m.active)
	r.learn.Store(&learnWish{m.learning, m.opts.LearnThreshold})
	r.simulate = m.simulated[lightKey(info.Name, info.Type.String(), info.Location)]
	m.assign(r, m.choose(info, r.has, r.log))
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

// inputRules is what the settings and its profile say about one device.
type inputRules struct {
	grab   bool
	toggle []control.Code
	// tap says that the toggle is one key and works by a tap, and hide
	// that the key is kept from the system.
	tap, hide bool
	// left says that the device would be kept from the system and is
	// not, because nothing on it turns HIDI off again.
	left bool
	// hysteresis and rest are described at [Options].
	hysteresis float64
	rest       engine.Rest
}

// rulesFor works out the rules for a device that plays with a profile, or
// with none. The toggle is the one the settings name for every device, or
// else the profile's own. A keyboard without either is not kept from the
// system, whatever the settings say: with nothing on it to turn HIDI off,
// whoever has only that keyboard could not type again.
func (m *Manager) rulesFor(info control.DeviceInfo, profile *config.Profile) *inputRules {
	toggle := m.opts.Toggle
	if len(toggle) == 0 && profile != nil {
		toggle = profile.Toggle
	}
	tap := m.opts.ToggleTap && len(toggle) == 1
	r := &inputRules{
		grab: m.opts.Grab != nil && m.opts.Grab(info), toggle: toggle,
		tap: tap, hide: len(toggle) == 1 && (tap && m.opts.HideToggle || m.opts.OwnToggle),
		hysteresis: m.opts.Hysteresis, rest: m.opts.Rest,
	}
	if r.grab && len(toggle) == 0 && info.Type == control.DeviceKeyboard {
		r.grab, r.left = false, profile != nil
	}
	return r
}

// rule gives a device the rules that go with a profile, and says so when
// they leave a keyboard typing that was to be kept from it. The caller
// holds m.mu.
func (m *Manager) rule(r *runner, profile *config.Profile) {
	rules := m.rulesFor(r.info, profile)
	if was := r.rules.Load(); rules.left && (was == nil || !was.left) {
		r.log.Warn("this keyboard keeps typing while it plays: its profile has no "+engine.ToggleName+" and the settings name no toggle, so nothing on it could turn HIDI off again",
			"profile", profile.Name)
	}
	r.rules.Store(rules)
}

// assign gives a device what it is to be, with the rules that go with
// it. The caller holds m.mu.
func (m *Manager) assign(r *runner, a assignment) {
	m.rule(r, a.profile)
	r.assign(a)
}

// SetToggleTap changes whether the toggle works by a tap and whether its
// key is hidden from the system, while the manager runs. See
// [Options.ToggleTap] and [Options.HideToggle].
func (m *Manager) SetToggleTap(tap, hide bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.ToggleTap, m.opts.HideToggle = tap, hide
	for r := range m.running {
		m.rule(r, r.assigned)
		r.poke()
	}
}

// SetOwnToggle changes whether a toggle of one key is kept from the rest
// of the system; see [Options.OwnToggle].
func (m *Manager) SetOwnToggle(own bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.OwnToggle = own
	for r := range m.running {
		m.rule(r, r.assigned)
		r.poke()
	}
}

// SetPlaying changes the hysteresis of axes that act as buttons and what
// turning HIDI off returns to rest, while the manager runs; see
// [Options.Hysteresis] and [Options.Rest].
func (m *Manager) SetPlaying(hysteresis float64, rest engine.Rest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts.Hysteresis, m.opts.Rest = hysteresis, rest
	for r := range m.running {
		m.rule(r, r.assigned)
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
		m.rule(r, r.assigned)
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
	// that begins with "built-in/". ProfileKind is the profile's kind: a
	// factory profile or one of the user's.
	ProfileSource string
	ProfileKind   string
	// State is the current state of the instrument.
	State engine.State
	// Input is what the device itself reports, before any profile is
	// applied to it.
	Input Input
	// InputRate is how many events the device reported in the last
	// second, and MIDIRate how many MIDI messages the instrument sent.
	InputRate, MIDIRate int
	// Trail is how busy the instrument was over the last seconds.
	Trail Trail
	// Keys is what every bound key does in the selected layout.
	Keys []engine.KeyRole
	// Controllers is what the selected layout sends by the position of
	// controls, in the order of those controls, with the values last
	// sent. It must not be changed.
	Controllers []engine.Sent
	// Lit says that the lights of the keyboard show the instrument. Then
	// Lights is the colour of every key that has one of its own, and
	// LightRest the colour of the other keys. Lights must not be changed.
	Lit       bool
	Lights    map[control.Code]light.RGB
	LightRest light.RGB
	// Simulated says that the lights are not the keyboard's own: HIDI
	// does not control them, and Lights and LightRest are what it would
	// show, as [Manager.SetSimulated] asked for.
	Simulated bool
	// Toggle is the key, or the keys pressed together, that turn HIDI
	// off and on from this device; none if it has no way to.
	Toggle []control.Code
	// Left says that the device would be kept from the rest of the
	// system while it plays and is not, because it has no toggle: a
	// keyboard that keeps typing.
	Left bool
}

// lightKey names a device for [Manager.SetSimulated].
func lightKey(name, kind, location string) string { return name + "\x00" + kind + "\x00" + location }

// SetSimulated turns the showing of lights on or off for a keyboard whose
// lights HIDI does not control: its status then carries what the lights
// would show. The device is named as its status names it. It lasts while
// HIDI runs, also while the device is away.
func (m *Manager) SetSimulated(name, kind, location string, on bool) {
	key := lightKey(name, kind, location)
	m.mu.Lock()
	if m.simulated == nil {
		m.simulated = map[string]bool{}
	}
	if on {
		m.simulated[key] = true
	} else {
		delete(m.simulated, key)
	}
	for r := range m.running {
		if lightKey(r.info.Name, r.info.Type.String(), r.info.Location) == key {
			r.mu.Lock()
			r.simulate = on
			r.mu.Unlock()
		}
	}
	m.mu.Unlock()
	m.changed()
}

// Status returns the running instruments, in no particular order.
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.running))
	for r := range m.running {
		if status, ok := r.snapshot(); ok {
			if rules := r.rules.Load(); rules != nil {
				status.Toggle, status.Left = rules.toggle, rules.left
			}
			out = append(out, status)
		}
	}
	return out
}

// RetryOutput is how long after its MIDI device broke an instrument first
// tries for a new one. Each further try waits twice as long, up to
// maxRetryOutput. healthPeriod is how often a MIDI device that can tell is
// asked whether it still works.
var (
	RetryOutput    = 2 * time.Second
	maxRetryOutput = 30 * time.Second
	healthPeriod   = 2 * time.Second
)

// parkedPoll is how often a parked device is looked at. Its input is of no
// interest, so it is taken in one go every so often and thrown away: a
// mouse reports a thousand times a second, and following each report for
// nothing would keep the processor busy.
var parkedPoll = 500 * time.Millisecond

// parkedQuiet is how long nothing has to come from a parked device for
// its input to count as emptied.
const parkedQuiet = 2 * time.Millisecond

// runner looks after one connected device. While a profile selects the
// device it is an instrument, with an engine and a MIDI endpoint; otherwise
// it is parked.
type runner struct {
	device input.Device
	info   control.DeviceInfo
	// has is what the device says it has, if it says.
	has  []control.Input
	log  *slog.Logger
	open func(name string) (output.Endpoint, error)

	// rules says whether the device is grabbed while the instrument is on,
	// and which keys turn HIDI off and on. The manager replaces it when
	// the settings change.
	rules atomic.Pointer[inputRules]
	// endpointName is the name of the instrument's MIDI device.
	endpointName string
	// onToggle is what the toggle combination calls.
	onToggle func()
	// onLearn is what the learn command calls, and learn what the
	// manager wants of learning mode.
	onLearn func()
	learn   atomic.Pointer[learnWish]
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
	measured   [rateSteps][2]uint64
	measuredAt int
	// trail holds the last steps, input and MIDI, the newest last, and
	// trailSeq counts them; r.mu guards both. trailBusy says that some
	// value in it is not zero.
	trail     [2][TrailLen]int
	trailSeq  uint64
	trailAt   time.Time // when the newest step was added
	trailBusy bool

	mu         sync.Mutex
	status     Status
	instrument bool // whether status describes a running instrument
	input      inputTracker
	lamp       Light // the lights in use, if any
	simulate   bool  // show what the lights would show; see SetSimulated
}

// learnWish is what the manager wants of learning mode.
type learnWish struct {
	on        bool
	threshold float64
}

func (r *runner) setLearning(on bool, threshold float64) {
	r.learn.Store(&learnWish{on, threshold})
	r.poke()
}

// measure works out the rates of the last period from the counters and
// reports whether they differ from those before.
func (r *runner) measure() bool {
	now := [2]uint64{r.seen.Load(), r.sent.Load()}
	// The oldest of the kept counts is from one period ago.
	then := r.measured[r.measuredAt]
	last := r.measured[(r.measuredAt+rateSteps-1)%rateSteps]
	in, out := int(now[0]-then[0]), int(now[1]-then[1])
	r.measured[r.measuredAt] = now
	r.measuredAt = (r.measuredAt + 1) % rateSteps
	r.mu.Lock()
	defer r.mu.Unlock()
	// The step that just ended joins the trail, as a rate.
	step := [2]int{int(now[0]-last[0]) * rateSteps, int(now[1]-last[1]) * rateSteps}
	was := r.trailBusy
	r.trailBusy = false
	for i := range r.trail {
		copy(r.trail[i][:], r.trail[i][1:])
		r.trail[i][TrailLen-1] = step[i]
		for _, v := range r.trail[i] {
			if v != 0 {
				r.trailBusy = true
			}
		}
	}
	r.trailSeq++
	r.trailAt = time.Now()
	if !r.instrument {
		return false
	}
	// A trail with something in it moves with every step, until what
	// was in it has left it.
	changed := was || r.trailBusy || r.status.InputRate != in || r.status.MIDIRate != out
	r.status.InputRate, r.status.MIDIRate = in, out
	return changed
}

// snapshot returns the status of the instrument, if the device is one.
func (r *runner) snapshot() (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.status
	if r.instrument {
		status.Input = r.input.snapshot()
		status.Trail = Trail{
			Seq: r.trailSeq, Step: ratePeriod / rateSteps, Age: time.Since(r.trailAt),
			Input: slices.Clone(r.trail[0][:]), MIDI: slices.Clone(r.trail[1][:]),
		}
		if r.lamp != nil {
			status.Lights, status.LightRest, status.Lit = r.lamp.Painted()
			if !status.Lit && r.simulate {
				status.Lights, status.LightRest = r.lamp.Preview()
				status.Simulated = true
			}
		}
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
func (r *runner) run() { newSession(r).run() }

// discard throws away what the device has reported and nobody took, until
// nothing has come for a moment.
func (r *runner) discard() {
	events := r.device.Events()
	quiet, enough := time.NewTimer(parkedQuiet), time.After(parkedPoll/5)
	defer quiet.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // the device is gone, which the caller finds out
			}
			quiet.Reset(parkedQuiet)
		case <-quiet.C:
			return
		case <-enough:
			return
		}
	}
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
		r.log.Debug("instrument changed",
			"octave", now.Octave, "transpose", now.Transpose,
			"channel", now.Channel+1, "velocity", now.Velocity, "layout", now.Layout)
	}
	switch {
	case chord && now.Chord == "":
		r.log.Debug("chord forgotten")
	case chord:
		r.log.Debug("chord remembered", "intervals", now.Chord)
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
		r.log.Error("sending MIDI failed; the MIDI device is created anew", "error", err)
		return false
	}
	r.sent.Add(uint64(len(msgs)))
	return true
}
