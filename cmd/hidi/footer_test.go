package main

import (
	"strings"
	"testing"

	"github.com/gethiox/HIDI/internal/ipc"
)

func TestFooterLines(t *testing.T) {
	lines := footerLines(twoInstruments, "KEY_F1+KEY_F2", false)
	want := []string{
		strings.Repeat("─", 78),
		"HIDI is on · 2 instruments · KEY_F1+KEY_F2 turns it off",
		"Keychron K2                  keyboard ch  3  oct  +1  notes  2  in   12/s  MIDI  340/s  Piano",
		"Pad                          gamepad  ch  1  oct  +0  notes  1  in    0/s  MIDI    0/s  Default",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// Many instruments: the first are named, the rest counted.
	many := ipc.Snapshot{}
	for range 12 {
		many.Instruments = append(many.Instruments, ipc.Instrument{Name: "A keyboard with a name longer than the column is wide"})
	}
	lines = footerLines(many, "", true)
	if len(lines) != 2+footerInstruments {
		t.Fatalf("%d lines for twelve instruments", len(lines))
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "and 5 more") {
		t.Errorf("last line: %q", last)
	}
	if !strings.Contains(lines[1], "HIDI is off") || !strings.Contains(lines[1], "\x1b[") {
		t.Errorf("state line: %q", lines[1])
	}
	if !strings.Contains(lines[2], "A keyboard with a name long…") {
		t.Errorf("a long name was not cut: %q", lines[2])
	}
}
