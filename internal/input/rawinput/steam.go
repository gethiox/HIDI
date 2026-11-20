package rawinput

import (
	"encoding/binary"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// Valve's Steam Controller of 2026 does not describe itself to Windows as
// a game controller. Left alone it is a mouse and a keyboard, which Valve
// calls its lizard mode; what its sticks, pads and buttons do it tells in
// reports of Valve's own, on an interface of its own, to whoever asks it
// to stop being a mouse. Steam does that, and so does the Linux kernel,
// whose driver (drivers/hid/hid-steam.c) is where the layout below is
// from. The codes are the ones that driver gives the controls, so that a
// profile is the same on both systems.

const (
	// valveVendor is Valve's USB vendor number.
	valveVendor = 0x28de
	// usagePageVendor is the usage page of the controller's own
	// interface, and usageSteamPogo the usage of the interface of the
	// wireless receiver that is of no use: a cut-down one for a
	// controller that sits on the receiver's pins.
	usagePageVendor = 0xff00
	usageSteamPogo  = 2

	// The reports of the controller, by their first byte.
	steamReportInput    = 0x42 // everything
	steamReportInput2   = 0x45 // the same without the last few values
	steamReportWireless = 0x79 // a controller came or went, on the receiver
	// steamWirelessConnect is what a wireless report says when a
	// controller came.
	steamWirelessConnect = 2
	// steamInputLen is how much of an input report is read.
	steamInputLen = 28

	// steamFeatureReport is the feature report commands are sent in.
	steamFeatureReport = 1
)

// isSteamController reports whether a device is a Steam Controller of
// 2026: by cable, by Bluetooth, or one of the two wireless receivers.
func isSteamController(vendor, product uint16) bool {
	return vendor == valveVendor && product >= 0x1302 && product <= 0x1305
}

// isSteamInterface reports whether an interface with this usage is the
// one a Steam Controller of 2026 reports on.
func isSteamInterface(usagePage, usage, vendor, product uint16) bool {
	return usagePage == usagePageVendor && usage != usageSteamPogo && isSteamController(vendor, product)
}

// The commands that make the controller stop being a mouse and keyboard,
// and those that make it one again, as the kernel sends them. Each is the
// content of a feature report after its number.
var (
	steamLizardOff = [][]byte{
		{0x81},                       // forget what the buttons type
		{0x87, 6, 9, 0, 0, 71, 0, 0}, // settings: no lizard mode, and do not wait for Steam to keep it off
	}
	steamLizardOn = [][]byte{
		{0x85}, // the buttons type again
		{0x8e}, // the settings as they come
	}
)

// steamFeature returns a command as the feature report to send: the
// number of the report, the command, and nothing up to the length the
// device wants, which is at least 64.
func steamFeature(command []byte, length int) []byte {
	report := make([]byte, max(length, 64, len(command)+1))
	report[0] = steamFeatureReport
	copy(report[1:], command)
	return report
}

// steamButtons says where in an input report each button is: the byte
// and the bit in it.
var steamButtons = []struct {
	name      string
	byte, bit uint8
}{
	{"BTN_A", 2, 0}, {"BTN_B", 2, 1}, {"BTN_X", 2, 2}, {"BTN_Y", 2, 3},
	{"BTN_BASE", 2, 4}, {"BTN_THUMBR", 2, 5}, {"BTN_START", 2, 6}, {"BTN_GRIPR", 2, 7},
	{"BTN_GRIPR2", 3, 0}, {"BTN_TR", 3, 1},
	{"BTN_DPAD_DOWN", 3, 2}, {"BTN_DPAD_RIGHT", 3, 3}, {"BTN_DPAD_LEFT", 3, 4}, {"BTN_DPAD_UP", 3, 5},
	{"BTN_SELECT", 3, 6}, {"BTN_THUMBL", 3, 7},
	{"BTN_MODE", 4, 0}, {"BTN_GRIPL", 4, 1}, {"BTN_GRIPL2", 4, 2}, {"BTN_TL", 4, 3},
	{"BTN_THUMB2", 4, 6}, {"BTN_TR2", 4, 7},
	{"BTN_THUMB", 5, 2}, {"BTN_TL2", 5, 3},
}

// steamAxes says where each axis is: the byte its 16 bits begin at, and
// how they are read. A pad has a value only while it is touched, which a
// bit of the buttons says; untouched it rests in the middle.
var steamAxes = []struct {
	name    string
	at      uint8
	invert  bool
	trigger bool // from 0 to 32767 instead of from -32767 to 32767
	// touchByte and touchBit, if touchByte is not zero, say where the
	// bit is that tells that the pad is touched.
	touchByte, touchBit uint8
}{
	{name: "ABS_HAT2Y", at: 6, trigger: true}, // the left trigger
	{name: "ABS_HAT2X", at: 8, trigger: true}, // the right trigger
	{name: "ABS_X", at: 10},
	{name: "ABS_Y", at: 12, invert: true},
	{name: "ABS_RX", at: 14},
	{name: "ABS_RY", at: 16, invert: true},
	{name: "ABS_HAT0X", at: 18, touchByte: 5, touchBit: 1}, // the left pad
	{name: "ABS_HAT0Y", at: 20, touchByte: 5, touchBit: 1},
	{name: "ABS_HAT1X", at: 24, touchByte: 4, touchBit: 5}, // the right pad
	{name: "ABS_HAT1Y", at: 26, touchByte: 4, touchBit: 5},
}

// steamReader turns the reports of a Steam Controller into events: one
// for every button and axis that is not as it was in the report before.
type steamReader struct {
	buttons []control.Code
	axes    []control.Code
	down    []bool
	values  []float64
	known   bool // whether a report was read before
	// connected is called when a wireless report says that a controller
	// came; it may be nil.
	connected func()
}

func newSteamReader() *steamReader {
	r := &steamReader{
		down:   make([]bool, len(steamButtons)),
		values: make([]float64, len(steamAxes)),
	}
	for _, b := range steamButtons {
		code, _ := control.ParseCode(b.name)
		r.buttons = append(r.buttons, code)
	}
	for _, a := range steamAxes {
		code, _ := control.ParseCode(a.name)
		r.axes = append(r.axes, code)
	}
	return r
}

// read returns the events of one report.
func (r *steamReader) read(report []byte, at time.Time) []control.Event {
	if len(report) >= 2 && report[0] == steamReportWireless {
		if report[1] == steamWirelessConnect && r.connected != nil {
			r.connected()
		}
		return nil
	}
	if len(report) < steamInputLen || (report[0] != steamReportInput && report[0] != steamReportInput2) {
		return nil
	}
	var events []control.Event
	for i, b := range steamButtons {
		down := report[b.byte]&(1<<b.bit) != 0
		if down == r.down[i] && r.known {
			continue
		}
		r.down[i] = down
		value := 0.0
		if down {
			value = 1
		}
		// At the first report only what is down is news.
		if down || r.known {
			events = append(events, control.Event{Code: r.buttons[i], Value: value, Time: at})
		}
	}
	for i, a := range steamAxes {
		raw := float64(int16(binary.LittleEndian.Uint16(report[a.at:])))
		var value float64
		switch {
		case a.trigger:
			value = raw / 32767
		case a.touchByte != 0 && report[a.touchByte]&(1<<a.touchBit) == 0:
			value = 0.5
		default:
			if a.invert {
				raw = -raw
			}
			value = (raw + 32767) / 65534
		}
		value = min(max(value, 0), 1)
		if value == r.values[i] && r.known {
			continue
		}
		r.values[i] = value
		events = append(events, control.Event{Code: r.axes[i], Value: value, Time: at})
	}
	r.known = true
	return events
}
