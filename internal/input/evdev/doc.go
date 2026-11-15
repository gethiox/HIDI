//go:build linux

// Package evdev reads Linux input devices through the kernel's event
// interface, the /dev/input/event* character devices.
//
// A [Node] is one such device file. The kernel creates one for every
// interface a piece of hardware presents, so a single keyboard or game
// controller often appears as several nodes; grouping them into one device is
// the job of the layer above.
//
// The package talks to the kernel with ioctl and read, without cgo. The
// constants mirror the kernel header linux/input.h.
package evdev
