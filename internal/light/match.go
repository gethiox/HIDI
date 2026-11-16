package light

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gethiox/HIDI/internal/control"
)

// hidrawRoot is where Linux describes its raw HID devices.
var hidrawRoot = "/sys/class/hidraw"

var (
	hidrawNode = regexp.MustCompile(`/dev/(hidraw\d+)`)
	vendorPart = regexp.MustCompile(`(?i)vid[_&]([0-9a-f]{4})`)
	productID  = regexp.MustCompile(`(?i)pid[_&]([0-9a-f]{4})`)
)

// Matches reports whether the hardware OpenRGB reaches at a location is the
// input device described by info. OpenRGB says where it reaches a keyboard,
// not which input device that is, so the two are compared by model and, as
// far as the system tells, by where they are plugged in.
//
// On Linux the location names a raw HID device, such as "HID:
// /dev/hidraw3", which the system describes with its model and port. On
// Windows it is the path of the device, which has the model in it.
func Matches(location string, info control.DeviceInfo) bool {
	if m := hidrawNode.FindStringSubmatch(location); m != nil {
		data, err := os.ReadFile(filepath.Join(hidrawRoot, m[1], "device", "uevent"))
		if err != nil {
			return false
		}
		vendor, product, port := parseUevent(string(data))
		if vendor != info.Hardware.Vendor || product != info.Hardware.Product {
			return false
		}
		// Two keyboards of one model differ in their port.
		return port == "" || info.Location == "" || port == info.Location
	}
	v, p := vendorPart.FindStringSubmatch(location), productID.FindStringSubmatch(location)
	if v == nil || p == nil {
		return false
	}
	return hex(v[1]) == info.Hardware.Vendor && hex(p[1]) == info.Hardware.Product && info.Hardware.Vendor != 0
}

func hex(s string) uint16 {
	v, _ := strconv.ParseUint(s, 16, 16)
	return uint16(v)
}

// parseUevent reads the model and the port from the description of a HID
// device: the lines "HID_ID=0003:000005AC:0000024F" and
// "HID_PHYS=usb-0000:00:14.0-2/input1".
func parseUevent(data string) (vendor, product uint16, port string) {
	for _, line := range strings.Split(data, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "HID_ID":
			if parts := strings.Split(value, ":"); len(parts) == 3 {
				v, _ := strconv.ParseUint(parts[1], 16, 32)
				p, _ := strconv.ParseUint(parts[2], 16, 32)
				vendor, product = uint16(v), uint16(p)
			}
		case "HID_PHYS":
			port = value
			// The interfaces of one device differ in the end only.
			if i := strings.LastIndex(port, "/input"); i >= 0 {
				port = port[:i]
			}
		}
	}
	return vendor, product, port
}
