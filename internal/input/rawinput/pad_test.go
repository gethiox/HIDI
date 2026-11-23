package rawinput

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

func TestButtonCodes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		layout   layout
		joystick bool
		n        uint16
		want     string // "" for no code
	}{
		{"gamepad, first", layoutGeneric, false, 1, "BTN_SOUTH"},
		{"gamepad, fourth", layoutGeneric, false, 4, "BTN_NORTH"},
		{"gamepad, sixteenth", layoutGeneric, false, 16, "KEY_0x13f"},
		{"gamepad, seventeenth", layoutGeneric, false, 17, "BTN_TRIGGER_HAPPY1"},
		{"joystick, first", layoutGeneric, true, 1, "BTN_TRIGGER"},
		{"joystick, second", layoutGeneric, true, 2, "BTN_THUMB"},
		{"joystick, seventeenth", layoutGeneric, true, 17, "BTN_TRIGGER_HAPPY1"},
		{"last extra button", layoutGeneric, true, 56, "BTN_TRIGGER_HAPPY40"},
		{"too many buttons", layoutGeneric, true, 57, ""},
		{"no button zero", layoutGeneric, false, 0, ""},
		{"Xbox A", layoutXbox, false, 1, "BTN_SOUTH"},
		{"Xbox X", layoutXbox, false, 3, "BTN_NORTH"},
		{"Xbox Y", layoutXbox, false, 4, "BTN_WEST"},
		{"Xbox LB", layoutXbox, false, 5, "BTN_TL"},
		{"Xbox Back", layoutXbox, false, 7, "BTN_SELECT"},
		{"Xbox right stick", layoutXbox, false, 10, "BTN_THUMBR"},
		{"Sony Square", layoutSony, false, 1, "BTN_WEST"},
		{"Sony Cross", layoutSony, false, 2, "BTN_SOUTH"},
		{"Sony Triangle", layoutSony, false, 4, "BTN_NORTH"},
		{"Sony L2", layoutSony, false, 7, "BTN_TL2"},
		{"Sony PS", layoutSony, false, 13, "BTN_MODE"},
	} {
		code, ok := tt.layout.button(tt.n, tt.joystick)
		got := ""
		if ok {
			got = control.Key(code).String()
		}
		if got != tt.want {
			t.Errorf("%s: button(%d) = %q, want %q", tt.name, tt.n, got, tt.want)
		}
	}
}

func TestLayoutOf(t *testing.T) {
	if got := layoutOf(0x045e, 0x028e, true); got != layoutXbox {
		t.Errorf("an Xbox controller got layout %d", got)
	}
	if got := layoutOf(0x054c, 0x09cc, false); got != layoutSony {
		t.Errorf("a DualShock 4 got layout %d", got)
	}
	if got := layoutOf(0x054c, 0x0268, false); got != layoutGeneric {
		t.Errorf("another Sony controller got layout %d", got)
	}
	if got := layoutOf(0x046d, 0xc216, false); got != layoutGeneric {
		t.Errorf("a plain gamepad got layout %d", got)
	}
}

func TestParsePathXbox(t *testing.T) {
	id := parsePath(`\\?\HID#VID_045E&PID_028E&IG_00#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`)
	if !id.xinput || id.vendor != 0x045e || id.product != 0x028e {
		t.Errorf("parsePath = %+v", id)
	}
	if id := parsePath(`\\?\HID#VID_046D&PID_C216#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`); id.xinput {
		t.Error("a plain gamepad was taken for an Xbox controller")
	}
}

// names lists the axes a pad ended up with.
func axisNames(p *pad) []string {
	var names []string
	for _, a := range p.axes {
		names = append(names, control.Axis(a.code).String())
	}
	return names
}

func TestAxisCodes(t *testing.T) {
	generic := newPad(layoutGeneric, true)
	for _, usage := range []uint16{usageX, usageY, usageZ, usageRx, usageRy, usageRz, usageSlider, usageDial, usageHat, usageSlider, usageHat} {
		if !generic.addAxis(pageDesktop, usage, 8, 0, 255) {
			t.Errorf("usage %#x was refused", usage)
		}
	}
	want := []string{"ABS_X", "ABS_Y", "ABS_Z", "ABS_RX", "ABS_RY", "ABS_RZ", "ABS_THROTTLE", "ABS_RUDDER", "ABS_HAT0X", "ABS_MISC", "ABS_HAT1X"}
	if got := axisNames(generic); !slices.Equal(got, want) {
		t.Errorf("generic axes = %v, want %v", got, want)
	}
	if generic.addAxis(0xff00, 1, 8, 0, 255) || generic.addAxis(pageDesktop, 0x3d, 8, 0, 255) {
		t.Error("a usage without a code was accepted")
	}

	sony := newPad(layoutSony, false)
	for _, usage := range []uint16{usageX, usageY, usageZ, usageRz, usageRx, usageRy} {
		sony.addAxis(pageDesktop, usage, 8, 0, 255)
	}
	want = []string{"ABS_X", "ABS_Y", "ABS_RX", "ABS_RY", "ABS_Z", "ABS_RZ"}
	if got := axisNames(sony); !slices.Equal(got, want) {
		t.Errorf("Sony axes = %v, want %v", got, want)
	}

	sim := newPad(layoutGeneric, true)
	for _, usage := range []uint16{usageSimSteering, usageSimGas, usageSimBrake, usageSimRudder, usageSimThrottle} {
		sim.addAxis(pageSimulation, usage, 10, 0, 1023)
	}
	want = []string{"ABS_WHEEL", "ABS_GAS", "ABS_BRAKE", "ABS_RUDDER", "ABS_THROTTLE"}
	if got := axisNames(sim); !slices.Equal(got, want) {
		t.Errorf("simulation axes = %v, want %v", got, want)
	}
}

// describe writes events as "NAME=value" in a stable order.
func describe(events []control.Event) []string {
	var out []string
	for _, ev := range events {
		out = append(out, ev.Code.String()+"="+map[float64]string{0: "0", 0.5: "0.5", 1: "1"}[ev.Value])
	}
	sort.Strings(out)
	return out
}

func TestPadButtons(t *testing.T) {
	p := newPad(layoutXbox, false)
	at := time.Now()
	if got, want := describe(p.buttons([]uint16{1, 5}, at)), []string{"BTN_SOUTH=1", "BTN_TL=1"}; !slices.Equal(got, want) {
		t.Errorf("first report: %v, want %v", got, want)
	}
	if got := p.buttons([]uint16{5, 1}, at); len(got) != 0 {
		t.Errorf("an unchanged report made events: %v", got)
	}
	if got, want := describe(p.buttons([]uint16{5, 2}, at)), []string{"BTN_EAST=1", "BTN_SOUTH=0"}; !slices.Equal(got, want) {
		t.Errorf("third report: %v, want %v", got, want)
	}
	if got, want := describe(p.buttons(nil, at)), []string{"BTN_EAST=0", "BTN_TL=0"}; !slices.Equal(got, want) {
		t.Errorf("release: %v, want %v", got, want)
	}
}

func TestPadValues(t *testing.T) {
	at := time.Now()
	value := func(events []control.Event, name string) (float64, bool) {
		for _, ev := range events {
			if ev.Code.String() == name {
				return ev.Value, true
			}
		}
		return 0, false
	}

	p := newPad(layoutGeneric, false)
	p.addAxis(pageDesktop, usageX, 8, 0, 255)    // unsigned
	p.addAxis(pageDesktop, usageY, 8, -127, 127) // signed
	p.addAxis(pageDesktop, usageRx, 16, 0, -1)   // a range that is not one
	p.addAxis(pageDesktop, usageHat, 4, 0, 7)    // eight directions
	p.addAxis(pageDesktop, usageHat, 4, 1, 4)    // four directions
	for _, tt := range []struct {
		axis int
		raw  uint32
		name string
		want float64
	}{
		{0, 0, "ABS_X", 0},
		{0, 255, "ABS_X", 1},
		{1, 0x81, "ABS_Y", 0}, // -127
		{1, 0x80, "ABS_Y", 0}, // -128, below the range
		{1, 0x7f, "ABS_Y", 1},
		{1, 0, "ABS_Y", 0.5},
		{2, 0xffff, "ABS_RX", 1},
		{2, 0, "ABS_RX", 0},
	} {
		got, ok := value(p.value(tt.axis, tt.raw, at), tt.name)
		if tt.axis == 1 && tt.raw == 0x80 {
			// The same value as before makes no event.
			if ok {
				t.Errorf("axis %d raw %#x: an event for an unchanged value", tt.axis, tt.raw)
			}
			continue
		}
		if !ok || got != tt.want {
			t.Errorf("axis %d raw %#x: %s = %v (%v), want %v", tt.axis, tt.raw, tt.name, got, ok, tt.want)
		}
	}

	// Hats: released first, then around the clock.
	for _, tt := range []struct {
		axis int
		raw  uint32
		want []string
	}{
		{3, 8, []string{"ABS_HAT0X=0.5", "ABS_HAT0Y=0.5"}},
		{3, 0, []string{"ABS_HAT0Y=0"}},                  // up
		{3, 1, []string{"ABS_HAT0X=1"}},                  // up and right
		{3, 2, []string{"ABS_HAT0Y=0.5"}},                // right
		{3, 4, []string{"ABS_HAT0X=0.5", "ABS_HAT0Y=1"}}, // down
		{3, 6, []string{"ABS_HAT0X=0", "ABS_HAT0Y=0.5"}}, // left
		{3, 7, []string{"ABS_HAT0Y=0"}},                  // up and left
		{3, 15, []string{"ABS_HAT0X=0.5", "ABS_HAT0Y=0.5"}},
		{4, 0, []string{"ABS_HAT1X=0.5", "ABS_HAT1Y=0.5"}},
		{4, 2, []string{"ABS_HAT1X=1"}},                  // right
		{4, 3, []string{"ABS_HAT1X=0.5", "ABS_HAT1Y=1"}}, // down
	} {
		if got := describe(p.value(tt.axis, tt.raw, at)); !slices.Equal(got, tt.want) {
			t.Errorf("hat %d raw %d: %v, want %v", tt.axis, tt.raw, got, tt.want)
		}
	}
}

func TestXboxTriggers(t *testing.T) {
	p := newPad(layoutXbox, false)
	p.addAxis(pageDesktop, usageZ, 16, 0, -1)
	if p.addAxis(pageDesktop, usageRz, 16, 0, 65535) && axisNames(p)[1] == "ABS_RZ" {
		t.Error("a second axis took the code of the right trigger")
	}
	at := time.Now()
	for _, tt := range []struct {
		raw         uint32
		left, right float64
	}{
		{0x8000, 0, 0},
		{0xffff, 1, 0},
		{0x0000, 0, 1},
	} {
		p.value(0, tt.raw, at)
		l, r := p.last[absZ], p.last[absRZ]
		if abs(l-tt.left) > 0.001 || abs(r-tt.right) > 0.001 {
			t.Errorf("raw %#x: left %v right %v, want %v and %v", tt.raw, l, r, tt.left, tt.right)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestIsGameController(t *testing.T) {
	for _, usage := range []uint16{usageJoystick, usageGamepad, usageMultiAxis} {
		if !isGameController(pageDesktop, usage) {
			t.Errorf("usage %#x is not a game controller", usage)
		}
	}
	if isGameController(pageDesktop, 0x06) || isGameController(0x0c, usageGamepad) {
		t.Error("a keyboard or a consumer control is a game controller")
	}
}

func TestBitField(t *testing.T) {
	// Four hats of four bits each, then a value of twelve bits.
	buf := []byte{0x21, 0x43, 0xbc, 0x0a}
	for i, want := range []uint32{1, 2, 3, 4} {
		if got := bitField(buf, i*4, 4); got != want {
			t.Errorf("hat %d = %d, want %d", i, got, want)
		}
	}
	if got := bitField(buf, 16, 12); got != 0xabc {
		t.Errorf("the value of twelve bits = %#x, want 0xabc", got)
	}
	if got := bitField(buf, 28, 8); got != 0 {
		t.Errorf("a value past the end = %d, want 0", got)
	}
}

// A stick with four hats gets a pair of axes for each, as on Linux.
func TestFourHats(t *testing.T) {
	p := newPad(layoutGeneric, true)
	for range 4 {
		if !p.addAxis(pageDesktop, usageHat, 4, 0, 7) {
			t.Fatal("a hat was not taken in")
		}
	}
	for i, a := range p.axes {
		if want := uint16(absHat0X + 2*i); a.code != want {
			t.Errorf("hat %d has the code %#x, want %#x", i, a.code, want)
		}
	}
	// Only the third hat is pushed, to the right.
	got := describe(p.value(2, 2, time.Time{}))
	if len(got) != 2 || got[0] != "ABS_HAT2X=1" || got[1] != "ABS_HAT2Y=0.5" {
		t.Errorf("pushing the third hat gave %v", got)
	}
}

// A button past the end of a controller's table must not share a code
// with one in the table.
func TestExtraButtonsDoNotCollide(t *testing.T) {
	for _, l := range []layout{layoutXbox, layoutSony} {
		seen := map[uint16]uint16{}
		for n := uint16(1); n <= 24; n++ {
			code, ok := l.button(n, false)
			if !ok {
				t.Fatalf("layout %d: button %d has no code", l, n)
			}
			if other, dup := seen[code]; dup {
				t.Errorf("layout %d: buttons %d and %d are both %#x", l, other, n, code)
			}
			seen[code] = n
		}
	}
	// The click of a DualShock's touchpad is the first extra button.
	if code, _ := layoutSony.button(14, false); code != btnTriggerHappy {
		t.Errorf("Sony button 14 = %#x, want %#x", code, btnTriggerHappy)
	}
}

// The triggers of an Xbox controller take ABS_Z and ABS_RZ, and leave the
// right stick its own axes whatever order the controller declares them in.
func TestXboxTriggersLeaveTheStick(t *testing.T) {
	p := newPad(layoutXbox, false)
	for _, usage := range []uint16{usageX, usageY, usageZ, usageRx, usageRy} {
		if !p.addAxis(pageDesktop, usage, 16, 0, 0) {
			t.Fatalf("usage %#x was not taken in", usage)
		}
	}
	want := []uint16{absX, absY, absZ, absRX, absRY}
	for i, a := range p.axes {
		if a.code != want[i] {
			t.Errorf("axis %d has the code %#x, want %#x", i, a.code, want[i])
		}
	}
	if !p.used[absRZ] {
		t.Error("ABS_RZ is free although the triggers use it")
	}
}

func TestPadHas(t *testing.T) {
	// A stick with four buttons, two axes and a hat says so: the buttons
	// by the codes they will be reported with, the hat as its two axes.
	p := newPad(layoutOf(0x231d, 0x0200, false), true)
	p.addAxis(pageDesktop, usageX, 16, 0, 65535)
	p.addAxis(pageDesktop, usageY, 16, 0, 65535)
	p.addAxis(pageDesktop, usageHat, 4, 0, 7)
	got := p.has(1, 4)
	var keys, axes int
	for _, c := range got {
		switch c.Code.Kind {
		case control.KindKey:
			keys++
		case control.KindAxis:
			axes++
		}
	}
	if keys != 4 || axes != 4 {
		t.Fatalf("the stick has %d buttons and %d axes: %v", keys, axes, got)
	}
	// What it says it has is what it reports.
	for _, ev := range p.buttons([]uint16{1, 4}, time.Time{}) {
		if !slices.ContainsFunc(got, func(c control.Input) bool { return c.Code == ev.Code }) {
			t.Errorf("%v is reported and was not announced", ev.Code)
		}
	}
	// A controller that names no buttons has its axes.
	if got := p.has(1, 0); len(got) != 4 {
		t.Errorf("without buttons: %v", got)
	}
}
