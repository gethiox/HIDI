package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/gethiox/HIDI/internal/control"
)

// Matching names the section that says which devices a profile is for.
var Matching = Section{Table: "match"}

// ForDevice turns the text of a profile into that of a profile for one
// product: it carries the name given, and is for the devices of that kind
// with that vendor and product and no others, whatever the profile it is
// made of was for. Everything else stays as it is written.
//
// A device that does not say who made it cannot be told from others, and
// gets no profile of its own.
func ForDevice(text []byte, name string, kind control.DeviceType, hardware control.HardwareID) ([]byte, error) {
	if hardware.Vendor == 0 && hardware.Product == 0 {
		return nil, errors.New("the device does not say which product it is, so a profile cannot be for it alone")
	}
	if kind == control.DeviceUnknown {
		return nil, errors.New("the kind of the device is not known")
	}
	out, err := Upgrade(text)
	if err != nil {
		return nil, err
	}
	if out, err = setTop(out, "name", quote(name), "schema"); err != nil {
		return nil, err
	}
	if _, _, err := findSection(strings.SplitAfter(string(out), "\n"), Matching); err != nil {
		if out, err = addSection(out, Matching.Table); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"bus", "version", "serial"} {
		if out, err = Remove(out, Matching, key); err != nil {
			return nil, err
		}
	}
	for _, set := range []struct{ key, value string }{
		{"type", strconv.Quote(kind.String())},
		{"vendor", fmt.Sprintf("0x%04x", hardware.Vendor)},
		{"product", fmt.Sprintf("0x%04x", hardware.Product)},
	} {
		if out, err = Set(out, Matching, set.key, set.value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// quote writes a text as a TOML string on one line, without what cannot
// stand in one.
func quote(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, s)
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// setTop writes a setting of the profile itself, one of those that stand
// before the first table. One that is there keeps its place; a new one
// goes behind the last of the settings named in after, or to the very
// start if none of them is there.
func setTop(text []byte, key, value string, after ...string) ([]byte, error) {
	lines := strings.SplitAfter(string(text), "\n")
	at := 0
	for i, line := range lines {
		if _, _, header := parseHeader(line); header {
			break // the settings of the profile itself end where the first table begins
		}
		entry, ok, err := parseEntry(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if !ok {
			continue
		}
		if entry.key == key {
			lines[i] = entry.before + value + entry.after
			return checked(lines)
		}
		for _, name := range after {
			if entry.key == name {
				at = i + 1
			}
		}
	}
	ending := lineEnding(text)
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += ending
	}
	lines = append(lines[:at], append([]string{key + " = " + value + ending}, lines[at:]...)...)
	return checked(lines)
}

// addSection gives a profile an empty table, before the first table it
// has: after the settings of the profile itself.
func addSection(text []byte, name string) ([]byte, error) {
	lines := strings.SplitAfter(string(text), "\n")
	ending := lineEnding(text)
	at := len(lines)
	for i, line := range lines {
		if _, _, header := parseHeader(line); header {
			at = i
			break
		}
	}
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += ending
	}
	block := []string{"[" + name + "]" + ending, ending}
	lines = append(lines[:at], append(block, lines[at:]...)...)
	return checked(lines)
}

// DeviceFile returns the name of the file a profile for one product is
// kept in: the name of the device as far as it can stand in a file name,
// and the numbers of its vendor and product, which tell two devices of
// one name apart.
func DeviceFile(name string, hardware control.HardwareID) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	if b.Len() > 48 {
		return fmt.Sprintf("%s-%04x-%04x.toml", strings.TrimRight(b.String()[:48], "-"), hardware.Vendor, hardware.Product)
	}
	if b.Len() == 0 {
		b.WriteString("device")
	}
	return fmt.Sprintf("%s-%04x-%04x.toml", b.String(), hardware.Vendor, hardware.Product)
}
