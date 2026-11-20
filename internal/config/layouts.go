package config

import (
	"fmt"
	"strconv"
	"strings"
)

// layoutBlock is the lines of one [[layout]]: its header, its settings
// and its bindings, with the note that stands directly above it.
type layoutBlock struct {
	name       string
	start, end int // lines[start:end]
}

// layoutBlocks finds the layouts in the lines of a profile, in order. A
// layout ends where the next table begins that is not one of its own.
func layoutBlocks(lines []string) []layoutBlock {
	var blocks []layoutBlock
	open := false
	for i, line := range lines {
		header, array, ok := parseHeader(line)
		if !ok {
			if open {
				b := &blocks[len(blocks)-1]
				if entry, ok, _ := parseEntry(line); ok && entry.key == "name" && b.name == "" && !inBindings(lines, b.start, i) {
					b.name = unquote(entry.value)
				}
			}
			continue
		}
		own := !array && (header == "layout.bindings" || strings.HasPrefix(header, "layout."))
		if open && !own {
			blocks[len(blocks)-1].end = i
			open = false
		}
		if header == "layout" && array {
			// The note above the header, without a blank line between,
			// belongs to the layout.
			start := i
			for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
				start--
			}
			if open = true; len(blocks) > 0 && blocks[len(blocks)-1].end > start {
				blocks[len(blocks)-1].end = start
			}
			blocks = append(blocks, layoutBlock{start: start, end: len(lines)})
		}
	}
	return blocks
}

// inBindings reports whether line at lies behind the header of a table
// of the layout that began at start: a key called "name" there is a
// binding or something else, not the layout's name.
func inBindings(lines []string, start, at int) bool {
	seen := 0
	for i := start; i < at; i++ {
		if _, _, ok := parseHeader(lines[i]); ok {
			seen++
		}
	}
	return seen > 1
}

func lineEnding(text []byte) string {
	if strings.Contains(string(text), "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// checked returns the lines as text if they still make a valid profile.
func checked(lines []string) ([]byte, error) {
	out := []byte(strings.Join(lines, ""))
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the change would not leave a valid profile:\n%w", err)
	}
	return out, nil
}

// AddLayout adds an empty layout behind the last one.
func AddLayout(text []byte, name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\n\r") {
		return nil, fmt.Errorf("a layout needs a name of one line")
	}
	lines := strings.SplitAfter(string(text), "\n")
	blocks := layoutBlocks(lines)
	for _, b := range blocks {
		if b.name == name {
			return nil, fmt.Errorf("there is a layout %q already", name)
		}
	}
	ending := lineEnding(text)
	at := len(lines)
	if len(blocks) > 0 {
		at = blocks[len(blocks)-1].end
	}
	// Behind the last line of the layout before that says something, so
	// that what follows keeps its blank line.
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += ending
	}
	added := []string{ending, "[[layout]]" + ending, "name = " + strconv.Quote(name) + ending, ending, "[layout.bindings]" + ending}
	lines = append(lines[:at], append(added, lines[at:]...)...)
	return checked(lines)
}

// RemoveLayout takes a layout out of the profile, with its bindings. The
// last layout cannot be removed, and neither can the one the profile
// starts with; make another the first before.
func RemoveLayout(text []byte, name string) ([]byte, error) {
	lines := strings.SplitAfter(string(text), "\n")
	for _, b := range layoutBlocks(lines) {
		if b.name == name {
			return checked(append(lines[:b.start:b.start], lines[b.end:]...))
		}
	}
	return nil, fmt.Errorf("layout %q was not found; %w", name, ErrNotEditable)
}

// MoveLayout moves a layout one place towards the start of the profile
// (by -1) or towards its end (by 1). The layouts follow each other in the
// order of the file when the player switches between them.
func MoveLayout(text []byte, name string, by int) ([]byte, error) {
	lines := strings.SplitAfter(string(text), "\n")
	blocks := layoutBlocks(lines)
	at := -1
	for i, b := range blocks {
		if b.name == name {
			at = i
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("layout %q was not found; %w", name, ErrNotEditable)
	}
	other := at + by
	if by != 1 && by != -1 || other < 0 || other >= len(blocks) {
		return text, nil // already at that end
	}
	first, second := blocks[min(at, other)], blocks[max(at, other)]
	ending := lineEnding(text)
	part := func(from, to int) []string {
		out := append([]string(nil), lines[from:to]...)
		if n := len(out); n > 0 && !strings.HasSuffix(out[n-1], "\n") {
			out[n-1] += ending // the last line of the file
		}
		return out
	}
	// The layout that goes up must end in a blank line, as the one it
	// passes did, so that the two stay apart.
	up := part(second.start, second.end)
	if n := len(up); n > 0 && strings.TrimSpace(up[n-1]) != "" {
		up = append(up, ending)
	}
	var out []string
	out = append(out, lines[:first.start]...)
	out = append(out, up...)
	out = append(out, part(first.end, second.start)...)
	out = append(out, part(first.start, first.end)...)
	out = append(out, lines[second.end:]...)
	return checked(out)
}
