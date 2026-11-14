package config

import "fmt"

// Schema is the profile schema version this build writes and understands.
const Schema = 1

// migration upgrades the decoded contents of a profile file by one schema
// version, in place.
type migration func(doc map[string]any) error

// migrations maps a schema version to the step that upgrades a file from
// that version to the next. It must have an entry for every version from 1
// up to, but not including, Schema.
var migrations = map[int]migration{}

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
