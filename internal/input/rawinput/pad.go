package rawinput

import (
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// A game controller describes itself to Windows in the terms of the USB HID
// standard: buttons numbered from one, and values named by a usage such as
// "X" or "hat switch". This file turns those into the Linux codes HIDI uses
// everywhere, the same ones the Linux kernel gives the controller, so that
// one profile fits both systems.

// Usage pages and usages from the HID usage tables.
const (
	pageDesktop    = 0x01
	pageSimulation = 0x02
	pageButton     = 0x09

	usageJoystick  = 0x04
	usageGamepad   = 0x05
	usageMultiAxis = 0x08

	usageX      = 0x30
	usageY      = 0x31
	usageZ      = 0x32
	usageRx     = 0x33
	usageRy     = 0x34
	usageRz     = 0x35
	usageSlider = 0x36
	usageDial   = 0x37
	usageWheel  = 0x38
	usageHat    = 0x39

	usageSimRudder   = 0xBA
	usageSimThrottle = 0xBB
	usageSimGas      = 0xC4
	usageSimBrake    = 0xC5
	usageSimSteering = 0xC8
)

// Linux codes used below.
const (
	absX        = 0x00
	absY        = 0x01
	absZ        = 0x02
	absRX       = 0x03
	absRY       = 0x04
	absRZ       = 0x05
	absThrottle = 0x06
	absRudder   = 0x07
	absWheel    = 0x08
	absGas      = 0x09
	absBrake    = 0x0a
	absHat0X    = 0x10
	absHat0Y    = 0x11
	absMisc     = 0x28
	absLast     = 0x3f

	btnJoystick     = 0x120
	btnSouth        = 0x130
	btnEast         = 0x131
	btnNorth        = 0x133
	btnWest         = 0x134
	btnTL           = 0x136
	btnTR           = 0x137
	btnTL2          = 0x138
	btnTR2          = 0x139
	btnSelect       = 0x13a
	btnStart        = 0x13b
	btnMode         = 0x13c
	btnThumbL       = 0x13d
	btnThumbR       = 0x13e
	btnTriggerHappy = 0x2c0
	btnTriggerLast  = 0x2e7
)

// isGameController reports whether a top-level HID usage is one of the
// kinds of game controller.
func isGameController(page, usage uint16) bool {
	return page == pageDesktop && (usage == usageJoystick || usage == usageGamepad || usage == usageMultiAxis)
}

// layout is how the controls of a controller are numbered. Most
// controllers follow the generic rules of the Linux kernel. The common
// ones for which Linux has a driver of its own are numbered as that driver
// does.
type layout uint8

const (
	layoutGeneric layout = iota
	// layoutXbox is an Xbox controller, or one that imitates it, as
	// Windows presents it through HID. HIDI reads these through XInput
	// and comes here only when that is not to be had.
	layoutXbox
	// layoutSony is a DualShock 4 or DualSense.
	layoutSony
)

const vendorSony = 0x054c

// layoutOf chooses the layout for a controller.
func layoutOf(vendor, product uint16, xbox bool) layout {
	if xbox {
		return layoutXbox
	}
	if vendor == vendorSony {
		switch product {
		case 0x05c4, 0x09cc, 0x0ba0, 0x0ce6, 0x0df2:
			return layoutSony
		}
	}
	return layoutGeneric
}

// The buttons in the order the controllers number them.
var (
	// A, B, X, Y, LB, RB, Back, Start, left stick, right stick.
	xboxButtons = []uint16{btnSouth, btnEast, btnNorth, btnWest, btnTL, btnTR, btnSelect, btnStart, btnThumbL, btnThumbR}
	// Square, Cross, Circle, Triangle, L1, R1, L2, R2, Share, Options,
	// L3, R3, PS.
	sonyButtons = []uint16{btnWest, btnSouth, btnEast, btnNorth, btnTL, btnTR, btnTL2, btnTR2, btnSelect, btnStart, btnThumbL, btnThumbR, btnMode}
)

// button returns the code of button n, counted from one. joystick tells
// whether the controller calls itself a joystick rather than a gamepad.
func (l layout) button(n uint16, joystick bool) (uint16, bool) {
	if n == 0 {
		return 0, false
	}
	i := n - 1
	// A button past the end of a table must not get a code the table gave
	// away already, as the generic numbering would: the click of a
	// DualShock's touchpad, its button 14, would be the left stick's.
	// Such buttons get the codes of extra buttons.
	var named []uint16
	switch l {
	case layoutXbox:
		named = xboxButtons
	case layoutSony:
		named = sonyButtons
	}
	if named != nil {
		if int(i) < len(named) {
			return named[i], true
		}
		if code := btnTriggerHappy + (i - uint16(len(named))); code <= btnTriggerLast {
			return code, true
		}
		return 0, false
	}
	if i < 16 {
		if joystick {
			return btnJoystick + i, true
		}
		return btnSouth + i, true
	}
	if code := btnTriggerHappy + (i - 16); code <= btnTriggerLast {
		return code, true
	}
	return 0, false
}

// axisKind is how the value of an axis becomes events.
type axisKind uint8

const (
	// axisPlain is passed on across its range.
	axisPlain axisKind = iota
	// axisHat is a direction pad that reports one of eight directions; it
	// becomes a horizontal and a vertical axis.
	axisHat
	// axisTriggers is both triggers of an Xbox controller in one value,
	// which is all Windows reports through HID: the middle when both are
	// released, higher for the left one, lower for the right one. It
	// becomes two axes. Pressing both at once cancels out.
	axisTriggers
)

// axis returns the code and kind for a value with the given usage.
func (l layout) axis(page, usage uint16) (uint16, axisKind, bool) {
	if page == pageSimulation {
		switch usage {
		case usageSimRudder:
			return absRudder, axisPlain, true
		case usageSimThrottle:
			return absThrottle, axisPlain, true
		case usageSimGas:
			return absGas, axisPlain, true
		case usageSimBrake:
			return absBrake, axisPlain, true
		case usageSimSteering:
			return absWheel, axisPlain, true
		}
		return 0, 0, false
	}
	if page != pageDesktop {
		return 0, 0, false
	}
	if usage == usageHat {
		return absHat0X, axisHat, true
	}
	switch l {
	case layoutXbox:
		if usage == usageZ {
			return absZ, axisTriggers, true
		}
	case layoutSony:
		// The right stick and the triggers swap places.
		switch usage {
		case usageZ:
			return absRX, axisPlain, true
		case usageRz:
			return absRY, axisPlain, true
		case usageRx:
			return absZ, axisPlain, true
		case usageRy:
			return absRZ, axisPlain, true
		}
	}
	switch usage {
	case usageX, usageY, usageZ, usageRx, usageRy, usageRz:
		return absX + (usage - usageX), axisPlain, true
	case usageSlider:
		return absThrottle, axisPlain, true
	case usageDial:
		return absRudder, axisPlain, true
	case usageWheel:
		return absWheel, axisPlain, true
	}
	return 0, 0, false
}

// padAxis is one value a controller reports.
type padAxis struct {
	page, usage uint16
	kind        axisKind
	code        uint16 // for a hat, the horizontal axis; the vertical one follows
	bits        uint16
	min, max    int64
}

// pad turns the reports of one controller into events. It remembers the
// state of every control and reports only changes.
type pad struct {
	layout   layout
	joystick bool
	axes     []padAxis
	used     map[uint16]bool
	down     map[uint16]bool
	now      map[uint16]bool // of buttons, a scratch map
	last     map[uint16]float64
}

func newPad(l layout, joystick bool) *pad {
	return &pad{layout: l, joystick: joystick, used: map[uint16]bool{}, down: map[uint16]bool{}, last: map[uint16]float64{}}
}

// addAxis takes in a value the controller declares: its usage, its size in
// bits, and the range it says the value has. It returns false for a usage
// HIDI has no code for.
func (p *pad) addAxis(page, usage, bits uint16, minimum, maximum int32) bool {
	code, kind, ok := p.layout.axis(page, usage)
	if !ok || bits == 0 || bits > 32 {
		return false
	}
	// A controller with two values of the same usage, such as two
	// sliders, gets the next free codes for the further ones, as on Linux.
	width := uint16(1)
	if kind != axisPlain {
		width = 2
	}
	if kind == axisTriggers {
		// The two triggers are ABS_Z and ABS_RZ, which are not next to
		// each other.
		if p.used[absZ] || p.used[absRZ] {
			return false
		}
		width = 1
	}
	for p.taken(code, width) {
		if kind == axisHat {
			code += 2
		} else {
			if code < absMisc {
				code = absMisc
			} else {
				code++
			}
		}
		if code+width-1 > absLast {
			return false
		}
	}
	for i := range width {
		p.used[code+i] = true
	}
	if kind == axisTriggers {
		p.used[absRZ] = true
	}
	lo, hi := int64(minimum), int64(maximum)
	if hi <= lo {
		// Some controllers, those of the Xbox among them, declare a range
		// that only makes sense for an unsigned value.
		lo, hi = 0, int64(1)<<bits-1
	}
	p.axes = append(p.axes, padAxis{page: page, usage: usage, kind: kind, code: code, bits: bits, min: lo, max: hi})
	return true
}

func (p *pad) taken(code, width uint16) bool {
	for i := range width {
		if p.used[code+i] {
			return true
		}
	}
	return false
}

// buttons takes the numbers of the buttons that are down, counted from
// one, and returns events for those that changed.
func (p *pad) buttons(pressed []uint16, at time.Time) []control.Event {
	var events []control.Event
	if p.now == nil {
		p.now = map[uint16]bool{}
	}
	now := p.now // kept between reports, which come a thousand a second
	clear(now)
	for _, n := range pressed {
		if code, ok := p.layout.button(n, p.joystick); ok {
			now[code] = true
		}
	}
	for code := range p.down {
		if !now[code] {
			delete(p.down, code)
			events = append(events, control.Event{Code: control.Key(code), Value: 0, Time: at})
		}
	}
	for code := range now {
		if !p.down[code] {
			p.down[code] = true
			events = append(events, control.Event{Code: control.Key(code), Value: 1, Time: at})
		}
	}
	return events
}

// value takes the raw value of axis i and returns events for what changed.
func (p *pad) value(i int, raw uint32, at time.Time) []control.Event {
	a := p.axes[i]
	v := int64(raw)
	if a.min < 0 && a.bits < 64 && v&(1<<(a.bits-1)) != 0 {
		v -= 1 << a.bits // the value is signed
	}
	var events []control.Event
	set := func(code uint16, value float64) {
		if last, ok := p.last[code]; ok && last == value {
			return
		}
		p.last[code] = value
		events = append(events, control.Event{Code: control.Axis(code), Value: value, Time: at})
	}
	switch a.kind {
	case axisHat:
		x, y := hatPosition(v, a.min, a.max)
		set(a.code, x)
		set(a.code+1, y)
	case axisTriggers:
		pos := scale(v, a.min, a.max)
		set(absZ, clamp((pos-0.5)*2))
		set(absRZ, clamp((0.5-pos)*2))
	default:
		set(a.code, scale(v, a.min, a.max))
	}
	return events
}

// scale maps a value onto 0-1 across its range.
func scale(v, lo, hi int64) float64 {
	if hi <= lo {
		return 0
	}
	return clamp(float64(v-lo) / float64(hi-lo))
}

func clamp(x float64) float64 { return min(max(x, 0), 1) }

// hatPosition turns the direction a hat reports into the positions of a
// horizontal and a vertical axis: 0, 0.5 or 1 each, where up and left are
// 0. A hat counts its directions clockwise from up, eight of them or four,
// and reports a value outside its range when it is released.
func hatPosition(v, lo, hi int64) (x, y float64) {
	x, y = 0.5, 0.5
	if v < lo || v > hi {
		return x, y
	}
	dir := v - lo
	switch hi - lo {
	case 7:
	case 3:
		dir *= 2
	default:
		return x, y
	}
	switch dir {
	case 1, 2, 3:
		x = 1
	case 5, 6, 7:
		x = 0
	}
	switch dir {
	case 7, 0, 1:
		y = 0
	case 3, 4, 5:
		y = 1
	}
	return x, y
}

// bitField reads size bits of buf from bit number start on, the lowest bit
// first, as the values of a HID report are laid out. size is at most 32.
func bitField(buf []byte, start, size int) uint32 {
	var v uint32
	for i := range size {
		bit := start + i
		if bit/8 >= len(buf) {
			break
		}
		if buf[bit/8]&(1<<(bit%8)) != 0 {
			v |= 1 << i
		}
	}
	return v
}
