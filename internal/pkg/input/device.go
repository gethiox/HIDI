package input

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/holoplot/go-evdev"
	"go.uber.org/zap"
)

// Collects all separate device-info handlers together for building one logical handler

type DeviceType int
type DeviceID string

// Generic device types
const (
	UnknownDevice  DeviceType = iota
	KeyboardDevice            // keyboard, including keyboard with integrated mouse
	MouseDevice               // mouse device only
	JoystickDevice            // joystick device, may contain keyboard, mouse, sensors events
)

type InputEvent struct {
	Source DeviceInfo
	Event  evdev.InputEvent
}

func (e DeviceType) String() string {
	switch e {
	case KeyboardDevice:
		return "Keyboard"
	case MouseDevice:
		return "Mouse"
	case JoystickDevice:
		return "Joystick"
	default:
		return "Unknown"
	}
}

func containsOnly(in map[HandlerType]DeviceInfo, handlerTypes ...HandlerType) bool {
	if len(in) != len(handlerTypes) {
		return false
	}

	for _, ht := range handlerTypes {
		_, ok := in[ht]
		if !ok {
			return false
		}
	}

	return true
}

func contains(in map[HandlerType]DeviceInfo, handlerTypes ...HandlerType) bool {
	for _, ht := range handlerTypes {
		_, ok := in[ht]
		if !ok {
			return false
		}
	}
	return true
}

func DetermineDeviceType(handlers map[HandlerType]DeviceInfo) DeviceType {
	switch {
	case contains(handlers, DI_TYPE_JOYSTICK):
		return JoystickDevice
	case contains(handlers, DI_TYPE_STD_KBD, DI_TYPE_MULTIMEDIA, DI_TYPE_SYSTEM):
		return KeyboardDevice
	case containsOnly(handlers, DI_TYPE_MOUSE):
		return MouseDevice
	default:
		return UnknownDevice
	}
}

// Normalize processes all DeviceInfo list and returns generic devices with its underlying DeviceInfo handlers
func Normalize(deviceInfos []DeviceInfo) []Device {
	var collection = make(map[PhysicalID][]DeviceInfo, 0)
	// var collectionOrder = make([]PhysicalID, 0)

	for _, di := range deviceInfos {
		key := di.PhysicalUUID()
		collection[key] = append(collection[key], di)
	}

	var devices = make([]Device, 0)

	for devPhys, dis := range collection {
		var dev = Device{
			ID:       dis[0].ID,
			Handlers: make(map[HandlerType]DeviceInfo),
			Evdevs:   make(map[HandlerType]*evdev.InputDevice), // TODO: tests are failing because of that
		}

		var name = ""
		var uniq = ""

		for _, di := range dis {
			switch {
			case name == "":
				name = di.Name
			case len(di.Name) < len(name):
				name = di.Name
			}

			if di.Uniq != "" && uniq == "" {
				uniq = di.Uniq
			}

			v, ok := dev.Handlers[di.HandlerType()]
			if ok {
				panic(fmt.Errorf("handler already exist: %+v (want to overwrite by: %+v)", v, di))
			}

			dev.Handlers[di.HandlerType()] = di
		}

		dev.DeviceType = DetermineDeviceType(dev.Handlers)
		dev.Name = name
		dev.Uniq = uniq
		dev.Phys = string(devPhys)
		devices = append(devices, dev)
	}

	return devices
}

// Device is a representation of singular hardware device, it keeps all underlying DeviceInfo handlers
type Device struct {
	ID   InputID
	Name string
	Uniq string
	// Phys is a common part of Handlers Phys
	// for example "usb-20980000.usb-1.4/input0" will be used as "usb-20980000.usb-1.4"
	Phys string

	DeviceType DeviceType
	Handlers   map[HandlerType]DeviceInfo

	Evdevs map[HandlerType]*evdev.InputDevice
}

func (d *Device) String() string {
	return fmt.Sprintf(
		"[%s], \"%s\", %d handlers (0x%04x, 0x%04x, 0x%04x, 0x%04x, \"%s\")",
		d.DeviceType, d.Name, len(d.Handlers), d.ID.Bus, d.ID.Vendor, d.ID.Product, d.ID.Version, d.Uniq,
	)
}

// SupportsNKRO tells if device has N-Key rollover handler
func (d *Device) SupportsNKRO() bool {
	_, ok := d.Handlers[DI_TYPE_NKRO_KBD]
	return ok
}

// DeviceID returns unique UUID for every device as much as possible, regardless of its connection source.
// Vast amount of devices (especially keyboards) doesn't provide unique identifiers, so often it is
// impossible to distinguish between two the vert same types of devices.
// Sometimes (eg. dualshock 4, steam controller) device provide such information, so handling of separate configurations
// for those should be possible
func (d *Device) DeviceID() DeviceID {
	s := fmt.Sprintf("%04x%04x%04x%04x%s", d.ID.Bus, d.ID.Vendor, d.ID.Product, d.ID.Version, d.Uniq)
	return DeviceID(s)
}

func (d *Device) PhysicalUUID() PhysicalID {
	return PhysicalID(d.Phys)
}

func (d *Device) ProcessEvents(ctx context.Context, grab bool, absThrottle time.Duration) (<-chan InputEvent, error) {
	var events = make(chan InputEvent)

	wg := sync.WaitGroup{}
	for ht, h := range d.Handlers {
		dev, err := evdev.Open(h.EventPath())
		if err != nil {
			return nil, fmt.Errorf("opening handler failed: %v", err)
		}

		d.Evdevs[ht] = dev

		go func(dev *evdev.InputDevice) {
			<-ctx.Done()
			err := dev.Close()
			if err != nil {
				fmt.Printf("[%s] device close failed: %v\n", dev.Path(), err)
			}
		}(dev)

		absEvents := make(chan InputEvent, 64)
		go func(absEvents chan InputEvent) {
			var locks = make(map[evdev.EvCode]*sync.Mutex)
			var timers = make(chan evdev.EvCode, 64)
			var lastSentEvent = make(map[evdev.EvCode]InputEvent)
			var lastThrottledEvent = make(map[evdev.EvCode]InputEvent)
			var lastSent = make(map[evdev.EvCode]time.Time)
			var timerMap = make(map[evdev.EvCode]*time.Timer)

			defer close(timers)

			for _, abs := range evdev.ABSFromString {
				locks[abs] = &sync.Mutex{}
				// timers are set with a little of additional headroom which should prevent
				// from firing when analog axis is in active use (not guarantee it tho, but it's not critical anyway)
				// this way it should prevent the 99.9% cases of firing the same event twice at the time
				timerMap[abs] = time.NewTimer(absThrottle + time.Millisecond*20)
				lastSent[abs] = time.Now().Add(absThrottle * 2 * -1)
			}

			go func() {
				for evCode := range timers {
					go func(evCode evdev.EvCode) {
						for {
							select {
							case <-ctx.Done():
								return
							case <-timerMap[evCode].C:
								break
							}
							locks[evCode].Lock()
							event := lastThrottledEvent[evCode]
							if event.Event.Value != lastSentEvent[evCode].Event.Value {
								events <- event
							}
							locks[evCode].Unlock()
						}
					}(evCode)
				}
			}()

			var encounteredEvCodes = make(map[evdev.EvCode]bool, 16)

			for ev := range absEvents {
				now := time.Now()
				last := lastSent[ev.Event.Code]

				if now.Sub(last) > absThrottle {
					// throttle period exceeded, event can be sent
					_, ok := encounteredEvCodes[ev.Event.Code]
					if !ok {
						// this is first time this EvCode appear, need to spin goroutine for it
						timers <- ev.Event.Code
						encounteredEvCodes[ev.Event.Code] = true
					}

					// but before that, timer has to be reset
					timerMap[ev.Event.Code].Reset(absThrottle + time.Millisecond*20)
					// doesn't matter if the timer already fired or not

					locks[ev.Event.Code].Lock()
					lastSent[ev.Event.Code] = now
					lastSentEvent[ev.Event.Code] = ev
					lastThrottledEvent[ev.Event.Code] = ev
					locks[ev.Event.Code].Unlock()
					events <- ev
				} else {
					// throttled
					locks[ev.Event.Code].Lock()
					lastThrottledEvent[ev.Event.Code] = ev
					timerMap[ev.Event.Code].Reset(absThrottle + time.Millisecond*20)
					locks[ev.Event.Code].Unlock()
				}
			}
		}(absEvents)

		wg.Add(1)
		go func(dev *evdev.InputDevice, ht HandlerType, info DeviceInfo, absEvents chan InputEvent) {
			event := info.Event()
			name, _ := dev.Name()
			name = strings.Trim(name, "\x00") // TODO: fix in go-evdev
			defer wg.Done()
			defer close(absEvents)

			if grab {
				_ = dev.Grab()
				log.Info("Grabbing device for exclusive usage", zap.String("handler_event", event), zap.String("handler_name", name), logger.Debug)
			}
			log.Info("Reading input events", zap.String("handler_event", event), zap.String("handler_name", name), logger.Debug)

			err = dev.NonBlock()
			if err != nil {
				log.Info(fmt.Sprintf("enabling non-blocking event reading mode failed: %v", err),
					zap.String("handler_event", event), zap.String("handler_name", name),
					logger.Warning,
				)
			}
			for {
				event, err := dev.ReadOne()
				if err != nil {
					break
				}

				if event.Type == evdev.EV_KEY && event.Value == 2 { // repeat
					continue
				}

				outputEvent := InputEvent{
					Source: info,
					Event:  *event,
				}

				if event.Type == evdev.EV_ABS {
					// throttling
					absEvents <- outputEvent
					continue
				}
				events <- outputEvent
			}
			if grab {
				log.Info("Ungrabbing device", zap.String("handler_event", event), zap.String("handler_name", name), logger.Debug)
				_ = dev.Ungrab()
			}
			log.Info("Reading input events finished", zap.String("handler_event", event), zap.String("handler_name", name), logger.Debug)
		}(dev, ht, h, absEvents)
	}

	go func() {
		wg.Wait()
		log.Info(fmt.Sprintf("All handlers done, closing events channel"), logger.Debug)
		close(events)
	}()

	return events, nil
}
