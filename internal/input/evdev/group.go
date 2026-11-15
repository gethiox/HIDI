//go:build linux

package evdev

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gethiox/HIDI/internal/control"
)

// The kernel creates one event node for every interface a piece of hardware
// presents. A keyboard commonly has a second node for its media keys, and a
// game controller may have separate nodes for a touchpad and motion sensors.
// The functions in this file decide which nodes belong together and what
// kind of device they form. They are pure, so the rules can be tested
// without devices.

// Key code ranges from linux/input-event-codes.h.
const (
	keyTyping     = 0x100 // codes below this are keyboard keys
	btnLeft       = 0x110 // the primary mouse button
	btnJoystick   = 0x120 // start of the joystick and gamepad buttons
	btnDigi       = 0x140 // end of the joystick and gamepad buttons
	btnTriggerMin = 0x2c0 // start of the extra joystick buttons
	btnTriggerMax = 0x2e7 // end of the extra joystick buttons

	// minKeyboardKeys is how many keyboard keys a node must have to count
	// as a keyboard. Power buttons, headsets and mice expose a handful of
	// keys; macro pads and keyboards expose many.
	minKeyboardKeys = 10
)

// groupKey identifies the hardware a node belongs to. Two nodes with equal
// keys are interfaces of the same device.
type groupKey struct {
	hardware control.HardwareID
	location string
	uniq     string
	// path is set only for a node that reports neither a location nor a
	// unique id. Such a node cannot be matched to others and stands alone.
	path string
}

func groupKeyOf(info Info) groupKey {
	key := groupKey{hardware: info.Hardware, location: location(info.Phys), uniq: info.Uniq}
	if key.location == "" && key.uniq == "" {
		key.path = info.Path
	}
	return key
}

// location strips the interface suffix from a physical location:
// "usb-0000:00:14.0-2/input1" becomes "usb-0000:00:14.0-2". The interfaces
// of one USB device differ only in that suffix. Bluetooth devices report the
// address of the host adapter here and are told apart by their unique id.
func location(phys string) string {
	if i := strings.LastIndex(phys, "/input"); i >= 0 {
		if _, err := strconv.Atoi(phys[i+len("/input"):]); err == nil {
			return phys[:i]
		}
	}
	return phys
}

// nodeNumber returns the number in a path like /dev/input/event12, or a
// large number for other paths so that they sort last.
func nodeNumber(path string) int {
	if i := strings.LastIndex(path, "event"); i >= 0 {
		if n, err := strconv.Atoi(path[i+len("event"):]); err == nil {
			return n
		}
	}
	return 1 << 30
}

// sortNodes orders infos by node number. The kernel numbers the interfaces
// of a device in order, so the main interface comes first.
func sortNodes(infos []Info) {
	slices.SortStableFunc(infos, func(a, b Info) int {
		return cmp.Or(cmp.Compare(nodeNumber(a.Path), nodeNumber(b.Path)), cmp.Compare(a.Path, b.Path))
	})
}

// nodeType classifies a single node by the controls it has.
func nodeType(info Info) control.DeviceType {
	keyboardKeys := 0
	var mouseButton, gameButton bool
	for _, key := range info.Keys {
		switch n := key.Num; {
		case n < keyTyping:
			keyboardKeys++
		case n == btnLeft:
			mouseButton = true
		case n >= btnJoystick && n < btnDigi, n >= btnTriggerMin && n <= btnTriggerMax:
			gameButton = true
		}
	}
	switch {
	case gameButton:
		return control.DeviceGamepad
	case info.Relative && mouseButton:
		return control.DeviceMouse
	case keyboardKeys >= minKeyboardKeys:
		return control.DeviceKeyboard
	}
	return control.DeviceUnknown
}

// deviceType classifies a device from its nodes, which must be sorted. The
// first node that can be classified decides: the main interface of a
// keyboard is its keyboard, even if it also has a pointing stick, and the
// main interface of a mouse is its pointer, even if it also presents a
// keyboard for its extra buttons.
func deviceType(infos []Info) control.DeviceType {
	for _, info := range infos {
		if t := nodeType(info); t != control.DeviceUnknown {
			return t
		}
	}
	return control.DeviceUnknown
}

// deviceName picks the name of a device from its nodes: the shortest one,
// because the others are usually that name with a suffix such as
// "Consumer Control" or "Touchpad".
func deviceName(infos []Info) string {
	name := ""
	for _, info := range infos {
		if info.Name != "" && (name == "" || len(info.Name) < len(name)) {
			name = info.Name
		}
	}
	return name
}

// partNames returns the part name for each node of a device, which must be
// sorted. See [control.Event] for what a part is.
//
// A node becomes a named part only if one of its axes is also an axis of the
// main part, because then the code alone no longer says which control moved:
// the touchpad of a game controller reports ABS_X just like its left stick.
// Nodes that only add keys, such as the media keys of a keyboard, join the
// main part, so that a profile can name every key the same way.
func partNames(infos []Info) []string {
	parts := make([]string, len(infos))
	if len(infos) == 0 {
		return parts
	}
	name := deviceName(infos)
	mainAxes := map[control.Code]bool{}
	used := map[string]bool{"": true}
	for i, info := range infos {
		collides := false
		for _, axis := range info.Axes {
			if mainAxes[axis] {
				collides = true
				break
			}
		}
		if !collides {
			for _, axis := range info.Axes {
				mainAxes[axis] = true
			}
			continue
		}

		part := strings.TrimSpace(strings.TrimPrefix(info.Name, name))
		if part == "" || used[part] {
			part = fmt.Sprintf("part %d", i+1)
		}
		used[part] = true
		parts[i] = part
	}
	return parts
}

// describe builds the description of a device from its nodes, which must be
// sorted.
func describeDevice(infos []Info) control.DeviceInfo {
	d := control.DeviceInfo{Name: deviceName(infos), Type: deviceType(infos)}
	if len(infos) > 0 {
		d.Hardware = infos[0].Hardware
		d.Location = location(infos[0].Phys)
	}
	for _, info := range infos {
		if info.Uniq != "" {
			d.Serial = info.Uniq
			break
		}
	}
	return d
}
