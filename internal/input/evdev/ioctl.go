//go:build linux && !(mips || mipsle || mips64 || mips64le || ppc64 || ppc64le || sparc64)

package evdev

// Linux packs an ioctl request number from a direction, a size, a type
// letter and a sequence number. This is the layout used by most
// architectures; the ones excluded by the build constraint above use a
// different one, and HIDI does not target them.
const (
	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

// Requests on an event device, from linux/input.h.
var (
	// reqID reads struct input_id.
	reqID = ioc(iocRead, 'E', 0x02, 8)
	// reqGrab takes (argument 1) or releases (argument 0) exclusive access.
	reqGrab = ioc(iocWrite, 'E', 0x90, 4)
)

// reqName, reqPhys and reqUniq read the device name, its physical location
// and its unique identifier as a string of at most size bytes.
func reqName(size int) uintptr { return ioc(iocRead, 'E', 0x06, uintptr(size)) }
func reqPhys(size int) uintptr { return ioc(iocRead, 'E', 0x07, uintptr(size)) }
func reqUniq(size int) uintptr { return ioc(iocRead, 'E', 0x08, uintptr(size)) }

// reqKeyState reads the bitmap of keys that are currently down.
func reqKeyState(size int) uintptr { return ioc(iocRead, 'E', 0x18, uintptr(size)) }

// reqBits reads the bitmap of codes the device supports for an event type.
// Event type 0 reads the bitmap of supported event types.
func reqBits(eventType, size int) uintptr {
	return ioc(iocRead, 'E', 0x20+uintptr(eventType), uintptr(size))
}

// reqAbs reads struct input_absinfo for an absolute axis.
func reqAbs(axis uint16) uintptr { return ioc(iocRead, 'E', 0x40+uintptr(axis), absInfoSize) }
