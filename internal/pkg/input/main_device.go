package input

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/gethiox/go-evdev"
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

func (d *Device) ProcessEvents(ctx context.Context, grab bool) (<-chan *evdev.InputEvent, error) {
	var events = make(chan *evdev.InputEvent)

	wg := sync.WaitGroup{}
	for ht, h := range d.Handlers {
		dev, err := evdev.Open(h.EventPath())
		if err != nil {
			return events, fmt.Errorf("opening handler failed: %v", err)
		}

		d.Evdevs[ht] = dev

		go func(dev *evdev.InputDevice) {
			<-ctx.Done()
			dev.Close()
		}(dev)

		wg.Add(1)
		go func(dev *evdev.InputDevice, ht HandlerType, info DeviceInfo) {
			name, _ := dev.InputID()
			defer wg.Done()
			if grab {
				_ = dev.Grab()
				log.Printf("[%v] Grabbing device for exclusive usage", name)
			}
			log.Printf("[%v] Reading input events", name)
			for {
				event, err := dev.ReadOne()
				if err != nil {
					log.Printf("[%v] Reading input events error: %v", name, err)
					break
				}
				events <- event
			}
			if grab {
				log.Printf("[%v] Ungrabbing device", name)
				_ = dev.Ungrab()
			}
			log.Printf("[%v] Reading input events finished", name)
		}(dev, ht, h)
	}

	go func() {
		wg.Wait()
		close(events)
	}()

	return events, nil
}
