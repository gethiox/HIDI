//go:build linux

package evdev

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/control"
)

// Event types and codes from linux/input-event-codes.h.
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	synReport  = 0
	synDropped = 3

	keyCount = 0x300 // KEY_CNT
	absCount = 0x40  // ABS_CNT

	absInfoSize = 24 // sizeof(struct input_absinfo)
)

// A raw struct input_event is a timestamp of two machine words followed by
// the type, the code and the value.
const (
	wordSize     = int(unsafe.Sizeof(uintptr(0)))
	rawEventSize = 2*wordSize + 8
)

var native = binary.NativeEndian

// Info describes an event device. It is read once, when the node is opened.
type Info struct {
	// Path is the device file, such as /dev/input/event5.
	Path string
	// Name is the name the device reports.
	Name string
	// Phys is the physical location of the device, such as
	// "usb-0000:00:14.0-2/input0". Nodes of the same piece of hardware
	// share everything before the last slash. It can be empty.
	Phys string
	// Uniq is the unique identifier of the device, usually a serial number
	// or a Bluetooth address. Most devices leave it empty.
	Uniq string
	// Hardware is the bus, vendor, product and version the device reports.
	Hardware control.HardwareID
	// Keys and Axes list the keys, buttons and absolute axes the device
	// has, in ascending order.
	Keys, Axes []control.Code
	// Relative reports whether the device has relative axes, as a mouse
	// does.
	Relative bool
}

// axisRange is the range of raw values an absolute axis reports.
type axisRange struct {
	min, max int32
}

// Node is an open event device. Read must not be called from several
// goroutines at once. Grab, Release and Close may be called from another
// goroutine while a Read is in progress; Close interrupts it.
type Node struct {
	file *os.File
	info Info

	ranges map[uint16]axisRange

	// The node tracks the state of every control so that it can report
	// what changed after the kernel dropped events.
	keysDown [keyCount / 8]byte
	absValue map[uint16]int32
	// dropped is set between a SYN_DROPPED event and the SYN_REPORT that
	// ends the incomplete packet.
	dropped bool

	// mu guards the key state and the grab state, which Grab and Release
	// touch from other goroutines than the one reading.
	mu sync.Mutex
	// wantGrab is set while the caller wants the device grabbed; grabbed is
	// set once the kernel grab is in place.
	wantGrab, grabbed bool

	raw []byte
}

// Open opens an event device and reads its description.
func Open(path string) (*Node, error) {
	// Non-blocking mode lets the Go runtime wait for input, so that Close
	// interrupts a Read in progress.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	n := &Node{
		file:     os.NewFile(uintptr(fd), path),
		ranges:   map[uint16]axisRange{},
		absValue: map[uint16]int32{},
		raw:      make([]byte, 64*rawEventSize),
	}
	n.info.Path = path
	if err := n.describe(); err != nil {
		n.file.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

// Info returns the description of the device.
func (n *Node) Info() Info { return n.info }

// ioctl runs a request whose argument is a pointer to a buffer.
func (n *Node) ioctl(request uintptr, arg unsafe.Pointer) error {
	// The pointer is converted inside the call expression, which is what
	// keeps the buffer alive and in place for the duration of the call.
	return n.control(func(fd uintptr) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(arg))
		return errno
	})
}

// ioctlValue runs a request whose argument is a plain number.
func (n *Node) ioctlValue(request, arg uintptr) error {
	return n.control(func(fd uintptr) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, arg)
		return errno
	})
}

// control runs a system call on the file descriptor, retrying it when it is
// interrupted by a signal.
func (n *Node) control(call func(fd uintptr) unix.Errno) error {
	conn, err := n.file.SyscallConn()
	if err != nil {
		return err
	}
	var errno unix.Errno
	if err := conn.Control(func(fd uintptr) {
		for {
			if errno = call(fd); errno != unix.EINTR {
				return
			}
		}
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// text reads a string property. A device without the property yields "".
func (n *Node) text(request func(size int) uintptr) string {
	var buf [256]byte
	if err := n.ioctl(request(len(buf)), unsafe.Pointer(&buf[0])); err != nil {
		return ""
	}
	return unix.ByteSliceToString(buf[:])
}

// bits reads the bitmap of codes supported for an event type and returns the
// codes that are set.
func (n *Node) bits(eventType, count int) ([]uint16, error) {
	buf := make([]byte, (count+7)/8)
	if err := n.ioctl(reqBits(eventType, len(buf)), unsafe.Pointer(&buf[0])); err != nil {
		return nil, err
	}
	var codes []uint16
	for code := 0; code < count; code++ {
		if buf[code/8]&(1<<(code%8)) != 0 {
			codes = append(codes, uint16(code))
		}
	}
	return codes, nil
}

func (n *Node) describe() error {
	var id [4]uint16 // struct input_id: bustype, vendor, product, version
	if err := n.ioctl(reqID, unsafe.Pointer(&id)); err != nil {
		return fmt.Errorf("reading the device id: %w", err)
	}
	n.info.Hardware = control.HardwareID{Bus: id[0], Vendor: id[1], Product: id[2], Version: id[3]}
	n.info.Name = n.text(reqName)
	n.info.Phys = n.text(reqPhys)
	n.info.Uniq = n.text(reqUniq)

	types, err := n.bits(evSyn, 0x20)
	if err != nil {
		return fmt.Errorf("reading the supported event types: %w", err)
	}
	for _, t := range types {
		switch t {
		case evKey:
			keys, err := n.bits(evKey, keyCount)
			if err != nil {
				return fmt.Errorf("reading the supported keys: %w", err)
			}
			for _, k := range keys {
				n.info.Keys = append(n.info.Keys, control.Key(k))
			}
		case evAbs:
			axes, err := n.bits(evAbs, absCount)
			if err != nil {
				return fmt.Errorf("reading the supported axes: %w", err)
			}
			for _, a := range axes {
				value, r, err := n.readAxis(a)
				if err != nil {
					return fmt.Errorf("reading the range of %v: %w", control.Axis(a), err)
				}
				n.info.Axes = append(n.info.Axes, control.Axis(a))
				n.ranges[a] = r
				n.absValue[a] = value
			}
		case evRel:
			n.info.Relative = true
		}
	}
	// Keys that are already down when the node is opened are recorded but
	// not reported: HIDI did not see them go down.
	if err := n.ioctl(reqKeyState(len(n.keysDown)), unsafe.Pointer(&n.keysDown[0])); err != nil {
		return fmt.Errorf("reading the key state: %w", err)
	}
	return nil
}

// readAxis returns the current value and the range of an absolute axis.
func (n *Node) readAxis(axis uint16) (int32, axisRange, error) {
	// struct input_absinfo: value, minimum, maximum, fuzz, flat, resolution.
	var info [absInfoSize / 4]int32
	if err := n.ioctl(reqAbs(axis), unsafe.Pointer(&info)); err != nil {
		return 0, axisRange{}, err
	}
	return info[0], axisRange{min: info[1], max: info[2]}, nil
}

// Grab takes the device away from the rest of the system: while it is
// grabbed, no other program, the desktop included, receives its events.
//
// The grab takes effect once no key of the device is down. A program that
// saw a key go down and then loses the device never sees the key come up,
// and would treat it as held; the Enter key that started HIDI is the usual
// victim. If a key is down, Grab returns nil and the grab happens when the
// last key is released; a failure at that point is not reported.
//
// The grab ends with Release or when the node is closed.
func (n *Node) Grab() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.wantGrab = true
	return n.grabIfIdle()
}

// grabIfIdle puts a wanted grab in place if no key is down. The caller
// holds n.mu.
func (n *Node) grabIfIdle() error {
	if !n.wantGrab || n.grabbed || n.keysDown != ([keyCount / 8]byte{}) {
		return nil
	}
	if err := n.ioctlValue(reqGrab, 1); err != nil {
		return fmt.Errorf("%s: grabbing the device: %w", n.info.Path, err)
	}
	n.grabbed = true
	return nil
}

// Release ends a grab, or cancels one that has not taken effect yet.
func (n *Node) Release() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.wantGrab = false
	if !n.grabbed {
		return nil
	}
	n.grabbed = false
	if err := n.ioctlValue(reqGrab, 0); err != nil {
		return fmt.Errorf("%s: releasing the device: %w", n.info.Path, err)
	}
	return nil
}

// Close closes the device. It interrupts a Read in progress.
func (n *Node) Close() error { return n.file.Close() }

// ErrDisconnected is returned by [Node.Read] when the device was unplugged.
var ErrDisconnected = errors.New("input device disconnected")

// Read waits for input and appends the resulting events to dst. It returns
// at least one event or an error: [ErrDisconnected] when the device is
// gone, and an error wrapping [os.ErrClosed] after Close.
//
// Key repeat reports are dropped, and so are relative axes, whose movements
// do not fit the position model of [control.Event].
func (n *Node) Read(dst []control.Event) ([]control.Event, error) {
	start := len(dst)
	for len(dst) == start {
		got, err := n.file.Read(n.raw)
		if err != nil {
			if errors.Is(err, unix.ENODEV) {
				return dst, ErrDisconnected
			}
			return dst, fmt.Errorf("%s: %w", n.info.Path, err)
		}
		n.mu.Lock()
		for off := 0; off+rawEventSize <= got; off += rawEventSize {
			dst = n.translate(n.raw[off:off+rawEventSize], dst)
		}
		n.grabIfIdle() // a grab may have been waiting for a key release
		n.mu.Unlock()
	}
	return dst, nil
}

// translate converts one raw event and appends what it amounts to. The
// caller holds n.mu.
func (n *Node) translate(raw []byte, dst []control.Event) []control.Event {
	body := raw[2*wordSize:]
	typ, code := native.Uint16(body[0:]), native.Uint16(body[2:])
	value := int32(native.Uint32(body[4:]))

	if n.dropped {
		// The kernel ran out of buffer space. Everything up to the end of
		// the current packet is incomplete and must be discarded; after
		// that the real state is read back from the device.
		if typ == evSyn && code == synReport {
			n.dropped = false
			dst = n.resync(dst)
		}
		return dst
	}

	switch typ {
	case evSyn:
		if code == synDropped {
			n.dropped = true
		}
	case evKey:
		if value == 2 || code >= keyCount { // 2 is key repeat
			return dst
		}
		down := value != 0
		if n.keyDown(code) == down {
			return dst
		}
		n.setKeyDown(code, down)
		dst = append(dst, keyEvent(code, down, eventTime(raw)))
	case evAbs:
		r, known := n.ranges[code]
		if !known {
			return dst
		}
		n.absValue[code] = value
		dst = append(dst, axisEvent(code, value, r, eventTime(raw)))
	}
	return dst
}

// resync compares the tracked state with the real state of the device and
// appends an event for every control that differs.
func (n *Node) resync(dst []control.Event) []control.Event {
	now := time.Now()

	var down [keyCount / 8]byte
	if err := n.ioctl(reqKeyState(len(down)), unsafe.Pointer(&down[0])); err == nil {
		for _, key := range n.info.Keys {
			code := key.Num
			isDown := down[code/8]&(1<<(code%8)) != 0
			if n.keyDown(code) != isDown {
				n.setKeyDown(code, isDown)
				dst = append(dst, keyEvent(code, isDown, now))
			}
		}
	}
	for _, axis := range n.info.Axes {
		code := axis.Num
		value, _, err := n.readAxis(code)
		if err != nil || value == n.absValue[code] {
			continue
		}
		n.absValue[code] = value
		dst = append(dst, axisEvent(code, value, n.ranges[code], now))
	}
	return dst
}

func (n *Node) keyDown(code uint16) bool { return n.keysDown[code/8]&(1<<(code%8)) != 0 }

func (n *Node) setKeyDown(code uint16, down bool) {
	if down {
		n.keysDown[code/8] |= 1 << (code % 8)
	} else {
		n.keysDown[code/8] &^= 1 << (code % 8)
	}
}

func keyEvent(code uint16, down bool, at time.Time) control.Event {
	ev := control.Event{Code: control.Key(code), Time: at}
	if down {
		ev.Value = 1
	}
	return ev
}

func axisEvent(code uint16, value int32, r axisRange, at time.Time) control.Event {
	return control.Event{Code: control.Axis(code), Value: normalize(value, r), Time: at}
}

// normalize maps a raw axis value onto 0-1 across the range the device
// declares. Values outside the range are clamped. An axis that declares an
// empty range reports 0.
func normalize(value int32, r axisRange) float64 {
	span := int64(r.max) - int64(r.min)
	if span <= 0 {
		return 0
	}
	x := float64(int64(value)-int64(r.min)) / float64(span)
	return min(max(x, 0), 1)
}

// eventTime reads the timestamp at the start of a raw event.
func eventTime(raw []byte) time.Time {
	var sec, usec int64
	if wordSize == 8 {
		sec, usec = int64(native.Uint64(raw[0:])), int64(native.Uint64(raw[8:]))
	} else {
		sec, usec = int64(native.Uint32(raw[0:])), int64(native.Uint32(raw[4:]))
	}
	return time.Unix(sec, usec*1000)
}
