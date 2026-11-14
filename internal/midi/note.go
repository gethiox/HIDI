package midi

import (
	"fmt"
	"strconv"
	"strings"
)

// Note is a MIDI note number in the range 0-127.
//
// HIDI names octaves so that note 0 is C-2 and middle C (note 60) is C3,
// which is the convention most DAWs display.
type Note uint8

// MaxNote is the highest valid note number.
const MaxNote Note = 127

var pitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// naturalSemitones maps the letters A-G to their offset from C.
var naturalSemitones = map[byte]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}

// Pitch returns the pitch class name of the note, such as "C#".
func (n Note) Pitch() string { return pitchNames[n%12] }

// Octave returns the octave of the note, from -2 to 8.
func (n Note) Octave() int { return int(n)/12 - 2 }

// String returns the note name, such as "C#3".
func (n Note) String() string { return n.Pitch() + strconv.Itoa(n.Octave()) }

// ParseNote converts a note name or number to a [Note].
//
// A name is a letter A-G in either case, an optional accidental ("#" for
// sharp, "b" for flat) and an octave from -2 to 8, for example "C3", "f#-1"
// or "Bb2". A number is the decimal note number, 0-127.
func ParseNote(s string) (Note, error) {
	if s == "" {
		return 0, fmt.Errorf("empty note")
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > int(MaxNote) {
			return 0, fmt.Errorf("note number %d is outside 0-127", n)
		}
		return Note(n), nil
	}

	semitone, ok := naturalSemitones[strings.ToUpper(s[:1])[0]]
	if !ok {
		return 0, fmt.Errorf("invalid note %q: want a name like C3 or F#-1, or a number 0-127", s)
	}
	rest := s[1:]
	switch {
	case strings.HasPrefix(rest, "#"):
		semitone++
		rest = rest[1:]
	case strings.HasPrefix(rest, "b"):
		semitone--
		rest = rest[1:]
	}
	octave, err := strconv.Atoi(rest)
	if err != nil {
		return 0, fmt.Errorf("invalid note %q: want a name like C3 or F#-1, or a number 0-127", s)
	}

	n := (octave+2)*12 + semitone
	if n < 0 || n > int(MaxNote) {
		return 0, fmt.Errorf("note %q is outside the MIDI range C-2 to G8", s)
	}
	return Note(n), nil
}
