package control

import (
	"fmt"
	"strconv"
	"strings"
)

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

// ParseHardwareID reads an ID as [HardwareID.String] writes it.
func ParseHardwareID(s string) (HardwareID, error) {
	var id HardwareID
	parts := strings.Split(s, ":")
	if len(parts) != 4 {
		return id, fmt.Errorf("hardware ID %q is not bus:vendor:product:version", s)
	}
	for i, to := range []*uint16{&id.Bus, &id.Vendor, &id.Product, &id.Version} {
		n, err := strconv.ParseUint(parts[i], 16, 16)
		if err != nil {
			return HardwareID{}, fmt.Errorf("hardware ID %q: %q is not a number of up to four hexadecimal digits", s, parts[i])
		}
		*to = uint16(n)
	}
	return id, nil
}

// ParseDeviceType reads a kind of device as [DeviceType.String] writes
// it; anything else is [DeviceUnknown].
func ParseDeviceType(s string) DeviceType {
	for _, t := range []DeviceType{DeviceKeyboard, DeviceGamepad, DeviceMouse} {
		if t.String() == s {
			return t
		}
	}
	return DeviceUnknown
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
