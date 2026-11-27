package config

import (
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Upgrade writes a profile in the form this version of HIDI writes, line
// by line like every change to a profile, so that what a person wrote
// into it stays. A profile in that form already is returned as it is.
//
// HIDI reads a profile in an older form as it is, through the same steps
// in what it decoded; Upgrade is for the text, before it is changed in
// the window or made into another profile, so that a change lands where
// the new form has it.
func Upgrade(text []byte) ([]byte, error) {
	p, err := Parse(text)
	if err != nil {
		return nil, err
	}
	if p.WrittenFor >= Schema {
		return text, nil
	}
	out := text
	if p.WrittenFor < 2 {
		if out, err = spreadGlobal(out, p); err != nil {
			return nil, err
		}
	}
	return setTop(out, "schema", strconv.Itoa(Schema))
}

// spreadGlobal moves the bindings of [global], except that of hidi_toggle,
// into every layout that does not bind the key itself, in the order the
// file has them, as layoutsOwnTheirBindings does with what was decoded.
func spreadGlobal(text []byte, p *Profile) ([]byte, error) {
	lines := strings.SplitAfter(string(text), "\n")
	start, end, err := findSection(lines, Global)
	if err != nil {
		return text, nil // no [global] of its own: nothing to move
	}
	var moved []entry
	for i := start; i < end; i++ {
		e, ok, err := parseEntry(lines[i])
		if err != nil {
			return nil, err
		}
		if ok && !toggleValue(e.value) {
			moved = append(moved, e)
		}
	}
	out := text
	if len(moved) == 0 {
		return out, nil
	}
	for _, l := range p.Engine.Layouts {
		if _, _, err := findSection(strings.SplitAfter(string(out), "\n"), Bindings(l.Name)); err != nil {
			if out, err = addBindings(out, l.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range moved {
		for _, l := range p.Engine.Layouts {
			if _, own, err := Get(out, Bindings(l.Name), e.key); err != nil {
				return nil, err
			} else if own {
				continue
			}
			if out, err = Set(out, Bindings(l.Name), e.key, e.value); err != nil {
				return nil, err
			}
		}
		if out, err = Remove(out, Global, e.key); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// toggleValue reports whether a binding, as it is written, runs
// hidi_toggle.
func toggleValue(value string) bool {
	var v map[string]any
	if toml.Unmarshal([]byte("v = "+value), &v) != nil {
		return false
	}
	return isToggle(v["v"])
}
