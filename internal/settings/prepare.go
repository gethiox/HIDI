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
	// Updated those brought up to date because the user had not changed
	// them.
	Written, Updated []string
	// Unrecognised says, for a directory that is not managed, what in it
	// HIDI does not know.
	Unrecognised string
}

// written is the record of what HIDI wrote: for each file, relative to
// the root, the fingerprint of the content.
type written struct {
	Note  string            `json:"note"`
	Files map[string]string `json:"files"`
}

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Prepare makes HIDI's directory ready, as the package documentation
// describes. profiles are the profiles HIDI comes with, by file name.
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

	record := written{Files: map[string]string{}}
	recorded := false
	if data, err := os.ReadFile(p.record); err == nil {
		recorded = true
		if err := json.Unmarshal(data, &record); err != nil || record.Files == nil {
			return Report{Unrecognised: filepath.Base(p.record) + " is not a record HIDI wrote"}, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return report, err
	} else if what := unknown(p, entries); what != "" {
		// No record, and something is there: not a directory HIDI made.
		return Report{Unrecognised: what}, nil
	}
	report.Managed = true

	// The files HIDI wants there: the profiles, and the settings.
	want := map[string][]byte{"settings.toml": Default().Text()}
	for name, data := range profiles {
		want[filepath.ToSlash(filepath.Join("profiles", name))] = data
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)

	if err := os.MkdirAll(p.Profiles, 0o755); err != nil {
		return report, err
	}
	changed := false
	// save writes the record of what HIDI wrote. A directory that is
	// HIDI's without one, copied without its hidden file for instance,
	// gets one too: without it the directory would be taken for not in
	// use at every start, before it is made ready, and its settings
	// would be passed over.
	save := func() error {
		if !changed && !report.Created && recorded {
			return nil
		}
		record.Note = "HIDI notes here which files it wrote, to tell later whether you changed them. Do not edit."
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		return writeFile(p.record, append(data, '\n'))
	}
	for _, name := range names {
		file := filepath.Join(p.Root, filepath.FromSlash(name))
		now := fingerprint(want[name])
		was, known := record.Files[name]
		have, err := os.ReadFile(file)
		switch {
		case errors.Is(err, fs.ErrNotExist) && known:
			continue // the user removed it, and means it
		case errors.Is(err, fs.ErrNotExist):
			report.Written = append(report.Written, file)
		case err != nil:
			return report, errors.Join(err, save())
		case !known:
			continue // the user's own file of that name
		case fingerprint(have) != was || was == now:
			continue // changed by the user, or up to date
		case name == "settings.toml":
			continue // the settings are the user's from the first day
		default:
			report.Updated = append(report.Updated, file)
		}
		if err := writeFile(file, want[name]); err != nil {
			return report, errors.Join(err, save())
		}
		record.Files[name] = now
		changed = true
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
// all.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
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
	return os.Rename(tmp.Name(), path)
}

// Managed reports whether the directory is HIDI's, without changing
// anything: one HIDI made ready before, or one that [Prepare] would take
// for HIDI's as it is, a settings file put there by hand or by a tool
// before the first start for instance. Its settings count from the first
// start on.
func Managed(p Paths) bool {
	if _, err := os.Stat(p.record); err == nil {
		return true
	}
	entries, err := os.ReadDir(p.Root)
	return err == nil && len(entries) > 0 && unknown(p, entries) == ""
}
