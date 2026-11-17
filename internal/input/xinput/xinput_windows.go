package xinput

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/gethiox/HIDI/internal/control"
)

const (
	// slots is the number of controllers XInput knows.
	slots = 4

	errorSuccess = 0

	// pollInterval is how often a connected controller is asked for its
	// state, and probeInterval how often XInput is asked whether a new
	// controller appeared, which is a slower question.
	pollInterval  = 4 * time.Millisecond
	probeInterval = time.Second

	// Functions of the library that have a number and no name. The first
	// is XInputGetState with the guide button, the second tells the
	// model of a controller. Programs have relied on both for years.
	ordinalGetStateEx        = 100
	ordinalGetCapabilitiesEx = 108
)

// Structures from XInput.h.
type (
	xState struct {
		packet uint32
		pad    xGamepad
		// The function that also tells of the Guide button is not
		// documented, and is said to write four bytes more.
		_ uint32
	}
	xGamepad struct {
		buttons        uint16
		leftTrigger    uint8
		rightTrigger   uint8
		leftX, leftY   int16
		rightX, rightY int16
	}
	// xCapabilitiesEx is XINPUT_CAPABILITIES followed by what the
	// function without a name adds.
	xCapabilitiesEx struct {
		typ, subType uint8
		flags        uint16
		pad          xGamepad
		vibration    [2]uint16
		vendor       uint16
		product      uint16
		version      uint16
		_            uint16
		_            uint32
	}
)

// ErrClosed and ErrDisconnected are what [Device.Err] returns.
var (
	ErrClosed       = errors.New("the device was closed")
	ErrDisconnected = errors.New("the device was unplugged")
)

// library is XInput as far as HIDI uses it.
type library struct {
	getState uintptr
	getCaps  uintptr // 0 when the function is not there
}

// load finds XInput. Every Windows that HIDI runs on has it.
func load() (*library, error) {
	dll := windows.NewLazySystemDLL("xinput1_4.dll")
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("loading XInput: %w", err)
	}
	lib := &library{}
	if addr, err := windows.GetProcAddressByOrdinal(windows.Handle(dll.Handle()), ordinalGetStateEx); err == nil {
		lib.getState = addr
	} else {
		proc := dll.NewProc("XInputGetState")
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("loading XInput: %w", err)
		}
		lib.getState = proc.Addr()
	}
	if addr, err := windows.GetProcAddressByOrdinal(windows.Handle(dll.Handle()), ordinalGetCapabilitiesEx); err == nil {
		lib.getCaps = addr
	}
	return lib, nil
}

// state asks for the state of a controller; ok is false when there is
// none with that number.
func (l *library) state(slot int) (xState, bool) {
	var s xState
	r, _, _ := syscall.SyscallN(l.getState, uintptr(slot), uintptr(unsafe.Pointer(&s)))
	return s, uint32(r) == errorSuccess
}

// model asks which model a controller is; both are 0 when XInput does not
// say.
func (l *library) model(slot int) (vendor, product uint16) {
	if l.getCaps == 0 {
		return 0, 0
	}
	var caps xCapabilitiesEx
	if r, _, _ := syscall.SyscallN(l.getCaps, 1, uintptr(slot), 0, uintptr(unsafe.Pointer(&caps))); uint32(r) != errorSuccess {
		return 0, 0
	}
	return caps.vendor, caps.product
}

// Watcher finds the controllers XInput knows, those present when it starts
// and those connected afterwards, and reads them.
type Watcher struct {
	lib     *library
	devices chan *Device
	stop    chan struct{}
	done    chan struct{}
	wake    chan struct{} // a device was closed by its owner
	close   sync.Once
}

// Watch starts watching for controllers.
func Watch() (*Watcher, error) {
	lib, err := load()
	if err != nil {
		return nil, err
	}
	w := &Watcher{lib: lib, devices: make(chan *Device, slots), stop: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1)}
	go w.run()
	return w, nil
}

// Devices returns the channel new controllers arrive on. Whoever receives
// a device owns it and must close it. The channel is closed when the
// watcher is closed.
func (w *Watcher) Devices() <-chan *Device { return w.devices }

// Close stops the watcher and ends every device it delivered.
func (w *Watcher) Close() error {
	w.close.Do(func() {
		close(w.stop)
		select {
		case <-w.done:
		case <-time.After(3 * time.Second):
		}
	})
	return nil
}

func (w *Watcher) run() {
	defer close(w.done)
	defer close(w.devices)

	var (
		connected [slots]*Device
		// present is set while a controller answers, also one whose
		// device was closed: it is delivered again only after it left.
		present   [slots]bool
		lastProbe time.Time
	)
	// finish ends a device; only this goroutine sends on the channel of
	// a device, so only it may close it.
	finish := func(d *Device, reason error) {
		d.end(reason)
		close(d.events)
	}
	defer func() {
		for _, d := range connected {
			if d != nil {
				finish(d, ErrClosed)
			}
		}
	}()

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-w.wake:
		case <-timer.C:
		}
		now := time.Now()
		probe := now.Sub(lastProbe) >= probeInterval
		if probe {
			lastProbe = now
		}
		active := false
		for slot := range slots {
			d := connected[slot]
			if d != nil && d.Err() != nil {
				finish(d, ErrClosed) // closed by its owner
				connected[slot], d = nil, nil
			}
			if d == nil && !probe {
				continue
			}
			s, ok := w.lib.state(slot)
			switch {
			case !ok:
				present[slot] = false
				if d != nil {
					connected[slot] = nil
					finish(d, ErrDisconnected)
				}
			case d != nil:
				active = true
				if !d.update(s, now, w.stop) {
					return
				}
			case !present[slot]:
				present[slot] = true
				d = w.newDevice(slot)
				select {
				case w.devices <- d:
					connected[slot] = d
					active = true
					if !d.update(s, now, w.stop) {
						return
					}
				default:
					// Nobody takes devices; this one cannot be used.
					finish(d, ErrClosed)
				}
			}
		}
		if active {
			timer.Reset(pollInterval)
		} else {
			timer.Reset(probeInterval)
		}
	}
}

func (w *Watcher) newDevice(slot int) *Device {
	vendor, product := w.lib.model(slot)
	return &Device{
		events: make(chan control.Event, 256),
		ended:  make(chan struct{}),
		wake:   w.wake,
		first:  true,
		info: control.DeviceInfo{
			Hardware: control.HardwareID{Vendor: vendor, Product: product},
			Name:     "Xbox controller",
			Location: fmt.Sprintf("XInput %d", slot+1),
			Type:     control.DeviceGamepad,
		},
	}
}

// Device is one controller. It implements input.Device.
type Device struct {
	info   control.DeviceInfo
	events chan control.Event

	// The fields below belong to the watcher's goroutine.
	first  bool
	packet uint32
	last   state

	mu  sync.Mutex
	err error
	// ended is closed when the device ends, so that a reader that went
	// away does not hold up the watcher; wake tells the watcher.
	ended chan struct{}
	wake  chan<- struct{}
}

// update passes on what changed since the last state. It returns false
// when the watcher is to stop.
func (d *Device) update(s xState, at time.Time, stop <-chan struct{}) bool {
	if !d.first && s.packet == d.packet {
		return true // XInput counts the changes
	}
	next := state{
		buttons:     s.pad.buttons,
		leftTrigger: s.pad.leftTrigger, rightTrigger: s.pad.rightTrigger,
		leftX: s.pad.leftX, leftY: s.pad.leftY,
		rightX: s.pad.rightX, rightY: s.pad.rightY,
	}
	events := changes(d.last, next, d.first, at)
	if len(events) > cap(d.events)-len(d.events) {
		// Nobody takes this controller's input at the moment. Waiting
		// for room would keep every other controller from being read;
		// the difference is found again at the next look.
		return true
	}
	d.first, d.packet, d.last = false, s.packet, next
	for _, ev := range events {
		select {
		case d.events <- ev:
		case <-d.ended:
			return true
		case <-stop:
			return false
		}
	}
	return true
}

// end stops the device for the given reason, once. The watcher closes the
// channel of events.
func (d *Device) end(reason error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return
	}
	d.err = reason
	close(d.ended)
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Info describes the controller.
func (d *Device) Info() control.DeviceInfo { return d.info }

// Events returns the channel the controller's input arrives on. It is
// closed shortly after the device ended.
func (d *Device) Events() <-chan control.Event { return d.events }

// Err returns why the device ended, or nil while it runs.
func (d *Device) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// Grab does nothing: Windows cannot keep a controller from other programs.
func (d *Device) Grab() error { return d.Err() }

// Release does nothing.
func (d *Device) Release() error { return nil }

// Close stops delivering the controller's input. The controller is
// delivered again if it is disconnected and connected again.
func (d *Device) Close() error {
	d.end(ErrClosed)
	return nil
}
