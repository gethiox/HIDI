package instrument

import (
	"errors"
	"slices"
	"time"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/input"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

// holding says how a device is held.
type holding int

const (
	holdFree    holding = iota // the system has the device
	holdGrabbed                // only HIDI has it
	holdPassing                // the system has it, except for the toggle key
)

// session is one device being run, from the moment it is plugged in until
// it is gone: the state of the runner's goroutine, which nothing else
// touches. What other goroutines read or ask for is in the runner.
type session struct {
	*runner

	eng      *engine.Engine  // nil while parked
	endpoint output.Endpoint // nil while parked
	profile  *config.Profile
	state    engine.State
	out      []midi.Message
	active   bool // the instrument is on, and grabbed if it should be
	failed   bool // the endpoint broke; a new one is being tried for
	events   <-chan control.Event

	// limit thins out axis input; due collects what it releases, and
	// release fires when it next has something.
	limit   *limiter
	due     []control.Event
	release <-chan time.Time
	// steady drops the trembling of resting axes.
	steady *steadier

	// held tracks which keys of the toggle combination are down.
	held map[control.Code]bool

	// retry fires when a new MIDI device is to be tried for after the old
	// one broke, and health when the one in use is next asked how it is.
	retry     <-chan time.Time
	retryWait time.Duration
	health    <-chan time.Time
	// openFailed is the assignment whose MIDI device could not be made;
	// it is tried again when retry fires, not at every wake-up.
	openFailed *assignment
	// stale says that the device has been parked for a while: what it
	// reported meanwhile is old, and must not be played when a profile
	// takes the device after all.
	stale bool

	// rule is the input rules in force, and hold says how the device is
	// held now. regrab makes that fit the rules and the state.
	rule *inputRules
	hold holding
	// passer is the device, if it can let single keys through to the
	// system. cannotPass says that passing was tried and failed; it is
	// not tried again.
	passer     input.Passer
	cannotPass bool
	// The keys the selected layout leaves to the system: shared are
	// those the grab lets through now, and passed those of the layout,
	// worked out anew only for another engine or layout.
	shared, passed []control.Code
	passedFor      *engine.Engine
	passedLayout   string
	cannotShare    bool

	// A tapped toggle: tapDown says its key is down, tapTimer fires when
	// it has been down too long for a tap, and typed says that the key
	// was then passed on to the system, which still awaits its release.
	tapDown, tapLong, typed bool
	tapTimer                <-chan time.Time

	// light is the lights of the device while it is an instrument. show
	// makes them fit the instrument; what every key does is worked out
	// anew only for another engine or layout.
	light       Light
	shownFor    *engine.Engine
	shownName   string
	keys        []engine.KeyRole
	at, layouts int

	// rolesFor and rolesLayout say for which engine and layout roles
	// last recorded what the keys do.
	rolesFor    *engine.Engine
	rolesLayout string
	// sentLayout and sentChannel are those for which sends last looked.
	sentLayout  string
	sentChannel uint8

	// learnt is the wish about learning mode the engine has, and
	// learntFor the engine that has it: a new engine knows of none.
	learnt    *learnWish
	learntFor *engine.Engine
}

// newSession begins with a device that nothing is done with yet.
func newSession(r *runner) *session {
	s := &session{
		runner: r,
		events: r.device.Events(),
		limit:  newLimiter(0),
		steady: newSteadier(0),
		held:   map[control.Code]bool{},
		rule:   r.rules.Load(),
	}
	s.passer, _ = r.device.(input.Passer)
	return s
}

// fail takes the instrument off a MIDI device that broke, and arranges for
// a new one to be tried for.
func (s *session) fail() {
	// The MIDI device broke, which on Windows happens when the MIDI
	// service is restarted. The instrument plays nothing until a new
	// device could be created, and gives its keys back meanwhile.
	if s.failed {
		return
	}
	s.failed = true
	if s.endpoint != nil {
		go s.endpoint.Close() // may wait for a service that is gone
		s.endpoint = nil
	}
	s.limit.drop()
	// A toggle key that is being held, and passed on to the system,
	// is let go of there; and what other programs sent to the device
	// that is gone does not stay lit.
	s.endTap()
	s.heard.clear()
	s.retryWait = RetryOutput
	s.retry = time.After(s.retryWait)
	s.regrab()
}

// regrab makes the way the device is held fit the rules and the state of
// the instrument: grabbed while it plays, the toggle key alone kept from
// the system where that is asked for, and free otherwise.
func (s *session) regrab() {
	want := holdFree
	var share []control.Code
	switch {
	case s.eng == nil || !s.rule.grab || s.failed:
	case s.active:
		want = holdGrabbed
		if s.passedFor != s.eng || s.passedLayout != s.state.Layout {
			s.passed, s.passedFor, s.passedLayout = s.eng.Passed(), s.eng, s.state.Layout
		}
		if s.passer != nil && !s.cannotShare {
			share = s.passed
		}
	case s.rule.hide && s.passer != nil && !s.cannotPass:
		want = holdPassing
	}
	if want == s.hold && slices.Equal(share, s.shared) {
		return
	}
	if want == holdPassing {
		if err := s.passer.Pass(s.rule.toggle); err != nil {
			s.cannotPass = true
			s.log.Warn("the toggle key cannot be hidden from the system on this device", "error", err)
			want = holdFree
			if s.hold == holdFree {
				return
			}
		}
	}
	switch want {
	case holdGrabbed:
		if len(share) > 0 {
			err := s.passer.Share(share)
			if err == nil {
				break
			}
			s.cannotShare, share = true, nil
			s.log.Warn("the keys the profile leaves to the system cannot be passed on from this device; they do nothing while HIDI is on", "error", err)
		}
		if err := s.device.Grab(); errors.Is(err, input.ErrGrabbedElsewhere) {
			// Not a fault: the user runs a program that takes the
			// device and passes its input on through one of its own.
			s.log.Info("another program has this device to itself, such as an input remapper; its input reaches HIDI through that program's device", "error", err)
		} else if err != nil {
			s.log.Warn("the device cannot be grabbed; it stays usable by other programs", "error", err)
		}
	case holdFree:
		if err := s.device.Release(); err != nil {
			s.log.Warn("the device cannot be released", "error", err)
		}
	}
	s.hold, s.shared = want, share
}

// ungrab turns the instrument off and gives the device back.
func (s *session) ungrab() {
	s.active = false
	s.regrab()
}

// endTap ends what a tapped toggle key has under way: a key that was
// passed on to the system is let go of there.
func (s *session) endTap() {
	if s.typed && s.passer != nil {
		s.passer.Type(s.rule.toggle[0], false)
	}
	s.tapDown, s.tapLong, s.typed, s.tapTimer = false, false, false, nil
}

// tap handles an event of the toggle key and reports whether it was a
// tap that toggles.
func (s *session) tap(ev control.Event) bool {
	switch {
	case ev.Pressed() && !s.tapDown:
		s.tapDown, s.tapLong, s.tapTimer = true, false, time.After(TapHold)
	case !ev.Pressed() && s.tapDown:
		toggles := !s.tapLong
		s.endTap()
		return toggles
	}
	return false
}

// lightDown shows a key that went down or up while HIDI is off, where
// the keys that are down light up. apply shows the lights before the
// event is recorded, so a key needs this to show as it is now; the
// toggle key does too, held as long as it is passed on to the system.
func (s *session) lightDown(ev control.Event) {
	if !s.active && s.light != nil && ev.Code.Kind == control.KindKey {
		s.show()
	}
}

// show makes the lights fit the instrument.
func (s *session) show() {
	if s.light == nil || s.eng == nil {
		return
	}
	if s.shownFor != s.eng || s.shownName != s.state.Layout {
		s.keys, s.at, s.layouts = s.eng.Keys()
		s.shownFor, s.shownName = s.eng, s.state.Layout
	}
	scene := light.Scene{Active: s.active, Keys: s.keys, Layout: s.at, Layouts: s.layouts, State: s.state, External: s.heard.snapshot()}
	if !s.active {
		// While HIDI is off the keys that are down show.
		s.mu.Lock()
		scene.Pressed = s.input.keysDown()
		s.mu.Unlock()
	}
	s.light.Show(scene)
}

// dark gives the lights back, and forgets what other programs played.
func (s *session) dark() {
	if s.light != nil {
		s.light.Close()
		s.light = nil
		s.mu.Lock()
		s.lamp = nil
		s.mu.Unlock()
	}
	s.heard.clear()
}

// roles records what the keys do, for [Manager.Status], when the engine
// or its layout is another.
func (s *session) roles() {
	if s.eng == nil || (s.rolesFor == s.eng && s.rolesLayout == s.state.Layout) {
		return
	}
	s.rolesFor, s.rolesLayout = s.eng, s.state.Layout
	now, _, _ := s.eng.Keys()
	rests := s.eng.Neutrals()
	s.mu.Lock()
	s.status.Keys, s.status.Neutrals = now, rests
	s.mu.Unlock()
	s.changed()
}

// sends records what the instrument sends by the position of controls,
// its controllers and the pitch bend with their last values, for
// [Manager.Status]: when one of them was among the messages just sent, and
// whenever asked to look anyway, after something other than a played
// event may have changed them.
func (s *session) sends(look bool, sent []midi.Message) {
	for _, m := range sent {
		if look {
			break
		}
		look = m.Kind == midi.KindControlChange || m.Kind == midi.KindPitchBend
	}
	if !look {
		return
	}
	var now []engine.Sent
	if s.eng != nil {
		now = s.eng.Controllers()
	}
	s.mu.Lock()
	same := slices.Equal(s.status.Controllers, now)
	s.status.Controllers = now
	s.mu.Unlock()
	if !same {
		s.changed()
	}
}

// reassign makes the device what the manager last asked for.
func (s *session) reassign() {
	if s.failed {
		return // what is pending is taken up once there is an output again
	}
	a := s.pending.Swap(nil)
	if a == nil {
		return
	}
	// What cannot be done now is put back, unless the manager has
	// asked for something newer meanwhile: the manager believes the
	// device has what it assigned, and would never assign it again.
	keep := func() { s.pending.CompareAndSwap(nil, a) }
	if a == s.openFailed && s.retry != nil {
		keep() // its next try has a time
		return
	}
	if s.eng != nil && s.active && !s.send(s.endpoint, s.eng.Reset(s.out[:0])) {
		keep()
		s.fail() // the old profile's notes could not be released
		return
	}
	if a.engine == nil {
		if s.eng == nil {
			s.log.Debug("device not used", "type", s.info.Type.String(), "hardware", s.info.Hardware.String())
			return
		}
		if s.active {
			s.ungrab()
		}
		s.limit.drop()
		if err := s.endpoint.Close(); err != nil {
			s.log.Warn("closing the MIDI device failed", "error", err)
		}
		s.dark()
		s.eng, s.endpoint, s.profile = nil, nil, nil
		clear(s.held)
		s.setStatus(Status{}, false)
		s.log.Info("instrument stopped", "reason", "no profile selects the device any more")
		return
	}
	if s.endpoint == nil {
		// Before the MIDI device is there for anyone to see: from then
		// on, what the device reports counts.
		if s.stale {
			s.stale = false
			s.discard()
		}
		e, err := s.open(s.endpointName)
		if err != nil {
			// Tried again, like for a MIDI device that broke: the
			// MIDI service of the system may only be starting.
			if a != s.openFailed {
				s.log.Error("MIDI device cannot be created; trying again", "error", err)
				s.openFailed, s.retryWait = a, RetryOutput
			} else {
				s.log.Debug("the MIDI device cannot be created yet", "error", err)
				s.retryWait = min(s.retryWait*2, maxRetryOutput)
			}
			s.retry = time.After(s.retryWait)
			keep()
			return
		}
		s.openFailed = nil
		s.endpoint = e
		s.health = time.After(healthPeriod)
		if s.lights != nil && s.info.Type == control.DeviceKeyboard {
			s.light = s.lights(s.info)
			s.mu.Lock()
			s.lamp = s.light
			s.mu.Unlock()
		}
		if receiver, ok := e.(output.Receiver); ok {
			go s.listen(receiver)
		}
		s.log.Info("instrument started",
			"type", s.info.Type.String(), "hardware", s.info.Hardware.String(),
			"location", s.info.Location, "profile", a.profile.Name, "kind", a.profile.Kind, "source", a.profile.Source)
	} else {
		s.log.Info("profile changed", "profile", a.profile.Name, "kind", a.profile.Kind, "source", a.profile.Source, "was", s.profile.Name)
	}
	s.eng, s.profile = a.engine, a.profile
	s.limit = newLimiter(s.profile.AxisRate)
	s.steady = newSteadier(s.profile.AxisThreshold)
	s.state = s.eng.State()
	s.setStatus(Status{
		Device: s.info, Profile: s.profile.Name, ProfileSource: s.profile.Source, ProfileKind: s.profile.Kind,
		// A factory profile with a rule: what the device plays is made
		// for it, written nowhere.
		Generated: s.profile.Kind == config.KindFactory && s.profile.Generate.Any(),
		State:     s.state,
	}, true)
	s.roles()
	s.replay() // where the axes stand, for the new profile
}

// apply brings the device to the state the manager wants.
func (s *session) apply() {
	defer s.show() // whatever changed, the lights follow
	defer s.sends(true, nil)
	s.reassign()
	if wish := s.learn.Load(); s.eng != nil && !s.failed && (wish != s.learnt || s.learntFor != s.eng) {
		s.learnt, s.learntFor = wish, s.eng
		// Leaving the mode, the axes say where they really are.
		if msgs := s.eng.SetLearning(wish.on, wish.threshold, s.out[:0]); s.active && !s.send(s.endpoint, msgs) {
			s.fail()
		}
		s.publish(s.eng, &s.state)
	}
	if now := s.rules.Load(); now != s.rule {
		// Changed settings: another combination starts with no key
		// of it held, and the grab follows at once.
		if s.tapDown {
			s.endTap()
		}
		s.rule = now
		clear(s.held)
		s.cannotPass, s.cannotShare = false, false
		if s.hold == holdPassing {
			// The keys to hide may be others now.
			if err := s.device.Release(); err != nil {
				s.log.Warn("the device cannot be released", "error", err)
			}
			s.hold = holdFree
		}
		s.regrab()
	}
	if s.eng != nil {
		// A new engine has none of these of its own. Another step can
		// move the velocity to the nearest value of its own, which is
		// then shown.
		s.eng.SetHysteresis(s.rule.playing.Hysteresis)
		s.eng.SetVelocityStep(s.rule.playing.VelocityStep)
		s.publish(s.eng, &s.state)
	}
	want := s.wanted.Load()
	if s.eng == nil || want == s.active || s.failed {
		s.regrab() // a new instrument, or one that stopped being one
		return
	}
	s.active = want
	if s.active {
		s.regrab()
		s.replay() // turning off put every controller to rest
		return
	}
	// Turning off: the device goes back to the system, and nothing
	// may keep sounding. The device first: whatever holds up the
	// sending must not keep a keyboard from its owner. The pitch bend
	// and the controllers go back to rest as far as the settings say;
	// what stays is as it was last sent.
	s.limit.drop() // a position from before must not arrive after
	s.regrab()
	if !s.send(s.endpoint, s.eng.Silence(s.rule.playing.Rest, s.out[:0])) {
		s.fail()
	}
	s.publish(s.eng, &s.state)
}

// play passes one event through the engine to the endpoint.
func (s *session) play(ev control.Event) {
	s.out = s.eng.Handle(ev, s.out[:0])
	if !s.send(s.endpoint, s.out) {
		s.fail()
		return
	}
	s.publish(s.eng, &s.state)
	if s.eng.LearnRequested() {
		s.onLearn() // for all instruments; this one follows below
	}
	// Another layout or channel sends other things, or to elsewhere.
	other := s.state.Layout != s.sentLayout || s.state.Channel != s.sentChannel
	s.sentLayout, s.sentChannel = s.state.Layout, s.state.Channel
	if s.state.Layout != s.passedLayout {
		s.regrab() // another layout may leave other keys to the system
	}
	s.roles()
	s.sends(other, s.out)
	s.show()
}

// replay sends where every axis stands, as far as the device has
// said. Turning off, another profile and a new MIDI device all leave
// the controllers at rest, while a lever stays where it was put; and
// a lever that rests would not speak up by itself, since its
// trembling is dropped.
func (s *session) replay() {
	if s.eng == nil || !s.active || s.failed {
		return
	}
	s.steady = newSteadier(s.profile.AxisThreshold)
	s.mu.Lock()
	axes := s.input.axesNow()
	s.mu.Unlock()
	now := time.Now()
	for _, a := range axes {
		if s.failed {
			return
		}
		s.play(control.Event{Code: a.Code, Part: a.Part, Value: a.Value, Time: now})
	}
}

// flush plays the axis events the limiter held back long enough, and
// arranges to be called again for those it still holds.
func (s *session) flush() {
	var wait time.Duration
	s.due, wait = s.limit.due(time.Now(), s.due[:0])
	for _, ev := range s.due {
		if s.failed {
			break
		}
		s.play(ev)
	}
	s.release = nil
	if wait > 0 {
		s.release = time.After(wait)
	}
}

// run is the life of the device: it applies what the manager asks for and
// plays what the device reports, until the device is gone or HIDI stops.
func (s *session) run() {
	s.apply()
loop:
	for {
		if s.eng == nil && !s.failed {
			// Parked: wait for a profile, and notice the device going.
			s.stale = true
			select {
			case <-s.wake:
				s.apply()
			case <-s.retry:
				s.retry = nil
				s.apply() // the MIDI device is tried for again
			case <-s.quit:
				break loop
			case <-time.After(parkedPoll):
				// Everything that has gathered is thrown away, not one
				// event: only Linux drops for a reader that falls behind,
				// and what is left to pile up elsewhere holds up the
				// reading of other devices, or would be played in one
				// burst when a profile takes the device after all.
				// A queue behind the channel fills it again a moment after
				// it was emptied, so the emptying ends only when nothing
				// has come for a moment, or after a while at the most.
				quiet, enough := time.NewTimer(parkedQuiet), time.After(parkedPoll/5)
				for drained := false; !drained; {
					select {
					case _, ok := <-s.events:
						if !ok {
							break loop
						}
						quiet.Reset(parkedQuiet)
					case <-quiet.C:
						drained = true
					case <-enough:
						drained = true
					}
				}
				quiet.Stop()
			}
			continue
		}
		select {
		case <-s.wake:
			s.apply()
		case ev, ok := <-s.events:
			if !ok {
				break loop
			}
			// A request from the manager may be waiting behind this
			// event; it comes first.
			s.apply()
			if s.failed || s.eng == nil {
				continue // draining, or parked a moment ago
			}
			s.saw(ev)
			if s.rule.tap {
				if ev.Code == s.rule.toggle[0] {
					if s.tap(ev) {
						s.onToggle()
						s.apply()
					} else {
						s.lightDown(ev)
					}
					continue // the key of a tapped toggle plays nothing
				}
			} else if toggled(s.rule.toggle, s.held, ev) {
				s.onToggle()
				s.apply()
				continue
			}
			if !s.active {
				s.lightDown(ev)
				continue
			}
			now := time.Now()
			if !s.steady.admit(ev, now) {
				continue
			}
			if !s.limit.admit(ev, now) {
				if s.release == nil {
					s.flush()
				}
				continue
			}
			s.play(ev)
		case <-s.tapTimer:
			// Held too long for a tap: the key does what it normally
			// does, which takes passing it on where it is hidden.
			s.tapTimer = nil
			if s.tapDown {
				s.tapLong = true
				if s.rule.hide && s.passer != nil && s.hold != holdFree {
					s.typed = s.passer.Type(s.rule.toggle[0], true) == nil
				}
			}
		case <-s.release:
			s.release = nil
			s.apply()
			if s.eng != nil && s.active && !s.failed {
				s.flush()
			}
		case <-s.health:
			s.health = nil
			if s.endpoint == nil {
				break
			}
			if checker, ok := s.endpoint.(output.Checker); ok {
				if err := checker.Check(); err != nil {
					s.log.Error("the MIDI device was lost; it is created anew", "error", err)
					s.fail()
					break
				}
				s.health = time.After(healthPeriod)
			}
		case <-s.retry:
			s.retry = nil
			e, err := s.open(s.endpointName)
			if err != nil {
				s.log.Debug("the MIDI device cannot be created yet", "error", err)
				s.retryWait = min(s.retryWait*2, maxRetryOutput)
				s.retry = time.After(s.retryWait)
				break
			}
			s.endpoint, s.failed = e, false
			s.health = time.After(healthPeriod)
			if receiver, ok := e.(output.Receiver); ok {
				go s.listen(receiver)
			}
			// Whatever was held when the old device broke is forgotten.
			s.eng.Reset(s.out[:0])
			clear(s.held)
			s.log.Info("the MIDI device is back")
			s.regrab()
			s.replay() // the new device knows nothing of where the axes stand
			s.apply()
			if s.eng != nil {
				s.publish(s.eng, &s.state)
			}
		}
	}

	if s.tapDown {
		s.endTap()
	}
	if s.eng == nil {
		s.log.Debug("unused device gone", "reason", s.device.Err())
		return
	}
	if !s.failed && s.active {
		// Release whatever is still held, so that nothing keeps sounding.
		s.send(s.endpoint, s.eng.Reset(s.out[:0]))
	}
	if s.endpoint != nil {
		if err := s.endpoint.Close(); err != nil {
			s.log.Warn("closing the MIDI device failed", "error", err)
		}
	}
	s.dark()
	s.setStatus(Status{}, false)
	s.log.Info("instrument stopped", "reason", s.device.Err())
}
