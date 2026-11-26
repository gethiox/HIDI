package instrument

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/midi"
)

func shortRetry(t *testing.T) {
	old := RetryOutput
	RetryOutput = 20 * time.Millisecond
	t.Cleanup(func() { RetryOutput = old })
}

// Found in review: a profile change that arrives while the endpoint is broken and a note
// is held is lost.
func TestProfileChangeSurvivesABrokenDevice(t *testing.T) {
	shortRetry(t)
	r := newRigWith(t, Options{Grab: GrabKeyboards}, profile(t, "Mine", `type = "keyboard"`, "C3"))
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	kbd.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")

	// The MIDI device breaks; nothing is sent yet so nobody noticed.
	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()
	r.manager.SetProfiles(append(builtin(t), profile(t, "Mine", `type = "keyboard"`, "D3")))

	eventually(t, "the new endpoint", func() bool { return r.endpoint(e.name) != e })
	time.Sleep(100 * time.Millisecond)
	e2 := r.endpoint(e.name)
	kbd.set(keyZ, 0)
	kbd.set(keyZ, 1)
	var sent string
	eventually(t, "a note on the new endpoint", func() bool { sent, _ = e2.state(); return sent != "" })
	t.Logf("after recovery the instrument plays: %s", sent)
	if !strings.Contains(sent, "note=D3") {
		t.Errorf("the profile change was lost: got %q, want D3", sent)
	}
}

// A2: a profile removal in the same situation leaves the instrument running.
func TestProfileRemovalSurvivesABrokenDevice(t *testing.T) {
	shortRetry(t)
	r := newRigWith(t, Options{Grab: func(control.DeviceInfo) bool { return true }},
		profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "C3"))
	pedal := newFakeDevice("Pedal", control.DeviceUnknown)
	pedal.info.Hardware = control.HardwareID{Vendor: 0x1234, Product: 1}
	e := r.plug(pedal)
	pedal.set(keyZ, 1)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=100")
	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()
	r.manager.SetProfiles(builtin(t)) // nothing selects the pedal any more

	eventually(t, "the new endpoint", func() bool { return r.endpoint(e.name) != e })
	time.Sleep(200 * time.Millisecond)
	if st := r.manager.Status(); len(st) != 0 {
		t.Errorf("the pedal is still an instrument with profile %q, grabbed=%v", st[0].Profile, pedal.isGrabbed())
	}
}

// Found in review: the first attempt to create the MIDI device fails; nothing ever tries again.
func TestFirstOpenIsTriedAgain(t *testing.T) {
	shortRetry(t)
	r := newRig(t)
	r.mu.Lock()
	r.openErr = errors.New("the MIDI service is not up yet")
	r.mu.Unlock()
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	r.devices <- kbd
	eventually(t, "the attempt", func() bool { return r.openFailures() == 1 })
	r.mu.Lock()
	r.openErr = nil
	r.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	r.manager.SetProfiles(builtin(t)) // the profile directory is read again
	r.manager.SetActive(false)
	r.manager.SetActive(true)
	time.Sleep(300 * time.Millisecond)
	if len(r.manager.Status()) != 1 {
		t.Errorf("after the MIDI service came up: %d instruments, %d failed attempts in all", len(r.manager.Status()), r.openFailures())
	}
}

// Found in review: a toggle key that was down when the endpoint broke.
func TestTapAcrossABrokenDevice(t *testing.T) {
	shortRetry(t)
	old := TapHold
	TapHold = 40 * time.Millisecond
	t.Cleanup(func() { TapHold = old })
	keyX := control.Key(45)

	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: []control.Code{keyZ}, ToggleTap: true, HideToggle: true})
	kbd := &passingDevice{fakeDevice: newFakeDevice("Test Keyboard", control.DeviceKeyboard)}
	r.devices <- kbd
	name := EndpointPrefix + "Test Keyboard"
	eventually(t, "endpoint", func() bool { return r.endpoint(name) != nil })
	e := r.endpoint(name)
	eventually(t, "the keyboard to be grabbed", kbd.isGrabbed)

	// The toggle key is held long: it is typed for the system.
	kbd.set(keyZ, 1)
	eventually(t, "the long press to be passed on", func() bool { return kbd.typedKeys() == keyZ.String()+"=true" })
	// The endpoint breaks while it is down.
	r.mu.Lock()
	r.openErr = errors.New("away")
	r.mu.Unlock()
	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()
	kbd.set(keyX, 1)
	eventually(t, "the keyboard to be given back", func() bool { return !kbd.isGrabbed() })
	kbd.set(keyZ, 0) // released while there is no MIDI device
	kbd.set(keyX, 0)
	kbd.drain(t)
	time.Sleep(100 * time.Millisecond)
	t.Logf("typed for the system while failed: %q", kbd.typedKeys())
	if !strings.HasSuffix(kbd.typedKeys(), "=false") {
		t.Errorf("the system never saw the toggle key come up: %q", kbd.typedKeys())
	}

	r.mu.Lock()
	r.openErr = nil
	r.mu.Unlock()
	eventually(t, "the new endpoint", func() bool { return r.endpoint(name) != e })
	eventually(t, "the keyboard to be grabbed again", kbd.isGrabbed)
	time.Sleep(50 * time.Millisecond)

	// One tap should turn HIDI off and give the keyboard back.
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	kbd.drain(t)
	time.Sleep(100 * time.Millisecond)
	if r.manager.Active() || kbd.isGrabbed() {
		t.Errorf("first tap after recovery: active=%v grabbed=%v typed=%q", r.manager.Active(), kbd.isGrabbed(), kbd.typedKeys())
	}
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	kbd.drain(t)
	time.Sleep(100 * time.Millisecond)
	t.Logf("second tap after recovery: active=%v grabbed=%v", r.manager.Active(), kbd.isGrabbed())
}

// Found in review: notes heard from other programs survive the loss of the MIDI device.
func TestHeardNotesGoWithTheDevice(t *testing.T) {
	shortRetry(t)
	var mu sync.Mutex
	var lamp *fakeLight
	r := newRigWith(t, Options{Lights: func(info control.DeviceInfo) Light {
		mu.Lock()
		defer mu.Unlock()
		lamp = &fakeLight{}
		return lamp
	}})
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	scene := func() light.Scene { mu.Lock(); l := lamp; mu.Unlock(); s, _ := l.last(); return s }
	has := func(set [2]uint64, note int) bool { return set[note/64]&(1<<(note%64)) != 0 }
	e.incoming <- []midi.Message{midi.NoteOn(2, 64, midi.From7(100))}
	eventually(t, "the note from outside", func() bool { return has(scene().External[2], 64) })

	e.mu.Lock()
	e.sendErr = errors.New("broken")
	e.mu.Unlock()
	kbd.set(keyZ, 1)
	eventually(t, "the new endpoint", func() bool { return r.endpoint(e.name) != e })
	kbd.set(keyZ, 0)
	kbd.set(keyZ, 1)
	r.expectSent(r.endpoint(e.name), "NoteOn ch=1 note=C0 velocity=64")
	time.Sleep(50 * time.Millisecond)
	if has(scene().External[2], 64) {
		t.Errorf("the lights still show a note that was sent to the MIDI device that is gone")
	}
}

// Found in review: a lever at rest is not sent again after HIDI was off and
// on. That is where turning off puts the controllers to rest; where it
// leaves them, as HIDI comes, see TestOffKeepsControllers.
func TestRestingAxisIsSentAfterOffAndOn(t *testing.T) {
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
	r := newRigWith(t, Options{Playing: Playing{Rest: engine.Rest{Controllers: true, PitchBend: true}}}, lever)
	pad := newFakeDevice("Throttle", control.DeviceGamepad)
	e := r.plug(pad)
	pad.set(control.Axis(2), 0.75)
	r.expectSent(e, "ControlChange ch=1 controller=7 value=95")
	pad.drain(t)
	r.manager.SetActive(false)
	var sent string
	eventually(t, "the reset", func() bool { sent, _ = e.state(); return strings.Count(sent, "ControlChange") == 2 })
	r.manager.SetActive(true)
	time.Sleep(50 * time.Millisecond)
	// The lever still stands at 0.75 and trembles, as levers do.
	for i := range 50 {
		pad.set(control.Axis(2), 0.75+float64(i%2)*0.004)
	}
	pad.drain(t)
	sent, _ = e.state()
	t.Logf("sent: %s", sent)
	if !strings.HasSuffix(sent, "value=95") && !strings.HasSuffix(sent, "value=96") {
		t.Errorf("after off and on the controller stays at its rest value although the lever is at 0.75")
	}
}

// Found in review: what a parked device reported long ago is played when a profile comes.
func TestParkedInputIsNotPlayedLater(t *testing.T) {
	old := parkedPoll
	parkedPoll = time.Hour // as good as never, like 500 ms against a full buffer
	t.Cleanup(func() { parkedPoll = old })
	r := newRig(t)
	pedal := newFakeDevice("Pedal", control.DeviceUnknown)
	pedal.info.Hardware = control.HardwareID{Vendor: 0x1234, Product: 1}
	r.devices <- pedal
	time.Sleep(50 * time.Millisecond)
	for range 8 { // pressed long before there is a profile
		pedal.set(keyZ, 1)
		pedal.set(keyZ, 0)
	}
	time.Sleep(50 * time.Millisecond)
	r.manager.SetProfiles(append(builtin(t), profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "C3")))
	name := EndpointPrefix + "Pedal"
	eventually(t, "the pedal's MIDI device", func() bool { return r.endpoint(name) != nil })
	time.Sleep(100 * time.Millisecond)
	sent, _ := r.endpoint(name).state()
	if sent != "" {
		t.Errorf("the new instrument played %d old notes at once", strings.Count(sent, "NoteOn"))
	}
}

// Turned off, an instrument puts back to rest what the settings say and
// leaves the rest as it was sent: the pitch bend goes, a controller stays
// and is not sent again when HIDI is turned on with its axis where it
// was. Changed while HIDI runs, the settings count from the next time.
func TestOffKeepsControllers(t *testing.T) {
	stick, err := config.Parse([]byte(`
schema = 1
name = "Stick"
[match]
type = "gamepad"
[defaults]
axis_rate = 0
[[layout]]
name = "Main"
[layout.bindings]
ABS_Z = { cc = 7 }
ABS_Y = { pitch_bend = true, centered = true }
`))
	if err != nil {
		t.Fatal(err)
	}
	r := newRigWith(t, Options{Playing: Playing{Rest: engine.Rest{PitchBend: true}}}, stick)
	pad := newFakeDevice("Stick", control.DeviceGamepad)
	e := r.plug(pad)
	pad.set(control.Axis(2), 0.75)
	pad.set(control.Axis(1), 1)
	const played = "ControlChange ch=1 controller=7 value=95; PitchBend ch=1 bend=+8191"
	r.expectSent(e, played)
	pad.drain(t)

	r.manager.SetActive(false)
	r.expectSent(e, played+"; PitchBend ch=1 bend=+0")
	// The output view keeps showing what the controller stands at.
	shows := func(want string) bool {
		for _, st := range r.manager.Status() {
			var got []string
			for _, c := range st.Controllers {
				if c.Known && c.PitchBend {
					got = append(got, fmt.Sprint("bend ", int(midi.To14(c.Value))-8192))
				} else if c.Known {
					got = append(got, fmt.Sprint("cc", c.Controller, " ", midi.To7(c.Value)))
				}
			}
			if strings.Join(got, ", ") == want {
				return true
			}
		}
		return false
	}
	eventually(t, "the status after turning off", func() bool { return shows("bend 0, cc7 95") })

	// On again: the stick is still pushed, so the pitch is bent again;
	// the controller is where it was and says nothing.
	r.manager.SetActive(true)
	r.expectSent(e, played+"; PitchBend ch=1 bend=+0; PitchBend ch=1 bend=+8191")

	// The other way round, set while HIDI runs.
	r.manager.SetPlaying(Playing{Rest: engine.Rest{Controllers: true}})
	r.manager.SetActive(false)
	r.expectSent(e, played+"; PitchBend ch=1 bend=+0; PitchBend ch=1 bend=+8191; ControlChange ch=1 controller=7 value=0")
}

// Reported by the maintainer: a profile of one's own that was made of a
// factory profile was not used by a device that was playing until HIDI
// was started again. The copy reads the same as what it was made of, and
// a device was only given another profile when the content differed.
func TestCopyOfAProfileTakesOverAtOnce(t *testing.T) {
	factory := profile(t, "Pads", `type = "gamepad"`, "C3")
	factory.Source, factory.Kind = "/hidi/profiles/factory/pads.toml", config.KindFactory
	r := newRigWith(t, Options{Profiles: nil}, factory)
	pad := newFakeDevice("Test Pad", control.DeviceGamepad)
	r.plug(pad)
	plays := func(source, kind string) func() bool {
		return func() bool {
			st := r.manager.Status()
			return len(st) == 1 && st[0].ProfileSource == source && st[0].ProfileKind == kind
		}
	}
	eventually(t, "the device to play with the factory profile", plays(factory.Source, config.KindFactory))

	// The user's copy: the same text, another file, and the user's.
	own := profile(t, "Pads", `type = "gamepad"`, "C3")
	own.Source, own.Kind = "/hidi/profiles/user/pads.toml", config.KindUser
	if own.Digest != factory.Digest {
		t.Fatal("the copy does not read the same as the factory profile")
	}
	r.manager.SetProfiles(append(builtin(t), factory, own))
	eventually(t, "the device to play with the copy", plays(own.Source, config.KindUser))
	// The same profiles again disturb nothing.
	before := r.manager.Status()[0].Trail.Seq
	r.manager.SetProfiles(append(builtin(t), factory, own))
	time.Sleep(20 * time.Millisecond)
	if !plays(own.Source, config.KindUser)() || r.manager.Status()[0].Trail.Seq < before {
		t.Error("the same profiles again changed what the device plays with")
	}
}

// The step of the velocity commands comes from the settings, at the start
// and while HIDI runs.
func TestVelocityStepFromSettings(t *testing.T) {
	p, err := config.Parse([]byte("schema = 2\nname = \"Steps\"\n[match]\ntype = \"keyboard\"\n[defaults]\nvelocity = 100\n" +
		"[[layout]]\nname = \"Main\"\n[layout.bindings]\nKEY_Z = \"C3\"\nKEY_X = \"velocity_up\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := newRigWith(t, Options{Grab: GrabKeyboards, Toggle: toggleKeys, Playing: Playing{VelocityStep: 10}}, p)
	kbd := newFakeDevice("Test Keyboard", control.DeviceKeyboard)
	e := r.plug(kbd)
	velocity := func(want uint8) {
		t.Helper()
		eventually(t, fmt.Sprintf("the velocity to be %d", want), func() bool {
			st := r.manager.Status()
			return len(st) == 1 && st[0].State.Velocity == want
		})
	}
	keyX := control.Key(45)
	kbd.set(keyX, 1)
	kbd.set(keyX, 0)
	velocity(110)
	kbd.set(keyZ, 1)
	kbd.set(keyZ, 0)
	r.expectSent(e, "NoteOn ch=1 note=C3 velocity=110; NoteOff ch=1 note=C3 velocity=0")

	// 110 is as far from 100 as from 120: the nearer to the profile's.
	r.manager.SetPlaying(Playing{VelocityStep: 20})
	velocity(100)
	kbd.set(keyX, 1)
	kbd.set(keyX, 0)
	velocity(120)
}

// Toggles from several devices at once each count: an even number of
// them leaves HIDI as it was. Read and flipped in two steps, two toggles
// at the same moment could both read "on" and turn it off once.
func TestTogglesAtOnceAllCount(t *testing.T) {
	m := NewManager(Options{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				m.toggle()
				m.toggleLearning()
			}
		}()
	}
	wg.Wait()
	if !m.Active() || m.Learning() {
		t.Errorf("after 2000 toggles: active %v, learning %v", m.Active(), m.Learning())
	}
}
