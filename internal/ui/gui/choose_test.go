package gui

import (
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/output"
)

func TestCapture(t *testing.T) {
	var c capture
	one := func(keys ...string) map[string][]string { return map[string][]string{"kbd": keys} }
	steps := []struct {
		down map[string][]string
		done bool
	}{
		{one(), false}, // nothing pressed yet
		{one("KEY_LEFTCTRL"), false},
		{one("KEY_LEFTCTRL", "KEY_LEFTSHIFT"), false},
		{one("KEY_LEFTSHIFT"), false}, // one let go: still counted
		{one(), true},
	}
	for i, s := range steps {
		if got := c.step(s.down); got != s.done {
			t.Fatalf("step %d: done = %v, want %v", i, got, s.done)
		}
	}
	if want := []string{"KEY_LEFTCTRL", "KEY_LEFTSHIFT"}; !slices.Equal(c.keys, want) {
		t.Errorf("keys = %v, want %v", c.keys, want)
	}

	// No more keys than a hand can hold.
	c = capture{}
	c.step(one("KEY_A", "KEY_B", "KEY_C", "KEY_D", "KEY_E"))
	if len(c.keys) != maxToggleKeys {
		t.Errorf("%d keys taken, want %d", len(c.keys), maxToggleKeys)
	}

	// The keys of one keyboard only: the combination is looked for on
	// each device by itself.
	c = capture{}
	c.step(map[string][]string{"a": {"KEY_LEFTCTRL"}})
	c.step(map[string][]string{"a": {"KEY_LEFTCTRL"}, "b": {"KEY_F13"}})
	if !c.step(map[string][]string{"b": {"KEY_F13"}}) {
		t.Error("not complete although the keyboard it began on has no key down")
	}
	if want := []string{"KEY_LEFTCTRL"}; !slices.Equal(c.keys, want) {
		t.Errorf("keys = %v, want %v", c.keys, want)
	}
}

func TestKeysDown(t *testing.T) {
	state := &ipc.Snapshot{Instruments: []ipc.Instrument{
		{Name: "K", Location: "usb-1", Type: "keyboard", Buttons: []ipc.Button{{Name: "KEY_A", Down: true}, {Name: "KEY_B"}, {Name: "KEY_F", Part: "Extra", Down: true}}},
		{Name: "K", Location: "usb-2", Type: "keyboard", Buttons: []ipc.Button{{Name: "KEY_A", Down: true}, {Name: "KEY_C", Down: true}}},
		{Name: "Pad", Type: "gamepad", Buttons: []ipc.Button{{Name: "BTN_SOUTH", Down: true}}},
	}}
	got := keysDown(state)
	if len(got) != 2 || !slices.Equal(got["K\x00usb-1"], []string{"KEY_A"}) || !slices.Equal(got["K\x00usb-2"], []string{"KEY_A", "KEY_C"}) {
		t.Errorf("keysDown = %q", got)
	}
	if got := keysDown(nil); len(got) != 0 {
		t.Errorf("keysDown without HIDI = %v", got)
	}
}

func TestTogglePort(t *testing.T) {
	usb := output.Port{ID: "24:0", Name: "USB MIDI Interface MIDI 1"}
	synth := output.Port{ID: "128:0", Name: "FLUID Synth"}
	connect := []string{"usb midi", "Old Piano"}
	if !portChosen(connect, usb) || portChosen(connect, synth) {
		t.Fatal("portChosen is wrong")
	}
	if got, _ := togglePort(connect, synth); !slices.Equal(got, []string{"usb midi", "Old Piano", "FLUID Synth"}) {
		t.Errorf("adding = %v", got)
	}
	// An entry that is a part of the name may select other ports too: it
	// stays, and is named.
	if got, still := togglePort(connect, usb); !slices.Equal(got, connect) || still != "usb midi" {
		t.Errorf("removing what a part of a name selects = %v, still %q", got, still)
	}
	// The whole name, in whatever case, and the ID are taken out.
	if got, still := togglePort([]string{"usb midi interface midi 1", "Old Piano"}, usb); !slices.Equal(got, []string{"Old Piano"}) || still != "" {
		t.Errorf("removing by name = %v, still %q", got, still)
	}
	if got, _ := togglePort([]string{"24:0"}, usb); len(got) != 0 {
		t.Errorf("removing by ID = %v", got)
	}
	if got, want := absentPorts(connect, []output.Port{usb, synth}), []string{"Old Piano"}; !slices.Equal(got, want) {
		t.Errorf("absentPorts = %v, want %v", got, want)
	}
}
