package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultDir returns the directory HIDI reads the user's profiles from:
// hidi/profiles in the user's configuration directory, which is
// ~/.config on Linux and %AppData% on Windows.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the configuration directory: %w", err)
	}
	return filepath.Join(base, "hidi", "profiles"), nil
}

// Problem is a profile file that cannot be used.
type Problem struct {
	// File is the path of the file, or of the directory if it could not be
	// read at all.
	File string
	// Err says what is wrong. For a file with several mistakes it has one
	// line for each.
	Err error
}

func (p Problem) Error() string { return p.File + ":\n" + indent(p.Err.Error()) }

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// LoadDir reads the profiles in a directory: every file ending in .toml,
// in file name order. A file that cannot be used is reported as a problem
// and does not keep the others from loading. A directory that does not
// exist holds no profiles and is not a problem.
func LoadDir(dir string) (profiles []*Profile, problems []Problem) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []Problem{{File: dir, Err: err}}
	}
	for _, entry := range entries { // ReadDir sorts by name
		if entry.IsDir() || !isProfileFile(entry.Name()) {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(file)
		if err != nil {
			problems = append(problems, Problem{File: file, Err: err})
			continue
		}
		p, err := Parse(data)
		if err != nil {
			problems = append(problems, Problem{File: file, Err: err})
			continue
		}
		p.Source = file
		profiles = append(profiles, p)
	}
	return profiles, problems
}

// isProfileFile reports whether a file name is that of a profile. Names
// starting with a dot are left out: editors keep their temporary files
// under such names.
func isProfileFile(name string) bool {
	return strings.HasSuffix(name, ".toml") && !strings.HasPrefix(name, ".")
}

// Load returns the built-in profiles followed by those in dir, the order
// in which [Select] gives the user's profiles precedence. An empty dir
// means built-in profiles only.
func Load(dir string) ([]*Profile, []Problem, error) {
	profiles, err := Builtin()
	if err != nil {
		return nil, nil, err
	}
	if dir == "" {
		return profiles, nil, nil
	}
	user, problems := LoadDir(dir)
	return append(profiles, user...), problems, nil
}

// WriteBuiltin writes the built-in profiles into dir as files to start
// from, creating the directory if needed. A file that already exists is
// left alone and reported in skipped.
func WriteBuiltin(dir string) (written, skipped []string, err error) {
	names, err := fs.Glob(builtinFiles, "builtin/*.toml")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(names)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		data, err := builtinFiles.ReadFile(name)
		if err != nil {
			return written, skipped, err
		}
		file := filepath.Join(dir, path.Base(name))
		// O_EXCL: never overwrite what the user may have edited.
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			skipped = append(skipped, file)
			continue
		}
		if err != nil {
			return written, skipped, err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return written, skipped, fmt.Errorf("writing %s: %w", file, err)
		}
		written = append(written, file)
	}
	return written, skipped, nil
}

// Watch calls changed with the result of [Load] every time the profile
// files in dir change, until ctx is cancelled. It looks at the directory
// once per interval. A change is passed on when the files have stayed the
// same for one more interval, so that a file is not read while an editor
// is still writing it.
//
// The directory is polled instead of watched through the operating system,
// which keeps this code the same on every platform; reading the names and
// times of a few files once a second costs nothing noticeable.
func Watch(ctx context.Context, dir string, interval time.Duration, changed func([]*Profile, []Problem)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	loaded := fingerprint(dir)
	seen := loaded
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := fingerprint(dir)
		settled := now == seen
		seen = now
		if !settled || now == loaded {
			continue
		}
		loaded = now
		profiles, problems, err := Load(dir)
		if err != nil {
			continue // only a broken build has broken built-in profiles
		}
		changed(profiles, problems)
	}
}

// fingerprint describes the profile files of a directory by name, size and
// modification time. It changes whenever one is added, removed or written.
func fingerprint(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !isProfileFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // removed since the directory was read
		}
		fmt.Fprintf(&b, "%q %d %d\n", entry.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}
