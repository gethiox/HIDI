package input

import (
	"fmt"
	"strings"

	"github.com/holoplot/go-evdev"
)

type PhysicalID string
type HandlerType int

const (
	DI_TYPE_UNKNOWN    = HandlerType(iota)
	DI_TYPE_STD_KBD    // standard keyboard 6KRO mode
	DI_TYPE_NKRO_KBD   // N-Key Rollover mode
	DI_TYPE_MULTIMEDIA // Multimedia events, e.g. next track, volume up
	DI_TYPE_SYSTEM     // System events, e.g. sleep, power
	DI_TYPE_MOUSE
	DI_TYPE_JOYSTICK
)

func (ht HandlerType) String() string {
	switch ht {
	case DI_TYPE_STD_KBD:
		return "STD_KBD"
	case DI_TYPE_NKRO_KBD:
		return "NKRO_KBD"
	case DI_TYPE_MULTIMEDIA:
		return "MULTIMEDIA"
	case DI_TYPE_SYSTEM:
		return "SYSTEM"
	case DI_TYPE_MOUSE:
		return "MOUSE"
	case DI_TYPE_JOYSTICK:
		return "JOYSTICK"
	default:
		return "UNKNOWN"
	}
}

// DeviceInfo contains information of every reported event device
// it is supposed to be created by unmarshal function only
type DeviceInfo struct {
	ID    InputID // ID of the device
	Name  string  // name of the device
	Phys  string  // physical path to the device in the system hierarchy
	Sysfs string  // sysfs path
	Uniq  string  // unique identification code for the device (if device has it)

	eventName    string
	CapableTypes []evdev.EvType
	Properties   []evdev.EvProp
}

type InputID struct {
	Bus     uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

func (i *InputID) String() string {
	return fmt.Sprintf("0x%4x 0x%4x 0x%4x 0x%4x", i.Bus, i.Vendor, i.Product, i.Version)
}

// Event returns event name, like "event0" for /dev/input/event0
func (d *DeviceInfo) Event() string {
	return d.eventName
}

// EventPath returns a /dev/input/event filepath for button presses
func (d *DeviceInfo) EventPath() string {
	event := d.Event()
	if event == "" {
		return ""
	}
	return fmt.Sprintf("/dev/input/%s", event)
}

func has(list []evdev.EvType, elem ...evdev.EvType) bool {
	toHave := map[evdev.EvType]bool{}
	for _, e := range elem {
		toHave[e] = false
	}
	have := map[evdev.EvType]bool{}
	for _, e := range list {
		have[e] = true
	}

	for e := range have {
		if _, ok := toHave[e]; ok {
			toHave[e] = true
		}
	}

	for _, v := range toHave {
		if !v {
			return false
		}
	}
	return true
}

func hasExactly(list []evdev.EvType, elem ...evdev.EvType) bool {
	toHave := map[evdev.EvType]bool{}
	for _, e := range elem {
		toHave[e] = false
	}
	have := map[evdev.EvType]bool{}
	for _, e := range list {
		have[e] = true
	}

	for e := range have {
		if _, ok := toHave[e]; ok {
			toHave[e] = true
		} else {
			return false
		}
	}

	for _, v := range toHave {
		if !v {
			return false
		}
	}
	return true
}

func (d *DeviceInfo) HandlerType() HandlerType {
	switch {
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_MSC, evdev.EV_LED, evdev.EV_REP):
		return DI_TYPE_STD_KBD
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_MSC, evdev.EV_REP):
		return DI_TYPE_NKRO_KBD
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_REL, evdev.EV_MSC):
		return DI_TYPE_MOUSE
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_REL, evdev.EV_ABS, evdev.EV_MSC, evdev.EV_LED, evdev.EV_REP):
		return DI_TYPE_MOUSE
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_MSC):
		return DI_TYPE_SYSTEM
	case hasExactly(d.CapableTypes, evdev.EV_SYN, evdev.EV_KEY, evdev.EV_REL, evdev.EV_ABS, evdev.EV_MSC):
		return DI_TYPE_MULTIMEDIA
	case has(d.CapableTypes, evdev.EV_FF):
		return DI_TYPE_JOYSTICK
	}
	return DI_TYPE_UNKNOWN
}

// PhysicalUUID returns unique UUID based on connection of given USB port
// The main usage is to identify groups of handlers that represent one physical device
func (d *DeviceInfo) PhysicalUUID() PhysicalID {
	phys := strings.Split(d.Phys, "/")
	return PhysicalID(phys[0])
}
