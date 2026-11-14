// Package control defines HIDI's platform-neutral model of input devices:
// which physical controls exist, how they are named, and the events they
// produce. It performs no I/O; input backends translate what the operating
// system reports into these types.
//
// # Controls
//
// A [Code] identifies one physical control on a device. Codes use the
// numbering and names of the Linux input subsystem (KEY_A, BTN_SOUTH, ABS_X)
// on every platform. Those names describe positions on the device rather than
// characters, so KEY_Q is the key right of Tab whatever the keyboard layout
// prints on it. That is what an instrument mapping needs.
//
// # Values
//
// Every [Event] carries a value from 0 to 1. A key or button reports 0 when
// released and 1 when pressed. An axis reports its position across its whole
// travel, so a centered stick reads 0.5 and a released trigger reads 0.
// Controls that sense pressure or travel report the values in between, and
// the engine treats all of them the same way.
package control

//go:generate go run ./internal/gencodes
