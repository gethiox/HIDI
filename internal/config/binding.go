package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// bindings reads a table of bindings. Entries are processed in sorted order
// so that the result does not depend on map iteration.
func (r *reader) bindings(where, layout string, values map[string]any) []engine.Binding {
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
		// An axis has the dead zone of the profile unless its binding
		// names one.
		if table, _ := values[key].(map[string]any); table["deadzone"] != nil {
			if r.own == nil {
				r.own = map[ownKey]bool{}
			}
			r.own[ownKey{layout, id}] = true
		} else if source.Code.Kind == control.KindAxis {
			source.Deadzone = r.deadzone
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

// SourceKey writes a source as the key of its binding, the way
// parseSource reads it: "KEY_LEFTSHIFT+KEY_F1", "ABS_Y-", "Touchpad:ABS_X".
func SourceKey(s engine.Source) string {
	var b strings.Builder
	if s.Part != "" {
		b.WriteString(s.Part + ":")
	}
	for _, m := range s.Modifiers {
		b.WriteString(m.String() + "+")
	}
	b.WriteString(s.Code.String())
	switch s.Range {
	case engine.RangeUpper:
		b.WriteString("+")
	case engine.RangeLower:
		b.WriteString("-")
	}
	return b.String()
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
	// Where an axis triggers what a key would: a note, a program change
	// or a command.
	threshold := func() {
		allowed = append(allowed, "threshold")
		if !t.has("threshold") {
			return
		}
		source.Threshold = t.number("threshold", 0)
		switch {
		case source.Code.Kind == control.KindKey:
			r.failf(at, "threshold is for an axis; %s is a key, which is down or not", source.Code)
		case !(source.Threshold > 0 && source.Threshold < 1):
			r.failf(at, "threshold must be above 0 and below 1, not %v", source.Threshold)
		}
	}

	var action engine.Action
	switch chosen[0] {
	case "note":
		note, err := midi.ParseNote(fmt.Sprint(values["note"]))
		if err != nil {
			r.failf(at, "%v", err)
		}
		allowed = append(allowed, "velocity", "touch")
		threshold()
		action = engine.Note{Note: note, ChannelOffset: offset, Velocity: uint8(t.integer("velocity", 0, 1, 127)), Touch: t.boolean("touch", false)}
	case "cc":
		action = engine.ControlChange{Controller: uint8(t.integer("cc", 0, 0, 127)), ChannelOffset: offset}
	case "program":
		threshold()
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
		threshold()
		command, err := engine.ParseCommand(t.str("command", ""))
		if err != nil {
			r.failf(at, "%v", err)
		}
		action = command
	}
	t.done(allowed...)
	return action, len(r.errs) == before
}

// toggle finds the keys that turn HIDI off and on with a profile: those of
// its binding of the toggle command. HIDI is turned off and on for every
// instrument and in whatever layout, also while nothing else of the
// profile is in force, so the binding is one, stands among those for
// every layout, and names keys of the main part and nothing else.
func (r *reader) toggle(p *engine.Profile) []control.Code {
	var keys []control.Code
	for _, l := range p.Layouts {
		for _, b := range l.Bindings {
			if b.Action == engine.CommandToggle {
				r.failf(fmt.Sprintf("layout %q, %s", l.Name, b.Source.Code), "%s belongs in [global]: it has to work in every layout", engine.ToggleName)
			}
		}
	}
	for _, b := range p.Global {
		at := "global, " + b.Source.Code.String()
		if b.Action != engine.CommandToggle {
			r.failf(at, "only %s belongs in [global]; every other binding is written in the layouts it works in, each of which has its own", engine.ToggleName)
			continue
		}
		switch {
		case keys != nil:
			r.failf(at, "a profile has one %s; this is another", engine.ToggleName)
		case b.Source.Part != "" || b.Source.Range != engine.RangeWhole || b.Source.Code.Kind != control.KindKey:
			r.failf(at, "%s is bound to a key or a button of the main part, or to several pressed together", engine.ToggleName)
		default:
			keys = append(slices.Clone(b.Source.Modifiers), b.Source.Code)
		}
	}
	return keys
}
