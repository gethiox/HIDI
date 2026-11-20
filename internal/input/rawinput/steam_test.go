package rawinput

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// steamReport builds an input report with everything at rest.
func steamReport() []byte {
	r := make([]byte, 54)
	r[0] = steamReportInput
	return r
}

func code(t *testing.T, name string) control.Code {
	t.Helper()
	c, err := control.ParseCode(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSteamReader(t *testing.T) {
	// Every control has a name HIDI knows.
	r := newSteamReader()
	for i, c := range r.buttons {
		if c.Kind != control.KindKey || c.Num == 0 {
			t.Errorf("button %s has no code", steamButtons[i].name)
		}
	}
	for i, c := range r.axes {
		if c.Kind != control.KindAxis {
			t.Errorf("axis %s has no code", steamAxes[i].name)
		}
	}
	at := time.Unix(1, 0)
	find := func(events []control.Event, name string) (float64, bool) {
		for _, ev := range events {
			if ev.Code == code(t, name) {
				return ev.Value, true
			}
		}
		return 0, false
	}

	// The first report says where every axis stands and no button, since
	// none is down: sticks and pads in the middle, triggers at nothing.
	first := r.read(steamReport(), at)
	if len(first) != len(steamAxes) {
		t.Fatalf("the first report gave %d events, want one for each of the %d axes", len(first), len(steamAxes))
	}
	for name, want := range map[string]float64{"ABS_X": 0.5, "ABS_Y": 0.5, "ABS_HAT0X": 0.5, "ABS_HAT1Y": 0.5, "ABS_HAT2X": 0, "ABS_HAT2Y": 0} {
		if got, ok := find(first, name); !ok || got != want {
			t.Errorf("%s at rest = %v, %v, want %v", name, got, ok, want)
		}
	}
	// The same again is no news.
	if again := r.read(steamReport(), at); len(again) != 0 {
		t.Errorf("an unchanged report gave %v", again)
	}

	// A pressed, the left trigger half way, the stick up and to the
	// right, the left pad touched at its right edge, the right pad with
	// a value and not touched.
	report := steamReport()
	report[2] |= 1 << 0
	binary.LittleEndian.PutUint16(report[6:], 16384)
	binary.LittleEndian.PutUint16(report[10:], 32767)
	binary.LittleEndian.PutUint16(report[12:], 32767) // up, which the driver turns round
	report[5] |= 1 << 1
	binary.LittleEndian.PutUint16(report[18:], 32767)
	binary.LittleEndian.PutUint16(report[24:], 20000)
	events := r.read(report, at)
	for name, want := range map[string]float64{"BTN_A": 1, "ABS_X": 1, "ABS_Y": 0, "ABS_HAT0X": 1} {
		if got, ok := find(events, name); !ok || got != want {
			t.Errorf("%s = %v, %v, want %v", name, got, ok, want)
		}
	}
	if got, _ := find(events, "ABS_HAT2Y"); got < 0.49 || got > 0.51 {
		t.Errorf("the left trigger half way = %v", got)
	}
	if _, moved := find(events, "ABS_HAT1X"); moved {
		t.Error("a pad that is not touched moved")
	}
	if len(events) != 5 {
		t.Errorf("%d events, want five: %v", len(events), events)
	}

	// Let go: the button comes up and the pad goes back to the middle.
	events = r.read(steamReport(), at)
	if got, ok := find(events, "BTN_A"); !ok || got != 0 {
		t.Errorf("BTN_A released = %v, %v", got, ok)
	}
	if got, ok := find(events, "ABS_HAT0X"); !ok || got != 0.5 {
		t.Errorf("the pad let go of = %v, %v", got, ok)
	}

	// The shorter report is read the same way; others are not read.
	short := steamReport()[:46]
	short[0] = steamReportInput2
	short[3] |= 1 << 1 // the right shoulder
	if got, ok := find(r.read(short, at), "BTN_TR"); !ok || got != 1 {
		t.Errorf("BTN_TR in the short report = %v, %v", got, ok)
	}
	for _, other := range [][]byte{nil, {steamReportInput}, {0x43, 1, 2, 3}, make([]byte, 54)} {
		if got := r.read(other, at); len(got) != 0 {
			t.Errorf("a report that is none gave %v", got)
		}
	}

	// A button that is down in the very first report is told.
	fresh := newSteamReader()
	held := steamReport()
	held[4] |= 1 << 0
	if got, ok := find(fresh.read(held, at), "BTN_MODE"); !ok || got != 1 {
		t.Errorf("a button held from the start = %v, %v", got, ok)
	}
}

func TestSteamConnectAndCommands(t *testing.T) {
	r := newSteamReader()
	came := 0
	r.connected = func() { came++ }
	r.read([]byte{steamReportWireless, 1}, time.Time{}) // one went
	r.read([]byte{steamReportWireless, steamWirelessConnect}, time.Time{})
	if came != 1 {
		t.Errorf("told of %d controllers that came, want 1", came)
	}

	for _, product := range []uint16{0x1302, 0x1303, 0x1304, 0x1305} {
		if !isSteamController(valveVendor, product) {
			t.Errorf("product %#x is not taken for the controller", product)
		}
	}
	if isSteamController(valveVendor, 0x1102) || isSteamController(0x045e, 0x1302) {
		t.Error("another device is taken for the controller")
	}

	// Its own interface, not the cut-down one of the receiver, and not
	// the interfaces of other devices that keep one for themselves.
	if !isSteamInterface(0xff00, 1, valveVendor, 0x1304) || isSteamInterface(0xff00, 2, valveVendor, 0x1304) ||
		isSteamInterface(0x01, 6, valveVendor, 0x1304) || isSteamInterface(0xff00, 1, 0x046d, 0xc52b) {
		t.Error("the controller's interface is not told from the others")
	}
	// Being a mouse again takes two commands of one byte each.
	if len(steamLizardOn) != 2 || steamLizardOn[0][0] != 0x85 || steamLizardOn[1][0] != 0x8e {
		t.Errorf("the commands that end it: % x", steamLizardOn)
	}

	// The settings command: lizard mode (9) and Steam's watchdog (71),
	// both to nothing, six bytes of settings in all.
	report := steamFeature(steamLizardOff[1], 0)
	want := []byte{steamFeatureReport, 0x87, 6, 9, 0, 0, 71, 0, 0, 0}
	if len(report) != 64 || string(report[:len(want)]) != string(want) {
		t.Errorf("the feature report is % x (%d bytes)", report[:12], len(report))
	}
	if got := steamFeature([]byte{0x81}, 65); len(got) != 65 || got[0] != 1 || got[1] != 0x81 {
		t.Errorf("for a device that wants 65 bytes: % x (%d bytes)", got[:3], len(got))
	}
}
