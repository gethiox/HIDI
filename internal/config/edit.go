package config

import (
	"errors"
	"fmt"
	"strings"
)

// The functions in this file change one thing in the text of a profile
// and leave everything else as it is, byte for byte: the notes a person
// wrote into the file, the order of things, the spacing. A profile is
// changed from the window through them, never by writing the whole file
// anew from what HIDI understood of it.
//
// They know the shapes a profile has, not all of TOML. Where the text is
// written in a way they do not follow, such as a value that runs over
// several lines, they change nothing and say so, and the file is still
// there to change by hand.

// Section names a table of a profile that holds settings by key.
type Section struct {
	// Table is "global", "defaults", "match", or "layout.bindings".
	Table string
	// Layout names the layout for "layout.bindings": the table belongs to
	// the [[layout]] whose name it is.
	Layout string
}

// Global, Defaults and Bindings name the sections that are changed.
var (
	Global   = Section{Table: "global"}
	Defaults = Section{Table: "defaults"}
)

// Bindings names the bindings of a layout.
func Bindings(layout string) Section { return Section{Table: "layout.bindings", Layout: layout} }

func (s Section) String() string {
	if s.Layout != "" {
		return fmt.Sprintf("the bindings of layout %q", s.Layout)
	}
	return "[" + s.Table + "]"
}

// ErrNotEditable is wrapped by the errors that mean the text is written
// in a way these functions do not follow.
var ErrNotEditable = errors.New("the file has to be changed by hand")

// Set gives a key in a section a value, written as TOML, such as `"C3"`
// or `{ cc = 1, deadzone = 0.1 }`. A key that is there keeps its place
// and the note behind it; a new one goes to the end of the section. The
// result is checked: text that is no longer a valid profile is not
// returned.
func Set(text []byte, section Section, key, value string) ([]byte, error) {
	return edit(text, section, key, &value)
}

// Remove takes a key out of a section, with its line. A key that is not
// there is not an error.
func Remove(text []byte, section Section, key string) ([]byte, error) {
	return edit(text, section, key, nil)
}

// Get returns the value a key has in a section, as it is written in the
// text, quotes and all, and whether the section has the key.
func Get(text []byte, section Section, key string) (value string, found bool, err error) {
	lines := strings.SplitAfter(string(text), "\n")
	start, end, err := findSection(lines, section)
	if err != nil {
		return "", false, err
	}
	for i := start; i < end; i++ {
		entry, ok, err := parseEntry(lines[i])
		if err != nil {
			return "", false, fmt.Errorf("line %d: %w", i+1, err)
		}
		if ok && sameKey(section, entry.key, key) {
			return entry.value, true, nil
		}
	}
	return "", false, nil
}

func edit(text []byte, section Section, key string, value *string) ([]byte, error) {
	if key == "" || strings.ContainsAny(key, "\n\r") || value != nil && strings.ContainsAny(*value, "\n\r") {
		return nil, errors.New("a key or value with a line break cannot be written")
	}
	lines := strings.SplitAfter(string(text), "\n")
	start, end, err := findSection(lines, section)
	if err != nil {
		return nil, err
	}
	// The key, if the section has it.
	found := -1
	for i := start; i < end; i++ {
		entry, ok, err := parseEntry(lines[i])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if ok && sameKey(section, entry.key, key) {
			found = i
			break
		}
	}
	switch {
	case value == nil && found < 0:
		return text, nil
	case value == nil:
		lines = append(lines[:found], lines[found+1:]...)
	case found >= 0:
		entry, _, _ := parseEntry(lines[found])
		lines[found] = entry.before + *value + entry.after
	default:
		// After the last entry of the section, so that blank lines and a
		// note that stands above the next section stay with it. A
		// section without entries gets the line at its start.
		at := start
		for i := start; i < end; i++ {
			if _, ok, _ := parseEntry(lines[i]); ok {
				at = i + 1
			}
		}
		// The line ends as the lines of the file do.
		ending := "\n"
		if strings.Contains(string(text), "\r\n") {
			ending = "\r\n"
		}
		if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
			lines[at-1] += ending // the file ended without a line break
		}
		line := writeKey(key) + " = " + *value + ending
		lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	}
	out := []byte(strings.Join(lines, ""))
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the change would not leave a valid profile:\n%w", err)
	}
	return out, nil
}

// writeKey writes a key as TOML: bare where that is allowed, quoted
// otherwise, as for "KEY_LEFTSHIFT+KEY_F1" or "ABS_RX+".
func writeKey(key string) string {
	bare := key != ""
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			bare = false
		}
	}
	if bare {
		return key
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(key) + `"`
}

// findSection returns the lines of a section: from the one after its
// header to the one before the next header.
func findSection(lines []string, section Section) (start, end int, err error) {
	inLayout := "" // the name of the [[layout]] the lines belong to
	layoutAt := -1 // where that [[layout]] begins
	named := false // whether its name was seen
	start = -1
	for i, line := range lines {
		header, array, ok := parseHeader(line)
		if !ok {
			if layoutAt >= 0 && !named {
				if entry, ok, _ := parseEntry(line); ok && entry.key == "name" {
					inLayout, named = unquote(entry.value), true
				}
			}
			continue
		}
		if start >= 0 {
			return start, i, nil
		}
		switch {
		case header == "layout" && array:
			inLayout, layoutAt, named = "", i, false
		case header == section.Table && !array && (section.Layout == "" || section.Layout == inLayout):
			start = i + 1
		}
	}
	if start >= 0 {
		return start, len(lines), nil
	}
	return 0, 0, fmt.Errorf("%s is not in the file as a section of its own: %w", section, ErrNotEditable)
}

// parseHeader reads a line like "[defaults]" or "[[layout]]".
func parseHeader(line string) (name string, array, ok bool) {
	s := strings.TrimSpace(stripComment(line))
	switch {
	case strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]"):
		return strings.TrimSpace(s[2 : len(s)-2]), true, true
	case strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"):
		return strings.TrimSpace(s[1 : len(s)-1]), false, true
	}
	return "", false, false
}

// entry is a line that sets a key, in its parts: before is everything up
// to the value, after everything behind it, a note and the line break
// among it.
type entry struct {
	key, value    string
	before, after string
}

// parseEntry reads a line like `KEY_A = "C3"  # middle C`. It reports
// false for a line that sets nothing, and an error for one whose value
// does not end on the line.
func parseEntry(line string) (entry, bool, error) {
	rest := strings.TrimLeft(line, " \t")
	if rest == "" || rest[0] == '#' || rest[0] == '[' || rest[0] == '\n' || rest[0] == '\r' {
		return entry{}, false, nil
	}
	// The key: bare, or in quotes.
	var key string
	n := 0
	switch rest[0] {
	case '"', '\'':
		end := closingQuote(rest, 0)
		if end < 0 {
			return entry{}, false, nil
		}
		key, n = unquote(rest[:end+1]), end+1
	default:
		for n < len(rest) && rest[n] != '=' && rest[n] != ' ' && rest[n] != '\t' {
			n++
		}
		key = rest[:n]
	}
	afterKey := strings.TrimLeft(rest[n:], " \t")
	if !strings.HasPrefix(afterKey, "=") {
		return entry{}, false, nil // a dotted key, or not an entry at all
	}
	valueStart := len(line) - len(strings.TrimLeft(afterKey[1:], " \t"))
	// The value runs to a note or the end of the line, outside of quotes
	// and with every bracket closed.
	depth, i := 0, valueStart
	for i < len(line) {
		switch c := line[i]; {
		case c == '"' || c == '\'':
			if strings.HasPrefix(line[i:], `"""`) || strings.HasPrefix(line[i:], `'''`) {
				return entry{}, false, fmt.Errorf("the value of %s runs over several lines: %w", key, ErrNotEditable)
			}
			end := closingQuote(line, i)
			if end < 0 {
				return entry{}, false, fmt.Errorf("the value of %s has a quote that is not closed: %w", key, ErrNotEditable)
			}
			i = end
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '#' && depth == 0, c == '\n', c == '\r':
			goto done
		}
		i++
	}
done:
	if depth != 0 {
		return entry{}, false, fmt.Errorf("the value of %s runs over several lines: %w", key, ErrNotEditable)
	}
	valueEnd := valueStart + len(strings.TrimRight(line[valueStart:i], " \t"))
	if valueEnd == valueStart {
		return entry{}, false, nil
	}
	return entry{key: key, value: line[valueStart:valueEnd], before: line[:valueStart], after: line[valueEnd:]}, true, nil
}

// closingQuote returns where the string that opens at i ends, or -1.
func closingQuote(s string, i int) int {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] == '\\' && quote == '"':
			j++
		case s[j] == quote:
			return j
		case s[j] == '\n':
			return -1
		}
	}
	return -1
}

// unquote returns what a quoted string says; other text as it is.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(s[1 : len(s)-1])
	}
	return s
}

// stripComment removes a note at the end of a header line.
func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

// sameKey reports whether two keys of a section name the same thing: the
// same text, or in a table of bindings the same control. A control has
// several spellings, BTN_A and BTN_SOUTH, KEY_0x120 and BTN_TRIGGER, and
// either case; a file may use one and whoever asks for a change another.
func sameKey(section Section, a, b string) bool {
	if a == b {
		return true
	}
	if section.Table != Global.Table && section.Table != Bindings("").Table {
		return false
	}
	sa, errA := parseSource(a)
	sb, errB := parseSource(b)
	return errA == nil && errB == nil && sourceID(sa) == sourceID(sb)
}
