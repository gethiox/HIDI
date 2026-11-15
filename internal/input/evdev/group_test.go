//go:build linux

package evdev

import (
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
)

func keyCodes(nums ...uint16) []control.Code {
	codes := make([]control.Code, len(nums))
	for i, n := range nums {
		codes[i] = control.Key(n)
	}
	return codes
}

func axisCodes(nums ...uint16) []control.Code {
	codes := make([]control.Code, len(nums))
	for i, n := range nums {
		codes[i] = control.Axis(n)
	}
	return codes
}

// typingKeys returns the codes of the first n keyboard keys.
func typingKeys(n uint16) []control.Code {
	nums := make([]uint16, n)
	for i := range nums {
		nums[i] = uint16(i) + 1
	}
	return keyCodes(nums...)
}

func TestLocation(t *testing.T) {
	tests := map[string]string{
		"usb-0000:00:14.0-2/input0":   "usb-0000:00:14.0-2",
		"usb-0000:00:14.0-2/input12":  "usb-0000:00:14.0-2",
		"usb-0000:00:14.0-2.1/input1": "usb-0000:00:14.0-2.1",
		"aa:bb:cc:dd:ee:ff":           "aa:bb:cc:dd:ee:ff", // Bluetooth
		"isa0060/serio0/input0":       "isa0060/serio0",
		"usb-0000:00:14.0-2/inputfoo": "usb-0000:00:14.0-2/inputfoo",
		"":                            "",
	}
	for phys, want := range tests {
		if got := location(phys); got != want {
			t.Errorf("location(%q) = %q, want %q", phys, got, want)
		}
	}
}

func TestGroupKey(t *testing.T) {
	usb := control.HardwareID{Bus: 3, Vendor: 1, Product: 2}
	base := Info{Path: "/dev/input/event3", Hardware: usb, Phys: "usb-1/input0"}

	same := base
	same.Path, same.Phys = "/dev/input/event4", "usb-1/input1"
	if groupKeyOf(base) != groupKeyOf(same) {
		t.Error("two interfaces of one USB device are not grouped")
	}

	otherPort := base
	otherPort.Phys = "usb-2/input0"
	if groupKeyOf(base) == groupKeyOf(otherPort) {
		t.Error("identical devices on different ports are grouped")
	}

	otherModel := base
	otherModel.Hardware.Product = 9
	if groupKeyOf(base) == groupKeyOf(otherModel) {
		t.Error("different models at the same location are grouped")
	}

	// Bluetooth devices share the adapter's address as their location and
	// differ in their unique id.
	bt1 := Info{Path: "/dev/input/event5", Phys: "adapter", Uniq: "aa"}
	bt2 := Info{Path: "/dev/input/event6", Phys: "adapter", Uniq: "bb"}
	if groupKeyOf(bt1) == groupKeyOf(bt2) {
		t.Error("two Bluetooth devices on one adapter are grouped")
	}

	// Nodes that report nothing to match on stand alone.
	anon1 := Info{Path: "/dev/input/event7"}
	anon2 := Info{Path: "/dev/input/event8"}
	if groupKeyOf(anon1) == groupKeyOf(anon2) {
		t.Error("nodes without a location or unique id are grouped")
	}
}

func TestSortNodes(t *testing.T) {
	infos := []Info{{Path: "/dev/input/event10"}, {Path: "/dev/input/event9"}, {Path: "/dev/input/event2"}}
	sortNodes(infos)
	got := []string{infos[0].Path, infos[1].Path, infos[2].Path}
	if want := []string{"/dev/input/event2", "/dev/input/event9", "/dev/input/event10"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v (numeric, not alphabetical)", got, want)
	}
}

func TestNodeType(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want control.DeviceType
	}{
		{"keyboard", Info{Keys: typingKeys(100)}, control.DeviceKeyboard},
		{"macro pad", Info{Keys: typingKeys(minKeyboardKeys)}, control.DeviceKeyboard},
		{"power button", Info{Keys: keyCodes(116)}, control.DeviceUnknown},
		{"media keys only", Info{Keys: typingKeys(minKeyboardKeys - 1)}, control.DeviceUnknown},
		{"mouse", Info{Keys: keyCodes(0x110, 0x111, 0x112), Relative: true}, control.DeviceMouse},
		{"touchpad", Info{Keys: keyCodes(0x110, 0x14a), Axes: axisCodes(0, 1)}, control.DeviceUnknown},
		{"gamepad", Info{Keys: keyCodes(0x130, 0x131), Axes: axisCodes(0, 1)}, control.DeviceGamepad},
		{"joystick", Info{Keys: keyCodes(0x120), Axes: axisCodes(0, 1)}, control.DeviceGamepad},
		{"throttle with extra buttons", Info{Keys: keyCodes(0x2c0)}, control.DeviceGamepad},
		{"motion sensors", Info{Axes: axisCodes(0, 1, 2)}, control.DeviceUnknown},
		{"nothing", Info{}, control.DeviceUnknown},
	}
	for _, tt := range tests {
		if got := nodeType(tt.info); got != tt.want {
			t.Errorf("%s: nodeType = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDeviceType(t *testing.T) {
	keyboard := Info{Keys: typingKeys(100)}
	mouse := Info{Keys: keyCodes(0x110), Relative: true}
	media := Info{Keys: keyCodes(113, 114, 115)}
	pad := Info{Keys: keyCodes(0x130), Axes: axisCodes(0, 1)}
	sensors := Info{Axes: axisCodes(0, 1, 2)}

	tests := []struct {
		name  string
		nodes []Info
		want  control.DeviceType
	}{
		{"keyboard with media keys", []Info{keyboard, media}, control.DeviceKeyboard},
		{"keyboard with a pointing stick", []Info{keyboard, mouse}, control.DeviceKeyboard},
		// A mouse that presents a keyboard for its extra buttons must not
		// be taken for a keyboard: grabbing it would take the pointer away.
		{"mouse with a keyboard interface", []Info{mouse, keyboard}, control.DeviceMouse},
		{"controller with sensors", []Info{pad, sensors}, control.DeviceGamepad},
		{"unclassifiable first node", []Info{media, keyboard}, control.DeviceKeyboard},
		{"nothing recognisable", []Info{media, sensors}, control.DeviceUnknown},
		{"no nodes", nil, control.DeviceUnknown},
	}
	for _, tt := range tests {
		if got := deviceType(tt.nodes); got != tt.want {
			t.Errorf("%s: deviceType = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPartNames(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Info
		want  []string
	}{
		{
			"keyboard: extra key nodes join the main part",
			[]Info{
				{Name: "ACME Keyboard", Keys: typingKeys(100)},
				{Name: "ACME Keyboard Consumer Control", Keys: keyCodes(113, 114, 115)},
				{Name: "ACME Keyboard Keyboard", Keys: typingKeys(100)}, // an N-key rollover interface
			},
			[]string{"", "", ""},
		},
		{
			"controller: nodes repeating an axis become parts",
			[]Info{
				{Name: "Wireless Controller", Keys: keyCodes(0x130), Axes: axisCodes(0, 1, 3, 4)},
				{Name: "Wireless Controller Touchpad", Keys: keyCodes(0x110), Axes: axisCodes(0, 1)},
				{Name: "Wireless Controller Motion Sensors", Axes: axisCodes(0, 1, 2)},
			},
			[]string{"", "Touchpad", "Motion Sensors"},
		},
		{
			"axes that do not collide join the main part",
			[]Info{
				{Name: "Stick", Axes: axisCodes(0, 1)},
				{Name: "Stick Throttle", Axes: axisCodes(2)},
			},
			[]string{"", ""},
		},
		{
			"parts without a distinct name are numbered",
			[]Info{
				{Name: "Pad", Axes: axisCodes(0)},
				{Name: "Pad", Axes: axisCodes(0)},
				{Name: "Pad Extra", Axes: axisCodes(0)},
				{Name: "Pad Extra", Axes: axisCodes(0)},
			},
			[]string{"", "part 2", "Extra", "part 4"},
		},
		{"no nodes", nil, []string{}},
	}
	for _, tt := range tests {
		if got := partNames(tt.nodes); !slices.Equal(got, tt.want) {
			t.Errorf("%s: partNames = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDescribeDevice(t *testing.T) {
	hw := control.HardwareID{Bus: 5, Vendor: 0x054c, Product: 0x09cc, Version: 0x8111}
	got := describeDevice([]Info{
		{Name: "Wireless Controller", Hardware: hw, Phys: "adapter", Keys: keyCodes(0x130)},
		{Name: "Wireless Controller Touchpad", Hardware: hw, Phys: "adapter", Uniq: "aa:bb"},
	})
	want := control.DeviceInfo{
		Hardware: hw, Name: "Wireless Controller", Serial: "aa:bb",
		Location: "adapter", Type: control.DeviceGamepad,
	}
	if got != want {
		t.Errorf("describeDevice = %+v, want %+v", got, want)
	}
	if got := describeDevice(nil); got != (control.DeviceInfo{}) {
		t.Errorf("describeDevice(nil) = %+v, want the zero value", got)
	}
}

func TestIsEventNode(t *testing.T) {
	for name, want := range map[string]bool{
		"event0": true, "event17": true, "event": false, "event1a": false,
		"mouse0": false, "js0": false, "by-id": false, "": false,
	} {
		if got := isEventNode(name); got != want {
			t.Errorf("isEventNode(%q) = %v, want %v", name, got, want)
		}
	}
}
