package control

import "fmt"

// DeviceType is the broad class of an input device, used to choose a default
// configuration.
type DeviceType uint8

const (
	// DeviceUnknown is a device HIDI cannot classify.
	DeviceUnknown DeviceType = iota
	// DeviceKeyboard is a keyboard, including one with an integrated
	// pointing device.
	DeviceKeyboard
	// DeviceGamepad is a game controller: a gamepad, joystick, throttle or
	// wheel.
	DeviceGamepad
	// DeviceMouse is a pointing device with no keyboard.
	DeviceMouse
)

func (t DeviceType) String() string {
	switch t {
	case DeviceKeyboard:
		return "keyboard"
	case DeviceGamepad:
		return "gamepad"
	case DeviceMouse:
		return "mouse"
	}
	return "unknown"
}

// HardwareID is the identity a device model reports about itself. Two devices
// of the same model report the same HardwareID.
type HardwareID struct {
	// Bus is the bus type, using the Linux BUS_ constants (3 is USB, 5 is
	// Bluetooth).
	Bus uint16
	// Vendor and Product identify the model.
	Vendor, Product uint16
	// Version is the device or firmware version.
	Version uint16
}

// String formats the ID as "bus:vendor:product:version" in hexadecimal, for
// example "0003:054c:09cc:8111".
func (id HardwareID) String() string {
	return fmt.Sprintf("%04x:%04x:%04x:%04x", id.Bus, id.Vendor, id.Product, id.Version)
}

// DeviceInfo describes a connected input device.
type DeviceInfo struct {
	// Hardware identifies the device model.
	Hardware HardwareID
	// Name is the name the device reports.
	Name string
	// Serial distinguishes two devices of the same model. Most keyboards do
	// not report one, in which case it is empty.
	Serial string
	// Location identifies where the device is connected, such as a USB port
	// path. It is stable while the device stays plugged into the same port
	// and is the only way to tell apart identical devices without a serial.
	Location string
	// Type is the class of the device.
	Type DeviceType
}
