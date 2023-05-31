package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var stringToNoteRegex = regexp.MustCompile(`^(?P<pitch>[a-zA-Z]#?)(?P<octave>-?\d)$`)

func StringToNote(note string) (byte, error) {
	match := stringToNoteRegex.FindStringSubmatch(note)
	if len(match) == 0 {
		return 0, fmt.Errorf("unsupported format, bruh")
	}

	pitch := strings.ToUpper(match[1])
	octave, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, fmt.Errorf("parsing octave failed: %w", err)
	}

	calculated := (uint8(octave)+2)*12 + pitchToVal[pitch]
	if calculated < 0 || calculated > 127 {
		return 0, fmt.Errorf("note outside of midi range 0-127: %d", calculated)
	}
	return calculated, nil
}

func StringToNoteUnsafe(note string) byte {
	n, err := StringToNote(note)
	if err != nil {
		panic(err)
	}
	return n
}

var valToPitch = map[uint8]string{
	0: "C", 1: "C#", 2: "D", 3: "D#",
	4: "E", 5: "F", 6: "F#", 7: "G",
	8: "G#", 9: "A", 10: "A#", 11: "B",
}

var pitchToVal = map[string]uint8{
	"C": 0, "C#": 1, "D": 2, "D#": 3,
	"E": 4, "F": 5, "F#": 6, "G": 7,
	"G#": 8, "A": 9, "A#": 10, "B": 11,
}

func NoteToPitch(note byte) string {
	return valToPitch[note%12]
}

func NoteToOctave(note byte) int {
	return int(note/12) - 2
}
