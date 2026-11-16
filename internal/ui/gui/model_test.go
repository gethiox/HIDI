package gui

import (
	"testing"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/ipc"
)

func TestLevelRank(t *testing.T) {
	for level, want := range map[string]int{"DEBUG": 0, "DEBUG-4": 0, "INFO": 1, "INFO+2": 1, "WARN": 2, "ERROR": 3, "ERROR+4": 3, "": 0, "odd": 0} {
		if got := levelRank(level); got != want {
			t.Errorf("levelRank(%q) = %d, want %d", level, got, want)
		}
	}
}

func TestModelLog(t *testing.T) {
	var m Model
	at := time.Now()
	for i, level := range []string{"DEBUG", "INFO", "WARN", "ERROR", "INFO"} {
		m.AddLog(ipc.LogLine{Seq: i, Time: at.Add(time.Duration(i) * time.Second), Level: level, Text: level})
	}
	if got := len(m.Log()); got != 5 {
		t.Fatalf("%d lines, want 5", got)
	}
	before := m.Version()
	m.SetMinLevel(2)
	if m.Version() == before {
		t.Error("a new filter did not count as a change")
	}
	got := m.Log()
	if len(got) != 2 || got[0].Text != "WARN" || got[1].Text != "ERROR" {
		t.Errorf("from WARN up: %+v", got)
	}
	m.SetMinLevel(99)
	if m.MinLevel() != len(Levels)-1 {
		t.Errorf("MinLevel = %d", m.MinLevel())
	}
	m.SetMinLevel(0)

	// HIDI is found again and repeats the end of its log.
	m.AddLog(ipc.LogLine{Seq: 3, Time: at.Add(3 * time.Second), Level: "ERROR"})
	m.AddLog(ipc.LogLine{Seq: 4, Time: at.Add(4 * time.Second), Level: "INFO"})
	if got := len(m.Log()); got != 5 {
		t.Errorf("%d lines after a repeat, want 5", got)
	}
	// A HIDI that started anew counts from zero again, later in time.
	m.AddLog(ipc.LogLine{Seq: 0, Time: at.Add(time.Minute), Level: "INFO"})
	if got := len(m.Log()); got != 6 {
		t.Errorf("%d lines after a restart, want 6", got)
	}

	for i := range maxLogLines + 10 {
		m.AddLog(ipc.LogLine{Seq: 10 + i, Time: at.Add(time.Hour), Level: "INFO"})
	}
	if got := len(m.Log()); got != maxLogLines {
		t.Errorf("%d lines kept, want %d", got, maxLogLines)
	}
}

func TestModelSnapshot(t *testing.T) {
	var m Model
	if m.Snapshot() != nil {
		t.Error("a new model has a state")
	}
	m.SetSnapshot(&ipc.Snapshot{Active: true})
	if s := m.Snapshot(); s == nil || !s.Active {
		t.Errorf("Snapshot = %+v", s)
	}
	m.SetSnapshot(nil)
	if m.Snapshot() != nil {
		t.Error("the state stayed after HIDI stopped")
	}
}

func TestPiano(t *testing.T) {
	// One octave from C: seven white keys, five black.
	keys := Piano(60, 71, 10, 20, 70, 40)
	if len(keys) != 12 {
		t.Fatalf("%d keys, want 12", len(keys))
	}
	whites := 0
	for i, k := range keys {
		if k.Black != IsBlack(k.Note) {
			t.Errorf("key %d is wrong about its colour", k.Note)
		}
		if !k.Black {
			whites++
			if i >= 7 {
				t.Errorf("white key %d comes after a black one", k.Note)
			}
			if k.W != 10 || k.H != 40 || k.Y != 20 {
				t.Errorf("white key %d: %+v", k.Note, k)
			}
		}
	}
	if whites != 7 || keys[0].Note != 60 || keys[0].X != 10 || keys[6].Note != 71 || keys[6].X != 70 {
		t.Errorf("white keys: %d, first %+v, last %+v", whites, keys[0], keys[6])
	}
	// C sharp lies over the line between C and D.
	cs := keys[7]
	if cs.Note != 61 || cs.X+cs.W/2 != 20 || cs.H >= 40 {
		t.Errorf("C sharp: %+v", cs)
	}
	// A range that ends on black keys grows to the white ones beside it.
	if keys := Piano(61, 66, 0, 0, 100, 10); keys[0].Note != 60 || keys[len(keys)-1].Note > 67 {
		t.Errorf("a range from C sharp to F sharp starts at %d", keys[0].Note)
	}
	if Piano(60, 71, 0, 0, 0, 10) != nil {
		t.Error("keys without width")
	}
}

func TestAnsiColour(t *testing.T) {
	if AnsiColour(0) != colText || AnsiColour(12345) != colText {
		t.Error("the default colour is not that of text")
	}
	for _, code := range []int{30, 31, 32, 33, 34, 35, 36, 37, 90, 91, 92, 93, 94, 95, 96, 97} {
		if _, ok := ansiColours[code]; !ok {
			t.Errorf("no colour for code %d", code)
		}
	}
}

func TestKeyboard(t *testing.T) {
	keys := Keyboard()
	if len(keys) != 104 {
		t.Errorf("%d keys, want 104", len(keys))
	}
	seen := map[string]bool{}
	for i, key := range keys {
		code, err := control.ParseCode(key.Name)
		if err != nil || code.String() != key.Name {
			t.Errorf("%q is not the name of a control (%v)", key.Name, err)
		}
		if seen[key.Name] {
			t.Errorf("%s is there twice", key.Name)
		}
		seen[key.Name] = true
		if key.X < 0 || key.Y < 0 || key.X+key.W > KeyboardUnitsW+0.001 || key.Y+key.H > KeyboardUnitsH+0.001 {
			t.Errorf("%s lies outside the keyboard: %+v", key.Name, key)
		}
		for _, other := range keys[:i] {
			if key.X < other.X+other.W-0.001 && other.X < key.X+key.W-0.001 &&
				key.Y < other.Y+other.H-0.001 && other.Y < key.Y+key.H-0.001 {
				t.Errorf("%s and %s overlap", key.Name, other.Name)
			}
		}
	}
	// The blocks line up: the arrow keys under the keys above them, the
	// number pad at the right edge.
	at := func(name string) KeyCap {
		for _, key := range keys {
			if key.Name == name {
				return key
			}
		}
		t.Fatalf("no key %s", name)
		return KeyCap{}
	}
	if at("KEY_UP").X != at("KEY_DOWN").X || at("KEY_UP").X != at("KEY_HOME").X || at("KEY_LEFT").X != at("KEY_INSERT").X {
		t.Error("the arrow keys do not line up with the keys above them")
	}
	if at("KEY_KP0").X != at("KEY_NUMLOCK").X || at("KEY_KPENTER").X != at("KEY_KPMINUS").X || at("KEY_KPDOT").X != at("KEY_KP9").X {
		t.Error("the number pad does not line up")
	}
	if got := at("KEY_KPMINUS").X + 1; got != KeyboardUnitsW {
		t.Errorf("the keyboard is %v units wide, want %v", got, KeyboardUnitsW)
	}
	if at("KEY_RIGHTCTRL").X+1.25 != 15 || at("KEY_BACKSPACE").X+2 != 15 || at("KEY_ENTER").X+2.25 != 15 || at("KEY_RIGHTSHIFT").X+2.75 != 15 {
		t.Error("the rows of the main block do not end together")
	}
}
