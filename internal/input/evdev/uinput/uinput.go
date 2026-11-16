//go:build linux

// Package uinput creates input devices in software, through the kernel's
// uinput driver. HIDI's tests use it to get keyboards and game controllers
// to read from on a machine that has none.
package uinput

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Event types and codes, from linux/input-event-codes.h.
const (
	EvSyn = 0x00
	EvKey = 0x01
	EvAbs = 0x03

	synReport = 0
)

// BusVirtual is the bus a created device reports.
const BusVirtual = 0x06

// Requests on /dev/uinput, from linux/uinput.h.
var (
	uiDevCreate  = ioc(0, 'U', 1, 0)
	uiDevDestroy = ioc(0, 'U', 2, 0)
	uiDevSetup   = ioc(iocWrite, 'U', 3, 92) // struct uinput_setup
	uiAbsSetup   = ioc(iocWrite, 'U', 4, 28) // struct uinput_abs_setup
	uiSetEvBit   = ioc(iocWrite, 'U', 100, 4)
	uiSetKeyBit  = ioc(iocWrite, 'U', 101, 4)
	uiSetAbsBit  = ioc(iocWrite, 'U', 103, 4)
	uiSetPhys    = ioc(iocWrite, 'U', 108, wordSize)
	uiGetSysname = ioc(iocRead, 'U', 44, 64)
)

const (
	iocWrite = 1
	iocRead  = 2
	wordSize = unsafe.Sizeof(uintptr(0))
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

var native = binary.NativeEndian

// ErrUnavailable is returned by [Create] when /dev/uinput cannot be opened:
// the driver is not loaded, or the user may not use it.
var ErrUnavailable = errors.New("uinput is not available")

// Range is the range of values an absolute axis reports.
type Range struct {
	Min, Max int32
}

// Spec describes a device to create.
type Spec struct {
	// Name is the name the device reports.
	Name string
	// Phys is its physical location, which HIDI uses to tell the nodes of
	// one piece of hardware from those of another.
	Phys string
	// Vendor, Product and Version are its hardware identifiers.
	Vendor, Product, Version uint16
	// Keys are the codes of its keys and buttons.
	Keys []uint16
	// Axes are its absolute axes, by code.
	Axes map[uint16]Range
}

// Device is a created input device. It exists until it is closed.
type Device struct {
	fd int
	// Path is the device's event node, such as /dev/input/event7.
	Path string
}

// Create makes a new input device appear on the system.
func Create(spec Spec) (*Device, error) {
	fd, err := unix.Open("/dev/uinput", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	d := &Device{fd: fd}
	if err := d.setup(spec); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Device) ioctl(request, arg uintptr) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), request, arg); errno != 0 {
		return fmt.Errorf("uinput request %#x: %w", request, errno)
	}
	return nil
}

func (d *Device) setup(spec Spec) error {
	if err := d.ioctl(uiSetEvBit, EvKey); err != nil {
		return err
	}
	for _, key := range spec.Keys {
		if err := d.ioctl(uiSetKeyBit, uintptr(key)); err != nil {
			return err
		}
	}
	if len(spec.Axes) > 0 {
		if err := d.ioctl(uiSetEvBit, EvAbs); err != nil {
			return err
		}
	}
	for axis, r := range spec.Axes {
		if err := d.ioctl(uiSetAbsBit, uintptr(axis)); err != nil {
			return err
		}
		// struct uinput_abs_setup: code, padding, then struct input_absinfo
		// (value, minimum, maximum, fuzz, flat, resolution).
		var abs [28]byte
		native.PutUint16(abs[0:], axis)
		native.PutUint32(abs[8:], uint32(r.Min))
		native.PutUint32(abs[12:], uint32(r.Max))
		if err := d.ioctl(uiAbsSetup, uintptr(unsafe.Pointer(&abs))); err != nil {
			return err
		}
	}

	phys := append([]byte(spec.Phys), 0)
	if err := d.ioctl(uiSetPhys, uintptr(unsafe.Pointer(&phys[0]))); err != nil {
		return err
	}
	// struct uinput_setup: struct input_id, name[80], ff_effects_max.
	var setup [92]byte
	native.PutUint16(setup[0:], BusVirtual)
	native.PutUint16(setup[2:], spec.Vendor)
	native.PutUint16(setup[4:], spec.Product)
	native.PutUint16(setup[6:], spec.Version)
	copy(setup[8:87], spec.Name)
	if err := d.ioctl(uiDevSetup, uintptr(unsafe.Pointer(&setup))); err != nil {
		return err
	}
	if err := d.ioctl(uiDevCreate, 0); err != nil {
		return err
	}

	// The event node appears a moment after the device is created.
	var sysname [64]byte
	if err := d.ioctl(uiGetSysname, uintptr(unsafe.Pointer(&sysname))); err != nil {
		return err
	}
	pattern := filepath.Join("/sys/devices/virtual/input", unix.ByteSliceToString(sysname[:]), "event*")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if matches, _ := filepath.Glob(pattern); len(matches) == 1 {
			path := filepath.Join("/dev/input", filepath.Base(matches[0]))
			if _, err := os.Stat(path); err == nil {
				d.Path = path
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the event node for %s did not appear", pattern)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Emit makes the device report one event, followed by the end of a packet.
func (d *Device) Emit(typ, code uint16, value int32) error {
	for _, raw := range [][]byte{rawEvent(typ, code, value), rawEvent(EvSyn, synReport, 0)} {
		if _, err := unix.Write(d.fd, raw); err != nil {
			return fmt.Errorf("writing to uinput: %w", err)
		}
	}
	return nil
}

// Key presses or releases a key.
func (d *Device) Key(code uint16, down bool) error {
	var value int32
	if down {
		value = 1
	}
	return d.Emit(EvKey, code, value)
}

// Close removes the device from the system. Closing twice is not an error.
func (d *Device) Close() error {
	if d.fd < 0 {
		return nil
	}
	unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), uiDevDestroy, 0)
	err := unix.Close(d.fd)
	d.fd = -1
	return err
}

// rawEvent encodes struct input_event with a zero time, which the kernel
// fills in.
func rawEvent(typ, code uint16, value int32) []byte {
	b := make([]byte, 2*wordSize+8)
	native.PutUint16(b[2*wordSize:], typ)
	native.PutUint16(b[2*wordSize+2:], code)
	native.PutUint32(b[2*wordSize+4:], uint32(value))
	return b
}
