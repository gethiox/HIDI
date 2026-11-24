package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// bindings reads a table of bindings. Entries are processed in sorted order
// so that the result does not depend on map iteration.
func (r *reader) bindings(where string, values map[string]any) []engine.Binding {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []engine.Binding
	// seen detects two entries that name the same source in different
	// spellings, such as BTN_A and BTN_SOUTH.
	seen := map[string]string{}
	for _, key := range keys {
		at := where + ", " + key
		source, err := parseSource(key)
		if err != nil {
			r.failf(at, "%v", err)
			continue
		}
		id := sourceID(source)
		if other, dup := seen[id]; dup {
			r.failf(at, "names the same control as %q", other)
			continue
		}
		seen[id] = key

		action, ok := r.action(at, &source, values[key])
		if !ok {
			continue
		}
		out = append(out, engine.Binding{Source: source, Action: action})
	}
	return out
}

// sourceID returns a string that is equal for two sources exactly when they
// would compete for the same input.
func sourceID(s engine.Source) string {
	mods := make([]string, len(s.Modifiers))
	for i, m := range s.Modifiers {
		mods[i] = m.String()
	}
	sort.Strings(mods)
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", s.Part, s.Code, s.Range, strings.Join(mods, "+"))
}

// parseSource reads the key of a binding entry:
//
//	[part ":"] {modifier "+"} control ["+" | "-"]
//
// For example "KEY_A", "KEY_LEFTSHIFT+KEY_A", "ABS_Y-" (the lower half of an
// axis) or "Touchpad:ABS_X" (a control on a named part of the device).
func parseSource(key string) (engine.Source, error) {
	var s engine.Source
	rest := key
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		s.Part, rest = rest[:i], rest[i+1:]
		if s.Part == "" {
			return s, fmt.Errorf("the part name before \":\" is empty")
		}
	}
	switch {
	case strings.HasSuffix(rest, "+"):
		s.Range, rest = engine.RangeUpper, rest[:len(rest)-1]
	case strings.HasSuffix(rest, "-"):
		s.Range, rest = engine.RangeLower, rest[:len(rest)-1]
	}

	names := strings.Split(rest, "+")
	for i, name := range names {
		code, err := control.ParseCode(strings.TrimSpace(name))
		if err != nil {
			return s, err
		}
		if i < len(names)-1 {
			if code.Kind != control.KindKey {
				return s, fmt.Errorf("%s cannot be held as part of a combination; only keys and buttons can", code)
			}
			s.Modifiers = append(s.Modifiers, code)
			continue
		}
		s.Code = code
	}
	if s.Range != engine.RangeWhole && s.Code.Kind == control.KindKey {
		return s, fmt.Errorf("%s is a key, so it has no upper or lower half; remove the trailing + or -", s.Code)
	}
	return s, nil
}

// actionKeys are the settings that choose what a binding does. Exactly one
// must be present in a binding written as a table.
var actionKeys = []string{"note", "cc", "pitch_bend", "program", "command"}

// action reads the value of a binding entry and fills in the shaping options
// of its source.
func (r *reader) action(at string, source *engine.Source, raw any) (engine.Action, bool) {
	switch v := raw.(type) {
	case string:
		// A bare string is "passthrough", "nothing", a command name or a
		// note name.
		switch v {
		case engine.PassthroughName:
			return engine.Passthrough{}, true
		case engine.NothingName:
			return engine.Nothing{}, true
		}
		if command, err := engine.ParseCommand(v); err == nil {
			return command, true
		}
		note, err := midi.ParseNote(v)
		if err != nil {
			r.failf(at, "%q is neither a command, a note, %q nor %q (%v)", v, engine.PassthroughName, engine.NothingName, err)
			return nil, false
		}
		return engine.Note{Note: note}, true
	case int64:
		note, err := midi.ParseNote(fmt.Sprint(v))
		if err != nil {
			r.failf(at, "%v", err)
			return nil, false
		}
		return engine.Note{Note: note}, true
	case map[string]any:
		return r.actionTable(at, source, v)
	}
	r.failf(at, "must be a note, a command or a table, not %s", describe(raw))
	return nil, false
}

func (r *reader) actionTable(at string, source *engine.Source, values map[string]any) (engine.Action, bool) {
	t := r.table(at, values)
	before := len(r.errs)

	var chosen []string
	for _, key := range actionKeys {
		if t.has(key) {
			chosen = append(chosen, key)
		}
	}
	if len(chosen) != 1 {
		r.failf(at, "must set exactly one of %s; found %d", strings.Join(actionKeys, ", "), len(chosen))
		return nil, false
	}

	source.Centered = t.boolean("centered", false)
	source.Invert = t.boolean("invert", false)
	source.Deadzone = t.number("deadzone", 0)
	offset := uint8(t.integer("channel_offset", 0, 0, 15))
	allowed := []string{chosen[0], "channel_offset", "centered", "invert", "deadzone"}

	var action engine.Action
	switch chosen[0] {
	case "note":
		note, err := midi.ParseNote(fmt.Sprint(values["note"]))
		if err != nil {
			r.failf(at, "%v", err)
		}
		allowed = append(allowed, "velocity", "touch")
		action = engine.Note{Note: note, ChannelOffset: offset, Velocity: uint8(t.integer("velocity", 0, 1, 127)), Touch: t.boolean("touch", false)}
	case "cc":
		action = engine.ControlChange{Controller: uint8(t.integer("cc", 0, 0, 127)), ChannelOffset: offset}
	case "program":
		action = engine.ProgramChange{Program: uint8(t.integer("program", 0, 0, 127)), ChannelOffset: offset}
	case "pitch_bend":
		bend := engine.PitchBend{ChannelOffset: offset}
		switch values["pitch_bend"] {
		case true, "up":
		case "down":
			bend.Down = true
		default:
			r.failf(at, "pitch_bend must be true, \"up\" or \"down\"")
		}
		action = bend
	case "command":
		command, err := engine.ParseCommand(t.str("command", ""))
		if err != nil {
			r.failf(at, "%v", err)
		}
		action = command
	}
	t.done(allowed...)
	return action, len(r.errs) == before
}
