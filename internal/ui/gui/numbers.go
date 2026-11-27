package gui

import (
	"math"
	"strconv"
	"strings"

	"github.com/gethiox/HIDI/internal/midi"
)

// numberSpec is what a field for a number takes, and how the number is
// written in it.
type numberSpec struct {
	// lo and hi are the least and the most the number may be.
	lo, hi float64
	// whole takes whole numbers only; otherwise a fraction may be
	// typed, with a point or a comma.
	whole bool
	// plus writes a number above zero with a "+" in front, as an octave
	// or a transposition is shown.
	plus bool
	// unit is shown behind the field: "%" for one.
	unit string
}

// rules are what may be typed into the field of a number: no more
// characters than the longest number in range has, so that 1 to 127 takes
// three digits, and two places of a fraction behind a separator.
func (s numberSpec) rules() fieldRules {
	kind := decimalNumber
	if s.whole {
		kind = wholeNumber
	}
	wide := func(v float64) int { return len(strconv.FormatFloat(math.Trunc(v), 'f', 0, 64)) }
	most := max(wide(s.lo), wide(s.hi))
	if !s.whole {
		most += 3
	}
	return fieldRules{kind: kind, signed: s.lo < 0, most: most}
}

// format writes a number as its field shows it: a whole number as it is,
// a fraction with as few places as it needs, two at the most.
func (s numberSpec) format(v float64) string {
	if s.whole {
		v = math.Round(v)
	}
	text := strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	if s.plus && v > 0 {
		text = "+" + text
	}
	return text
}

// parse reads what was typed into the field of a number, and reports
// whether it is one the field takes.
func (s numberSpec) parse(text string) (float64, bool) {
	text = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), "+")
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(v) || v < s.lo || v > s.hi {
		return 0, false
	}
	if s.whole && v != math.Trunc(v) {
		return 0, false
	}
	return v, true
}

// zoneSpec is the field of a dead zone, in percent of the travel.
var zoneSpec = numberSpec{lo: 0, hi: 95, unit: "%"}

// thresholdSpec is the field of the point at which an axis counts as
// pressed, in percent of its travel; neither end is a point.
var thresholdSpec = numberSpec{lo: 1, hi: 99, unit: "%"}

// stepZone steps a dead zone in percent, within zoneSpec.
func stepZone(percent float64, by int) float64 {
	return stepTo(percent, by, 5, zoneSpec.lo, zoneSpec.hi)
}

// stepTo steps v to the next multiple of step up or down, so that a typed
// 2.5 goes to 5 and 0 by steps of 5, and keeps it within lo and hi.
func stepTo(v float64, by int, step, lo, hi float64) float64 {
	next := math.Floor(v/step+1e-9) * step
	if by > 0 {
		next += step
	} else if math.Abs(next-v) < 1e-9 {
		next -= step
	}
	return min(max(next, lo), hi)
}

// noteName reads a note written as the window shows notes, in either
// case: a letter, "#" or "b", and an octave, such as "C3" or "f#-1". A
// note number is not taken where a name is asked for.
func noteName(text string) (midi.Note, bool) {
	text = strings.TrimSpace(text)
	if _, err := strconv.Atoi(text); err == nil || text == "" {
		return 0, false
	}
	n, err := midi.ParseNote(text)
	return n, err == nil
}
