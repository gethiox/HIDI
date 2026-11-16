package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gethiox/HIDI/internal/ipc"
)

// footerInstruments is how many instruments the lines at the bottom of the
// terminal name; the rest are counted.
const footerInstruments = 8

// footerLines describes a running HIDI in a few lines, for the bottom of
// the terminal "hidi run" writes its log to: a rule, a line for HIDI and
// one for each instrument. toggle is the combination that turns HIDI off
// and on, as text, or empty.
func footerLines(s ipc.Snapshot, toggle string, colour bool) []string {
	paint := func(code, text string) string {
		if !colour {
			return text
		}
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	faint := func(text string) string { return paint("90", text) }

	state := paint("1;32", "HIDI is on")
	if !s.Active {
		state = paint("1;33", "HIDI is off")
	}
	head := state + faint(" · ") + plural(len(s.Instruments), "instrument")
	if toggle != "" {
		head += faint(" · " + toggle + " turns it " + map[bool]string{true: "off", false: "on"}[s.Active])
	}
	lines := []string{faint(strings.Repeat("─", 78)), head}

	shown := s.Instruments
	if len(shown) > footerInstruments {
		shown = shown[:footerInstruments-1]
	}
	for _, in := range shown {
		// Every column has a fixed width, so that nothing moves sideways
		// from one drawing to the next.
		line := fmt.Sprintf("%s %s ch %2d  oct %+3d  notes %2d  ",
			paint("1", fit(in.Name, 28)), faint(fit(in.Type, 8)), in.Channel, in.Octave, in.Sounding)
		line += busy(paint, "in", in.InputRate) + "  " + busy(paint, "MIDI", in.MIDIRate)
		line += faint("  " + in.Layout)
		lines = append(lines, line)
	}
	if more := len(s.Instruments) - len(shown); more > 0 {
		lines = append(lines, faint(fmt.Sprintf("and %d more; \"hidi status\" lists them all", more)))
	}
	return lines
}

// busy writes a rate, faint while nothing happens.
func busy(paint func(code, text string) string, label string, rate int) string {
	text := fmt.Sprintf("%s %4d/s", label, rate)
	if rate == 0 {
		return paint("90", text)
	}
	return text
}

// fit makes text exactly n characters wide, cutting it short or filling
// it up with spaces.
func fit(text string, n int) string {
	if utf8.RuneCountInString(text) > n {
		r := []rune(text)
		return string(r[:n-1]) + "…"
	}
	return text + strings.Repeat(" ", n-utf8.RuneCountInString(text))
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
