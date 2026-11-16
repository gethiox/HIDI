package gui

import (
	"testing"
	"time"

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
