package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Report says what [Prepare] found and did.
type Report struct {
	// Managed says that the directory is HIDI's own: what is in it is
	// what counts, and the profiles HIDI comes with are not used beside
	// it.
	Managed bool
	// Created says that the directory was made now.
	Created bool
	// Written are the files written because they were missing, and
	// Updated the factory profiles that were there and not as this HIDI
	// has them.
	Written, Updated []string
	// Removed are factory profiles this HIDI no longer comes with, and
	// copies of them an earlier HIDI kept among the user's profiles.
	Removed []string
	// Moved are profiles of the user's that an earlier HIDI kept in the
	// directory of profiles itself, and where they are now.
	Moved [][2]string
	// Unrecognised says, for a directory that is not managed, what in it
	// HIDI does not know.
	Unrecognised string
}

// written is the record of what HIDI wrote: for each file, relative to
// the root, the fingerprint of the content.
type written struct {
	Files map[string]string `json:"files"`
}

// readRecord reads the record of a directory: from the state directory,
// or else from the file an earlier build kept in the directory itself.
// It reports whether there was one. A record that cannot be read as one
// counts as none: it is a note of HIDI's own, and the directory is looked
// at instead.
func readRecord(p Paths) (record written, found bool, err error) {
	for _, file := range []string{p.record(), filepath.Join(p.Root, oldRecord)} {
		data, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return written{}, false, err
		}
		var r written
		if json.Unmarshal(data, &r) == nil && r.Files != nil {
			return r, true, nil
		}
	}
	return written{Files: map[string]string{}}, false, nil
}

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Prepare makes HIDI's directory ready, as the package documentation
// describes. profiles are the profiles HIDI comes with, by file name.
//
// The profiles HIDI comes with are kept in the directory of factory
// profiles exactly as this HIDI has them: written when missing, brought up
// to date when they differ, whoever changed them, and removed when HIDI
// no longer has them. They are there to be read and copied, not changed;
// the user's own profiles are in the directory beside it and are never
// touched.
//
// An error means the directory could not be read or written; HIDI then
// runs with what it comes with.
func Prepare(p Paths, profiles map[string][]byte) (Report, error) {
	var report Report
	entries, err := os.ReadDir(p.Root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		report.Created = true
	case err != nil:
		return report, err
	}

	record, recorded, err := readRecord(p)
	if err != nil {
		return report, err
	}
	if !recorded {
		if what := unknown(p, entries); what != "" {
			// No record, and something is there: not a directory HIDI made.
			return Report{Unrecognised: what}, nil
		}
	}
	report.Managed = true
	// The record is where it belongs after this, and what earlier builds
	// kept in the directory itself is gone.
	_, statErr := os.Stat(p.record())
	inPlace := statErr == nil
	defer func() {
		if _, err := os.Stat(p.record()); err == nil {
			os.Remove(filepath.Join(p.Root, oldRecord))
		}
		os.Remove(filepath.Join(p.Root, oldDevices))
	}()

	for _, dir := range []string{p.Factory, p.User} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return report, err
		}
	}
	changed := false
	// save writes the record of what HIDI wrote. A directory that is
	// HIDI's without one, brought along from another machine for
	// instance, gets one too: without it the directory would be taken
	// for not in use at every start, before it is made ready, and its
	// settings would be passed over.
	save := func() error {
		if !changed && !report.Created && inPlace {
			return nil
		}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(p.State, 0o755); err != nil {
			return err
		}
		return writeFile(p.record(), append(data, '\n'), 0o644)
	}
	fail := func(err error) (Report, error) { return report, errors.Join(err, save()) }

	// The settings: written once, and the user's from then on. Removed,
	// they stay removed.
	if _, err := os.Stat(p.Settings); errors.Is(err, fs.ErrNotExist) {
		if _, known := record.Files["settings.toml"]; !known {
			text := Default().Text()
			if err := writeFile(p.Settings, text, 0o644); err != nil {
				return fail(err)
			}
			record.Files["settings.toml"] = fingerprint(text)
			report.Written = append(report.Written, p.Settings)
			changed = true
		}
	} else if err != nil {
		return fail(err)
	}

	// What an earlier HIDI kept in the directory of profiles itself: its
	// own copies, as far as nobody changed them, go; everything else is
	// the user's and moves to the user's profiles.
	flat, err := os.ReadDir(p.Profiles)
	if err != nil {
		return fail(err)
	}
	for _, entry := range flat {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".toml") || strings.HasPrefix(name, ".") {
			continue
		}
		file, key := filepath.Join(p.Profiles, name), "profiles/"+name
		have, err := os.ReadFile(file)
		if err != nil {
			return fail(err)
		}
		was, known := record.Files[key]
		delete(record.Files, key)
		changed = true
		if known && fingerprint(have) == was {
			if err := os.Remove(file); err != nil {
				return fail(err)
			}
			report.Removed = append(report.Removed, file)
			continue
		}
		to := filepath.Join(p.User, name)
		if _, err := os.Stat(to); err == nil {
			continue // one of that name is there already; this one stays where it is
		}
		if err := os.Rename(file, to); err != nil {
			return fail(err)
		}
		report.Moved = append(report.Moved, [2]string{file, to})
	}
	for key := range record.Files {
		if strings.HasPrefix(key, "profiles/") {
			delete(record.Files, key) // of a copy the user removed long ago
			changed = true
		}
	}

	// The factory profiles, exactly as this HIDI has them.
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := filepath.Join(p.Factory, name)
		have, err := os.ReadFile(file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			report.Written = append(report.Written, file)
		case err != nil:
			return fail(err)
		case string(have) == string(profiles[name]):
			continue
		default:
			report.Updated = append(report.Updated, file)
		}
		if err := writeFile(file, profiles[name], 0o444); err != nil {
			return fail(err)
		}
	}
	old, err := os.ReadDir(p.Factory)
	if err != nil {
		return fail(err)
	}
	for _, entry := range old {
		name := entry.Name()
		if _, shipped := profiles[name]; shipped || entry.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		file := filepath.Join(p.Factory, name)
		os.Chmod(file, 0o644) // Windows removes no file that is marked read-only
		if err := os.Remove(file); err != nil {
			return fail(err)
		}
		report.Removed = append(report.Removed, file)
	}
	return report, save()
}

// unknown names something in a directory without a record that keeps HIDI
// from taking the directory for its own; empty if there is nothing.
// Profiles written for this HIDI and a settings file are fine: that is a
// directory an earlier build of HIDI 2 used.
func unknown(p Paths, entries []os.DirEntry) string {
	for _, entry := range entries {
		switch name := entry.Name(); {
		case strings.HasPrefix(name, ".tmp-") && !entry.IsDir():
			// What a write of HIDI's left behind when HIDI was ended in
			// the middle of it.
		case (name == oldRecord || name == oldDevices) && !entry.IsDir():
			// What earlier builds of HIDI 2 noted here.
		case name == ownState && entry.IsDir():
			// Where a directory that is not the usual one keeps what
			// HIDI notes.
		case name == "settings.toml" && !entry.IsDir():
			if _, err := Load(p.Settings); err != nil {
				return "settings.toml is not a settings file of this HIDI"
			}
		case name == "profiles" && entry.IsDir():
			files, err := os.ReadDir(p.Profiles)
			if err != nil {
				return "profiles cannot be read"
			}
			for _, f := range files {
				if f.IsDir() && (f.Name() == "factory" || f.Name() == "user") {
					continue
				}
				if f.IsDir() {
					return "profiles/" + f.Name() + " is a directory"
				}
				if !strings.HasSuffix(f.Name(), ".toml") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(p.Profiles, f.Name()))
				if err != nil || !strings.Contains(string(data), "schema") {
					return "profiles/" + f.Name() + " is not a profile of HIDI 2"
				}
			}
		default:
			return fmt.Sprintf("%q is nothing HIDI 2 keeps there", name)
		}
	}
	return ""
}

// writeFile writes a file so that it is either there complete or not at
// all, with the given permissions.
func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	// On the disk before it takes the place of the old file, so that a
	// power failure leaves the old file or the new one, never half of one.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows puts nothing in the place of a file that is marked
	// read-only.
	os.Chmod(path, 0o644)
	return os.Rename(tmp.Name(), path)
}

// Managed reports whether the directory is HIDI's, without changing
// anything: one HIDI made ready before, or one that [Prepare] would take
// for HIDI's as it is, a settings file put there by hand or by a tool
// before the first start for instance. Its settings count from the first
// start on.
func Managed(p Paths) bool {
	if _, found, err := readRecord(p); err == nil && found {
		return true
	}
	entries, err := os.ReadDir(p.Root)
	return err == nil && len(entries) > 0 && unknown(p, entries) == ""
}
