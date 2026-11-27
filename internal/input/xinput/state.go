package xinput

import (
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// state is what XInput reports about a controller: XINPUT_STATE without
// its packet number.
type state struct {
	buttons        uint16
	leftTrigger    uint8
	rightTrigger   uint8
	leftX, leftY   int16
	rightX, rightY int16
}

// Bits of state.buttons, from XInput.h. The guide button is only reported
// by a function Microsoft does not document.
const (
	dpadUp    = 0x0001
	dpadDown  = 0x0002
	dpadLeft  = 0x0004
	dpadRight = 0x0008
)

// Linux codes of the axes.
const (
	absX     = 0x00
	absY     = 0x01
	absZ     = 0x02
	absRX    = 0x03
	absRY    = 0x04
	absRZ    = 0x05
	absHat0X = 0x10
	absHat0Y = 0x11
)

// buttons pairs the bits with the codes of the Linux driver for Xbox
// controllers.
var buttons = []struct {
	bit  uint16
	code uint16
}{
	{0x1000, 0x130}, // A: BTN_SOUTH
	{0x2000, 0x131}, // B: BTN_EAST
	{0x4000, 0x133}, // X: BTN_NORTH
	{0x8000, 0x134}, // Y: BTN_WEST
	{0x0100, 0x136}, // left shoulder: BTN_TL
	{0x0200, 0x137}, // right shoulder: BTN_TR
	{0x0020, 0x13a}, // Back: BTN_SELECT
	{0x0010, 0x13b}, // Start: BTN_START
	{0x0400, 0x13c}, // guide: BTN_MODE
	{0x0040, 0x13d}, // left stick: BTN_THUMBL
	{0x0080, 0x13e}, // right stick: BTN_THUMBR
}

// axes returns the position of every axis, from 0 to 1. A stick pushed up
// reads low, as on Linux; XInput has it the other way round.
func (s state) axes() [8]struct {
	code  uint16
	value float64
} {
	stick := func(v int16) float64 { return (float64(v) + 32768) / 65535 }
	trigger := func(v uint8) float64 { return float64(v) / 255 }
	hat := func(low, high bool) float64 {
		switch {
		case low && !high:
			return 0
		case high && !low:
			return 1
		}
		return 0.5
	}
	return [8]struct {
		code  uint16
		value float64
	}{
		{absX, stick(s.leftX)},
		{absY, 1 - stick(s.leftY)},
		{absRX, stick(s.rightX)},
		{absRY, 1 - stick(s.rightY)},
		{absZ, trigger(s.leftTrigger)},
		{absRZ, trigger(s.rightTrigger)},
		{absHat0X, hat(s.buttons&dpadLeft != 0, s.buttons&dpadRight != 0)},
		{absHat0Y, hat(s.buttons&dpadUp != 0, s.buttons&dpadDown != 0)},
	}
}

// controls lists what every controller has.
func controls() []control.Input {
	var out []control.Input
	for _, b := range buttons {
		out = append(out, control.Input{Code: control.Key(b.code)})
	}
	for _, a := range (state{}).axes() {
		out = append(out, control.Input{Code: control.Axis(a.code), Hat: a.code == absHat0X || a.code == absHat0Y})
	}
	return out
}

// changes returns the events that lead from one state to the next. For
// the first state of a controller, every axis is reported, and the
// buttons that are down.
func changes(old, next state, first bool, at time.Time) []control.Event {
	var events []control.Event
	for _, b := range buttons {
		was, is := old.buttons&b.bit != 0 && !first, next.buttons&b.bit != 0
		if was == is {
			continue
		}
		ev := control.Event{Code: control.Key(b.code), Time: at}
		if is {
			ev.Value = 1
		}
		events = append(events, ev)
	}
	before, after := old.axes(), next.axes()
	for i, a := range after {
		if first || before[i].value != a.value {
			events = append(events, control.Event{Code: control.Axis(a.code), Value: a.value, Time: at})
		}
	}
	return events
}
