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
