package instrument

import (
	"errors"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/midi"
)

func TestStress(t *testing.T) {
	oldR, oldH, oldT, oldP := RetryOutput, healthPeriod, TapHold, ratePeriod
	RetryOutput, healthPeriod, TapHold, ratePeriod = 3*time.Millisecond, 3*time.Millisecond, 2*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { RetryOutput, healthPeriod, TapHold, ratePeriod = oldR, oldH, oldT, oldP })

	before := runtime.NumGoroutine()
	var lmu sync.Mutex
	r := newRigWith(t, Options{Grab: func(control.DeviceInfo) bool { return true }, Toggle: toggleKeys,
		Changed: func() {},
		Lights: func(info control.DeviceInfo) Light {
			lmu.Lock()
			defer lmu.Unlock()
			return &fakeLight{}
		}}, profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "C3"))
	var devs []*passingDevice
	for i := 0; i < 4; i++ {
		d := &passingDevice{fakeDevice: newFakeDevice("Kbd", control.DeviceKeyboard)}
		if i == 3 {
			d.info.Type = control.DeviceUnknown
			d.info.Hardware = control.HardwareID{Vendor: 0x1234, Product: 1}
		}
		devs = append(devs, d)
		r.devices <- d
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	run := func(f func(rng *rand.Rand)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for {
				select {
				case <-stop:
					return
				default:
				}
				f(rng)
			}
		}()
	}
	for _, d := range devs {
		d := d
		run(func(rng *rand.Rand) {
			code := control.Key(uint16(40 + rng.Intn(10)))
			if rng.Intn(5) == 0 {
				code = toggleKeys[rng.Intn(len(toggleKeys))]
			}
			select {
			case d.events <- control.Event{Code: code, Value: float64(rng.Intn(2))}:
			case <-time.After(time.Millisecond):
			}
		})
	}
	run(func(rng *rand.Rand) { r.manager.SetActive(rng.Intn(2) == 0); time.Sleep(time.Millisecond) })
	run(func(rng *rand.Rand) {
		if rng.Intn(2) == 0 {
			r.manager.SetProfiles(builtin(t))
		} else {
			r.manager.SetProfiles(append(builtin(t), profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "D3")))
		}
		time.Sleep(2 * time.Millisecond)
	})
	run(func(rng *rand.Rand) {
		switch rng.Intn(3) {
		case 0:
			r.manager.SetInput(toggleKeys, func(control.DeviceInfo) bool { return true })
			r.manager.SetToggleTap(false, false)
		case 1:
			r.manager.SetInput([]control.Code{keyZ}, func(control.DeviceInfo) bool { return true })
			r.manager.SetToggleTap(true, true)
		case 2:
			r.manager.SetToggleTap(true, false)
		}
		time.Sleep(2 * time.Millisecond)
	})
	run(func(rng *rand.Rand) { r.manager.Status(); r.manager.Active() })
	run(func(rng *rand.Rand) {
		// break endpoints, feed external notes
		r.mu.Lock()
		for _, e := range r.endpoints {
			e.mu.Lock()
			if rng.Intn(4) == 0 {
				e.sendErr = errors.New("broken")
			}
			e.mu.Unlock()
			select {
			case e.incoming <- []midi.Message{midi.NoteOn(1, 60, midi.From7(100))}:
			default:
			}
		}
		if rng.Intn(3) == 0 {
			r.openErr = errors.New("away")
		} else {
			r.openErr = nil
		}
		r.mu.Unlock()
		time.Sleep(3 * time.Millisecond)
	})
	time.Sleep(3 * time.Second)
	close(stop)
	wg.Wait()
	// Settle and check that nothing is stuck.
	r.mu.Lock()
	r.openErr = nil
	for _, e := range r.endpoints {
		e.mu.Lock()
		e.sendErr = nil
		e.mu.Unlock()
	}
	r.mu.Unlock()
	r.manager.SetInput(toggleKeys, func(control.DeviceInfo) bool { return true })
	r.manager.SetToggleTap(false, false)
	r.manager.SetProfiles(append(builtin(t), profile(t, "Pedal", "vendor = 0x1234\nproduct = 1", "D3")))
	for _, d := range devs {
		for len(d.events) > 0 {
			time.Sleep(time.Millisecond)
		}
		for _, k := range toggleKeys {
			d.events <- control.Event{Code: k, Value: 0}
		}
		for k := 40; k < 50; k++ {
			d.events <- control.Event{Code: control.Key(uint16(k)), Value: 0}
		}
	}
	time.Sleep(300 * time.Millisecond)
	r.manager.SetActive(false)
	time.Sleep(100 * time.Millisecond)
	for i, d := range devs {
		if d.isGrabbed() || d.isPassing() {
			t.Errorf("device %d: HIDI is off but grabbed=%v passing=%v", i, d.isGrabbed(), d.isPassing())
		}
	}
	r.manager.SetActive(true)
	time.Sleep(500 * time.Millisecond)
	if n := len(r.manager.Status()); n != 4 {
		r.manager.mu.Lock()
		for rr := range r.manager.running {
			rr.mu.Lock()
			t.Logf("device type=%v assigned=%v instrument=%v pendingNil=%v", rr.info.Type, rr.assigned != nil, rr.instrument, rr.pending.Load() == nil)
			rr.mu.Unlock()
		}
		r.manager.mu.Unlock()
		t.Fatalf("%d instruments, want 4; open failures %d", n, r.openFailures())
	}
	time.Sleep(100 * time.Millisecond)
	for i, d := range devs {
		if !d.isGrabbed() {
			t.Errorf("device %d: HIDI is on but the device is not grabbed", i)
		}
	}
	// the toggle combination on device 0 turns HIDI off
	for _, k := range toggleKeys {
		devs[0].events <- control.Event{Code: k, Value: 1}
	}
	eventually(t, "toggle off", func() bool { return !r.manager.Active() })
	time.Sleep(100 * time.Millisecond)
	for i, d := range devs {
		if d.isGrabbed() || d.isPassing() {
			t.Errorf("device %d: toggled off but grabbed=%v passing=%v", i, d.isGrabbed(), d.isPassing())
		}
	}
	r.stop()
	time.Sleep(200 * time.Millisecond)
	for _, d := range devs {
		if d.isGrabbed() {
			t.Errorf("a device is still grabbed after stop (fake Close does not release; informational)")
		}
	}
	t.Logf("goroutines before %d, after %d", before, runtime.NumGoroutine())
}
