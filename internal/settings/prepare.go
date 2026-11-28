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
	// Removed are factory profiles this HIDI no longer comes with.
	Removed []string
	// MovedAside are HIDI's usual directories that held something HIDI
	// did not make there, each with where it was moved and why; HIDI
	// made them anew.
	MovedAside []Aside
	// Unrecognised says, for a directory named by hand that is not
	// managed, what in it HIDI does not know. Such a directory is left
	// as it is.
	Unrecognised string
}

// Aside is a directory moved out of HIDI's way: From is its name as HIDI
// has it, To where it went, and Why what in it HIDI did not make.
type Aside struct{ From, To, Why string }

// written is the record of what HIDI wrote: for each file, relative to
// the root, the fingerprint of the content.
type written struct {
	Files map[string]string `json:"files"`
}

// readRecord reads the record of a directory from the state directory. It
// reports whether there was one. A record that cannot be read as one
// counts as none: it is a note of HIDI's own, and the directory is looked
// at instead.
func readRecord(p Paths) (record written, found bool, err error) {
	none := written{Files: map[string]string{}}
	data, err := os.ReadFile(p.record())
	if errors.Is(err, fs.ErrNotExist) {
		return none, false, nil
	}
	if err != nil {
		return none, false, err
	}
	var r written
	if json.Unmarshal(data, &r) == nil && r.Files != nil {
		return r, true, nil
	}
	return none, false, nil
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
// HIDI's usual directories are claimed first: one that holds something
// HIDI does not make there, of another program by the same name or of
// HIDI 1, is moved aside with [MoveAside], and HIDI begins anew. Nothing
// of it is removed. A directory named by hand is left as it is instead,
// and reported as not managed.
//
// An error means the directory could not be read or written; HIDI then
// runs with what it comes with.
func Prepare(p Paths, profiles map[string][]byte) (Report, error) {
	var report Report
	if p.usual {
		to, err := ClaimState(p.State)
		if err != nil {
			return report, err
		}
		if to != "" {
			report.MovedAside = append(report.MovedAside, Aside{From: p.State, To: to, Why: "it holds files HIDI 2 does not keep there"})
		}
	}
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
			if !p.usual {
				return Report{Unrecognised: what}, nil
			}
			to, err := MoveAside(p.Root)
			if err != nil {
				return report, fmt.Errorf("%s holds what HIDI did not make (%s), and cannot be moved aside: %w", p.Root, what, err)
			}
			report.MovedAside = append(report.MovedAside, Aside{From: p.Root, To: to, Why: what})
			report.Created = true
		}
	}
	report.Managed = true
	_, statErr := os.Stat(p.record())
	inPlace := statErr == nil

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
// from taking the directory for its own; empty if there is nothing. What
// HIDI 2 makes there is fine, in whatever state the user left it: the
// settings file, and the profiles in their two directories. A directory
// like that is HIDI's whose record went missing, or was filled by hand or
// by a tool before the first start.
func unknown(p Paths, entries []os.DirEntry) string {
	for _, entry := range entries {
		switch name := entry.Name(); {
		case strings.HasPrefix(name, ".tmp-") && !entry.IsDir():
			// What a write of HIDI's left behind when HIDI was ended in
			// the middle of it.
		case name == ownState && entry.IsDir() && !p.usual:
			// Where a directory named by hand keeps what HIDI notes.
		case name == "settings.toml" && !entry.IsDir():
		case name == "profiles" && entry.IsDir():
			files, err := os.ReadDir(p.Profiles)
			if err != nil {
				return "profiles cannot be read"
			}
			for _, f := range files {
				switch {
				case f.IsDir() && (f.Name() == "factory" || f.Name() == "user"):
				case strings.HasPrefix(f.Name(), ".tmp-") && !f.IsDir():
				default:
					return fmt.Sprintf("%q is nothing HIDI 2 keeps in profiles", f.Name())
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
