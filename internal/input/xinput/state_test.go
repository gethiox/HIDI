package xinput

import (
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

func describe(events []control.Event) []string {
	var out []string
	for _, ev := range events {
		out = append(out, fmt.Sprintf("%s=%.2f", ev.Code, ev.Value))
	}
	sort.Strings(out)
	return out
}

func TestFirstState(t *testing.T) {
	got := describe(changes(state{}, state{buttons: 0x1000}, true, time.Now()))
	want := []string{
		"ABS_HAT0X=0.50", "ABS_HAT0Y=0.50",
		"ABS_RX=0.50", "ABS_RY=0.50", "ABS_RZ=0.00",
		"ABS_X=0.50", "ABS_Y=0.50", "ABS_Z=0.00",
		"BTN_SOUTH=1.00",
	}
	if !slices.Equal(got, want) {
		t.Errorf("first state:\n got %v\nwant %v", got, want)
	}
}

func TestChanges(t *testing.T) {
	at := time.Now()
	for _, tt := range []struct {
		name      string
		old, next state
		want      []string
	}{
		{"nothing", state{buttons: 0x1000, leftX: 5}, state{buttons: 0x1000, leftX: 5}, nil},
		{"press and release", state{buttons: 0x1000}, state{buttons: 0x2000}, []string{"BTN_EAST=1.00", "BTN_SOUTH=0.00"}},
		{"X and Y as on Linux", state{}, state{buttons: 0x4000 | 0x8000}, []string{"BTN_NORTH=1.00", "BTN_WEST=1.00"}},
		{"shoulders and sticks", state{}, state{buttons: 0x0100 | 0x0200 | 0x0040 | 0x0080}, []string{"BTN_THUMBL=1.00", "BTN_THUMBR=1.00", "BTN_TL=1.00", "BTN_TR=1.00"}},
		{"back, start, guide", state{}, state{buttons: 0x0020 | 0x0010 | 0x0400}, []string{"BTN_MODE=1.00", "BTN_SELECT=1.00", "BTN_START=1.00"}},
		{"both triggers at once", state{}, state{leftTrigger: 255, rightTrigger: 255}, []string{"ABS_RZ=1.00", "ABS_Z=1.00"}},
		{"stick right", state{}, state{leftX: 32767}, []string{"ABS_X=1.00"}},
		{"stick left", state{}, state{leftX: -32768}, []string{"ABS_X=0.00"}},
		{"stick up reads low", state{}, state{leftY: 32767}, []string{"ABS_Y=0.00"}},
		{"right stick down reads high", state{}, state{rightY: -32768}, []string{"ABS_RY=1.00"}},
		{"pad up and left", state{}, state{buttons: dpadUp | dpadLeft}, []string{"ABS_HAT0X=0.00", "ABS_HAT0Y=0.00"}},
		{"pad down and right", state{buttons: dpadUp | dpadLeft}, state{buttons: dpadDown | dpadRight}, []string{"ABS_HAT0X=1.00", "ABS_HAT0Y=1.00"}},
		{"pad released", state{buttons: dpadDown}, state{}, []string{"ABS_HAT0Y=0.50"}},
	} {
		if got := describe(changes(tt.old, tt.next, false, at)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Every code must be one HIDI can name.
func TestCodesHaveNames(t *testing.T) {
	for _, b := range buttons {
		if name := control.Key(b.code).String(); len(name) < 5 || name[:4] != "BTN_" {
			t.Errorf("bit %#x has the code %s", b.bit, name)
		}
	}
	for _, a := range (state{}).axes() {
		if name := control.Axis(a.code).String(); len(name) > 6 && name[:6] == "ABS_0x" {
			t.Errorf("an axis has the code %s", name)
		}
	}
}

func TestControls(t *testing.T) {
	// Everything a controller can report is announced, once.
	got := controls()
	seen := map[control.Code]bool{}
	for _, c := range got {
		if seen[c.Code] {
			t.Errorf("%v is listed twice", c.Code)
		}
		seen[c.Code] = true
	}
	if len(got) != len(buttons)+8 {
		t.Errorf("%d controls", len(got))
	}
	all := state{buttons: 0xffff, leftX: 1000, leftTrigger: 9}
	for _, ev := range changes(state{}, all, true, time.Time{}) {
		if !seen[ev.Code] {
			t.Errorf("%v is reported and was not announced", ev.Code)
		}
	}
}
