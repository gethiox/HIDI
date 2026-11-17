package gui

import (
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/output"
)

func TestCapture(t *testing.T) {
	var c capture
	steps := []struct {
		down []string
		done bool
	}{
		{nil, false}, // nothing pressed yet
		{[]string{"KEY_LEFTCTRL"}, false},
		{[]string{"KEY_LEFTCTRL", "KEY_LEFTSHIFT"}, false},
		{[]string{"KEY_LEFTSHIFT"}, false}, // one let go: still counted
		{nil, true},
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
	c.step([]string{"KEY_A", "KEY_B", "KEY_C", "KEY_D", "KEY_E"})
	if len(c.keys) != maxToggleKeys {
		t.Errorf("%d keys taken, want %d", len(c.keys), maxToggleKeys)
	}
}

func TestKeysDown(t *testing.T) {
	state := &ipc.Snapshot{Instruments: []ipc.Instrument{
		{Type: "keyboard", Buttons: []ipc.Button{{Name: "KEY_A", Down: true}, {Name: "KEY_B"}, {Name: "KEY_F", Part: "Extra", Down: true}}},
		{Type: "keyboard", Buttons: []ipc.Button{{Name: "KEY_A", Down: true}, {Name: "KEY_C", Down: true}}},
		{Type: "gamepad", Buttons: []ipc.Button{{Name: "BTN_SOUTH", Down: true}}},
	}}
	if got, want := keysDown(state), []string{"KEY_A", "KEY_C"}; !slices.Equal(got, want) {
		t.Errorf("keysDown = %v, want %v", got, want)
	}
	if got := keysDown(nil); got != nil {
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
	if got, want := togglePort(connect, synth), []string{"usb midi", "Old Piano", "FLUID Synth"}; !slices.Equal(got, want) {
		t.Errorf("adding = %v, want %v", got, want)
	}
	if got, want := togglePort(connect, usb), []string{"Old Piano"}; !slices.Equal(got, want) {
		t.Errorf("removing = %v, want %v", got, want)
	}
	if got, want := togglePort([]string{"24:0"}, usb), []string(nil); !slices.Equal(got, want) {
		t.Errorf("removing by ID = %v, want %v", got, want)
	}
	if got, want := absentPorts(connect, []output.Port{usb, synth}), []string{"Old Piano"}; !slices.Equal(got, want) {
		t.Errorf("absentPorts = %v, want %v", got, want)
	}
}
