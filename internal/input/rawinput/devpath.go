package rawinput

import (
	"strconv"
	"strings"
)

// identity is what the interface path of a device says about it.
type identity struct {
	vendor, product uint16
	// xbox is set for a controller Windows also offers to games through
	// its interface for Xbox controllers, which the path marks with IG_.
	xbox bool
	// group is the same for all the interfaces of one piece of hardware
	// and different for another piece of the same model.
	group string
}

// parsePath reads a device interface path as Raw Input reports it, such as
//
//	\\?\HID#VID_046D&PID_C31C&MI_00#7&2a1b3c4d&0&0000#{884b96c3-...}
//
// A keyboard often has several interfaces, one for its keys and one for
// its media keys. They differ in the MI_ and Col parts and in the last
// number of the instance, and share the rest, which is what group keeps.
// For a path that does not have this form, such as that of a Bluetooth or
// a built-in keyboard, the group is the whole path.
func parsePath(path string) identity {
	id := identity{group: strings.ToUpper(path)}
	parts := strings.Split(path, "#")
	if len(parts) < 3 {
		return id
	}
	var kept []string
	for _, field := range strings.Split(strings.ToUpper(parts[1]), "&") {
		switch {
		case strings.HasPrefix(field, "VID_"):
			id.vendor = hex16(field[4:])
		case strings.HasPrefix(field, "PID_"):
			id.product = hex16(field[4:])
		case strings.HasPrefix(field, "IG_"):
			id.xbox = true
			continue
		case strings.HasPrefix(field, "MI_"), strings.HasPrefix(field, "COL"):
			continue // tells the interfaces of one device apart
		}
		kept = append(kept, field)
	}
	// The instance is "7&2a1b3c4d&0&0000": everything before the last
	// part names the place the hardware is plugged in.
	instance := strings.ToUpper(parts[2])
	if i := strings.LastIndex(instance, "&"); i >= 0 {
		instance = instance[:i]
	}
	id.group = strings.Join(kept, "&") + "#" + instance
	return id
}

// hex16 reads up to four hexadecimal digits at the start of s.
func hex16(s string) uint16 {
	if len(s) > 4 {
		s = s[:4]
	}
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0
	}
	return uint16(v)
}
