package config

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

//go:embed builtin/*.toml
var builtinFiles embed.FS

// Builtin returns the profiles that ship inside HIDI, in file name order.
// They give every keyboard and game controller a working instrument without
// any configuration, and serve as examples to copy from.
func Builtin() ([]*Profile, error) {
	names, err := fs.Glob(builtinFiles, "builtin/*.toml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	profiles := make([]*Profile, 0, len(names))
	for _, name := range names {
		data, err := builtinFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		p, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("built-in profile %s:\n%w", name, err)
		}
		p.Source = BuiltinSource + path.Base(name)
		profiles = append(profiles, p)
	}
	return profiles, nil
}
