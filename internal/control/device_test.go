package control

import "testing"

func TestHardwareIDString(t *testing.T) {
	id := HardwareID{Bus: 3, Vendor: 0x054c, Product: 0x09cc, Version: 0x8111}
	if got, want := id.String(), "0003:054c:09cc:8111"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestDeviceTypeString(t *testing.T) {
	tests := map[DeviceType]string{
		DeviceUnknown: "unknown", DeviceKeyboard: "keyboard",
		DeviceGamepad: "gamepad", DeviceMouse: "mouse", DeviceType(99): "unknown",
	}
	for typ, want := range tests {
		if got := typ.String(); got != want {
			t.Errorf("DeviceType(%d).String() = %q, want %q", typ, got, want)
		}
	}
}

func TestEventPressed(t *testing.T) {
	tests := map[float64]bool{0: false, 0.49: false, 0.5: true, 1: true}
	for value, want := range tests {
		if got := (Event{Value: value}).Pressed(); got != want {
			t.Errorf("Event{Value: %v}.Pressed() = %v, want %v", value, got, want)
		}
	}
}

func TestParseHardwareID(t *testing.T) {
	id := HardwareID{Bus: 3, Vendor: 0x054c, Product: 0x09cc, Version: 0x8111}
	if got, err := ParseHardwareID(id.String()); err != nil || got != id {
		t.Errorf("ParseHardwareID(%q) = %+v, %v", id.String(), got, err)
	}
	for _, bad := range []string{"", "0003:054c:09cc", "0003:054c:09cc:8111:0", "0003:054c:09cc:zzzz", "0003:054c:09cc:18111"} {
		if got, err := ParseHardwareID(bad); err == nil {
			t.Errorf("ParseHardwareID(%q) = %+v, want an error", bad, got)
		}
	}
	for _, typ := range []DeviceType{DeviceKeyboard, DeviceGamepad, DeviceMouse, DeviceUnknown} {
		if got := ParseDeviceType(typ.String()); got != typ {
			t.Errorf("ParseDeviceType(%q) = %v", typ.String(), got)
		}
	}
	if ParseDeviceType("spaceship") != DeviceUnknown {
		t.Error("an unknown kind of device was read as a known one")
	}
}
