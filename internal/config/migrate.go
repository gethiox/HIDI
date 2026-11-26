package config

import (
	"fmt"

	"github.com/gethiox/HIDI/internal/engine"
)

// Schema is the profile schema version this build writes and understands.
//
//   - 1: [global] holds bindings that every layout has, unless the layout
//     binds the key itself.
//   - 2: every layout holds all of its bindings, commands included, and
//     [global] holds only the binding of hidi_toggle, which works the same
//     in every layout and while HIDI is off.
const Schema = 2

// migration upgrades the decoded contents of a profile file by one schema
// version, in place.
type migration func(doc map[string]any) error

// migrations maps a schema version to the step that upgrades a file from
// that version to the next. It must have an entry for every version from 1
// up to, but not including, Schema.
var migrations = map[int]migration{1: layoutsOwnTheirBindings}

// layoutsOwnTheirBindings moves the bindings of [global] into every layout,
// except that of hidi_toggle. A layout that binds a key itself keeps its
// own binding, as it did when [global] was shared: a binding of [global]
// only applied where the layout had none of the same key. A file shaped
// in some other way is left as it is, for the reader to say what is wrong.
func layoutsOwnTheirBindings(doc map[string]any) error {
	global, ok := doc["global"].(map[string]any)
	if !ok {
		return nil
	}
	layouts, ok := doc["layout"].([]any)
	if !ok || len(layouts) == 0 {
		return nil
	}
	for key, value := range global {
		if isToggle(value) {
			continue
		}
		for _, raw := range layouts {
			layout, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			bindings, ok := layout["bindings"].(map[string]any)
			if !ok {
				if _, there := layout["bindings"]; there {
					continue // not a table: the reader says so
				}
				bindings = map[string]any{}
				layout["bindings"] = bindings
			}
			if _, own := bindings[key]; !own {
				bindings[key] = value
			}
		}
		delete(global, key)
	}
	return nil
}

// isToggle reports whether a decoded binding runs hidi_toggle, written
// either way a command can be.
func isToggle(value any) bool {
	switch v := value.(type) {
	case string:
		return v == engine.ToggleName
	case map[string]any:
		return v["command"] == engine.ToggleName
	}
	return false
}

// migrate brings a decoded profile file up to the current schema and returns
// the version the file was written for.
func migrate(doc map[string]any, steps map[int]migration, current int) (from int, err error) {
	raw, ok := doc["schema"]
	if !ok {
		return 0, fmt.Errorf("missing \"schema\": add \"schema = %d\" at the top of the file", current)
	}
	version, ok := raw.(int64)
	if !ok {
		return 0, fmt.Errorf("\"schema\" must be a whole number, not %s", describe(raw))
	}
	switch {
	case version < 1:
		return 0, fmt.Errorf("\"schema\" is %d; the first schema version is 1", version)
	case version > int64(current):
		return 0, fmt.Errorf("the file uses schema %d, but this version of HIDI only understands up to %d; update HIDI to use it", version, current)
	}

	from = int(version)
	for v := from; v < current; v++ {
		step, ok := steps[v]
		if !ok {
			return from, fmt.Errorf("internal error: no migration from schema %d", v)
		}
		if err := step(doc); err != nil {
			return from, fmt.Errorf("upgrading from schema %d to %d: %w", v, v+1, err)
		}
		doc["schema"] = int64(v + 1)
	}
	return from, nil
}

// describe names the TOML type of a decoded value for error messages.
func describe(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case int64:
		return "a whole number"
	case float64:
		return "a number"
	case bool:
		return "true or false"
	case map[string]any:
		return "a table"
	case []any:
		return "a list"
	}
	return fmt.Sprintf("a value of type %T", v)
}
