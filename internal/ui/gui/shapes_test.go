package gui

import (
	"math"
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/ipc"
)

func TestGearShape(t *testing.T) {
	// The hole, the disc, a tooth and the gap between two teeth.
	const step = 2 * math.Pi / gearTeeth
	at := func(r, a float64) bool { return inGear(r*math.Cos(a), r*math.Sin(a)) }
	if at(0.1, 1) || !at(0.5, 1) {
		t.Error("the hole or the disc is wrong")
	}
	for i := range gearTeeth {
		if !at(0.9, float64(i)*step) {
			t.Errorf("tooth %d is missing", i)
		}
		if at(0.9, (float64(i)+0.5)*step) {
			t.Errorf("the gap after tooth %d is filled", i)
		}
	}
	// A tooth is narrower at its top than where it stands.
	foot, top := 0.0, 0.0
	for a := 0.0; a < step/2; a += 0.001 {
		if at(0.72, a) {
			foot = 0.72 * math.Sin(a)
		}
		if at(0.95, a) {
			top = 0.95 * math.Sin(a)
		}
	}
	if !(top < foot*0.75) || top == 0 {
		t.Errorf("a tooth is %.3f wide at the top and %.3f at the foot", top, foot)
	}
	alpha := GearAlpha(64)
	if alpha[0] != 0 || alpha[32*64+32] != 0 || alpha[32*64+48] != 255 {
		t.Errorf("the drawn gear: corner %d, middle %d, disc %d", alpha[0], alpha[32*64+32], alpha[32*64+48])
	}
}

func TestFloppy(t *testing.T) {
	// The outline of the disk: rows 1 and 14 are drawn from column 1 to
	// 13 or 14, and the corners of the picture are clear.
	if FloppyPoint(0, 0) || FloppyPoint(15, 15) || FloppyPoint(-1, 3) {
		t.Error("the edge of the picture is drawn")
	}
	for x := 1; x <= 13; x++ {
		if !FloppyPoint(x, 1) || !FloppyPoint(x, 14) {
			t.Errorf("the outline has a hole in column %d", x)
		}
	}
	n := 0
	for y := range 16 {
		for x := range 16 {
			if FloppyPoint(x, y) {
				n++
			}
		}
	}
	if n < 80 || n > 140 {
		t.Errorf("%d points are drawn", n)
	}
}

func TestCut(t *testing.T) {
	if got := CutEnd("keyboard.toml · for keyboards", 12); got != "keyboard.to…" {
		t.Errorf("CutEnd: %q", got)
	}
	if got := CutStart("/home/someone/.config/HIDI", 12); got != "…config/HIDI" {
		t.Errorf("CutStart: %q", got)
	}
	if CutEnd("short", 12) != "short" || CutStart("short", 5) != "short" {
		t.Error("a text that fits is cut")
	}
}

func TestEntryKey(t *testing.T) {
	// One device that is a keyboard and a game controller at once makes
	// two entries of one name; what is remembered of one is not the
	// other's.
	pad := ipc.Instrument{Name: "Stick", Type: "gamepad", Location: "usb-1/input0"}
	keys := ipc.Instrument{Name: "Stick", Type: "keyboard", Location: "usb-1/input1"}
	second := ipc.Instrument{Name: "Stick", Type: "gamepad", Location: "usb-2/input0"}
	if EntryKey(pad) == EntryKey(keys) || EntryKey(pad) == EntryKey(second) {
		t.Errorf("entries share a key: %q, %q, %q", EntryKey(pad), EntryKey(keys), EntryKey(second))
	}
	shown := flip(nil, EntryKey(pad))
	if len(shown) != 1 || slices.Contains(shown, EntryKey(keys)) {
		t.Errorf("shown: %v", shown)
	}
}
