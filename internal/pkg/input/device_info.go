package input

// Related things to separate handlers that comes from /proc/bus/input/devices

import (
	"fmt"
	"strings"

	"github.com/gethiox/go-evdev"
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
	ID       InputID  // ID of the device
	Name     string   // name of the device
	Phys     string   // physical path to the device in the system hierarchy
	Sysfs    string   // sysfs path
	Uniq     string   // unique identification code for the device (if device has it)
	Handlers []string // list of input handles associated with the device
	Bitmaps  Bitmaps
}

type InputID struct {
	Bus     uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type Bitmaps struct {
	// BITS_TO_LONGS/__KERNEL_DIV_ROUND_UP calculation for keeping original lengths of bitmaps,
	// kernel 5.14.15
	PROP [(evdev.INPUT_PROP_CNT + 32 - 1) / 32]uint32 // 1   // device properties and quirks
	EV   [(evdev.EV_CNT + 32 - 1) / 32]uint32         // 1   // types of events supported by the device
	KEY  [(evdev.KEY_CNT + 32 - 1) / 32]uint32        // 24  // keys/buttons this device has
	REL  [(evdev.REL_CNT + 32 - 1) / 32]uint32        // 1
	ABS  [(evdev.ABS_CNT + 32 - 1) / 32]uint32        // 2
	MSC  [(evdev.MSC_CNT + 32 - 1) / 32]uint32        // 1   // miscellaneous events supported by the device
	LED  [(evdev.LED_CNT + 32 - 1) / 32]uint32        // 1   // leds present on the device
	SND  [(evdev.SND_CNT + 32 - 1) / 32]uint32        // 1
	FF   [(evdev.FF_CNT + 32 - 1) / 32]uint32         // 4
	SW   [(evdev.SW_CNT + 32 - 1) / 32]uint32         // 1
}

// Event returns event name, like "event0" for /dev/input/event0
func (d *DeviceInfo) Event() string {
	for _, handler := range d.Handlers {
		if strings.HasPrefix(handler, "event") {
			return handler
		}
	}
	return ""
}

// EventPath returns a /dev/input/event filepath for button presses
func (d *DeviceInfo) EventPath() string {
	event := d.Event()
	if event == "" {
		return ""
	}
	return fmt.Sprintf("/dev/input/%s", event)
}

func (d *DeviceInfo) HandlerType() HandlerType {
	switch d.Bitmaps.EV[0] {
	case 0x120013:
		return DI_TYPE_STD_KBD
	case 0x100013:
		return DI_TYPE_NKRO_KBD
	case 0x17, 0x12001f: // std, mouse
		return DI_TYPE_MOUSE
	case 0x13:
		return DI_TYPE_SYSTEM
	case 0x1f:
		return DI_TYPE_MULTIMEDIA
	}

	for _, h := range d.Handlers {
		switch {
		case strings.HasPrefix(h, "js"):
			return DI_TYPE_JOYSTICK
		case strings.HasPrefix(h, "mouse"):
			return DI_TYPE_MOUSE
		}
	}

	return DI_TYPE_UNKNOWN
}

// PhysicalUUID returns unique UUID based on connection of given USB port
// The main usage is to identify groups of handlers that represent one physical device
func (d *DeviceInfo) PhysicalUUID() PhysicalID {
	phys := strings.Split(d.Phys, "/")
	return PhysicalID(phys[0])
}
