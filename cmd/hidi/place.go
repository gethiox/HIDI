package main

import (
	"fmt"
	"github.com/spf13/pflag"
	"io"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/settings"
)

// place is where this HIDI keeps its files, and what it found there.
type place struct {
	// paths is empty when there is no directory to use.
	paths settings.Paths
	// managed says that the directory is HIDI's own, so that the profiles
	// in it are all there are.
	managed bool
	// profiles is the directory the profiles are read from; empty for
	// the built-in ones only.
	profiles string
	settings settings.Settings
	// settingsFile is the file the settings were read from; empty when
	// they are the defaults for lack of a directory.
	settingsFile string
	// notes are things to tell the user, and problems things that are
	// wrong, in the order found.
	notes, problems []string
}

// placeFlags adds the options that say where HIDI's files are.
func placeFlags(fs *pflag.FlagSet) (root, profiles *string) {
	dir, err := settings.DefaultRoot()
	if err != nil {
		dir = "" // no home directory: what HIDI comes with only
	}
	root = fs.String("config", dir, "HIDI's `directory`, with its settings and profiles; empty uses what HIDI comes with only")
	profiles = fs.String("profiles", "", "read profiles from this `directory` instead, beside the built-in ones")
	return root, profiles
}

// findPlace reads HIDI's directory. With prepare it also makes the
// directory ready: fills it on the first start and brings unchanged
// built-in profiles up to date.
func findPlace(root, profiles string, prepare bool) place {
	pl := place{settings: settings.Default(), profiles: profiles}
	if root == "" {
		return pl
	}
	pl.paths = settings.PathsIn(root)
	if prepare {
		files, err := config.BuiltinFiles()
		if err != nil {
			pl.problems = append(pl.problems, fmt.Sprintf("the built-in profiles cannot be read: %v", err))
			return pl
		}
		report, err := settings.Prepare(pl.paths, files)
		switch {
		case err != nil:
			pl.problems = append(pl.problems, fmt.Sprintf("%s cannot be made ready; using what HIDI comes with: %v", root, err))
		case report.Unrecognised != "":
			pl.problems = append(pl.problems, fmt.Sprintf("%s was not made by HIDI 2 (%s) and is left alone; using what HIDI comes with. Move it away, or name another directory with --config", root, report.Unrecognised))
		case report.Created:
			pl.notes = append(pl.notes, fmt.Sprintf("first start: settings and profiles written to %s", root))
		}
		for _, file := range report.Updated {
			pl.notes = append(pl.notes, "factory profile brought up to date with this HIDI: "+file)
		}
		for _, file := range report.Removed {
			pl.notes = append(pl.notes, "removed, HIDI keeps the factory profiles elsewhere now or no longer has this one: "+file)
		}
		for _, move := range report.Moved {
			pl.notes = append(pl.notes, fmt.Sprintf("your profile %s is now kept in %s", move[0], move[1]))
		}
		if !report.Created {
			for _, file := range report.Written {
				pl.notes = append(pl.notes, "new factory file written: "+file)
			}
		}
		pl.managed = report.Managed
	} else {
		pl.managed = settings.Managed(pl.paths)
	}
	if !pl.managed {
		return pl
	}
	s, err := settings.Load(pl.paths.Settings)
	if err != nil {
		pl.problems = append(pl.problems, fmt.Sprintf("%s has mistakes; using the default settings:\n%v", pl.paths.Settings, err))
	}
	pl.settings = s
	pl.settingsFile = pl.paths.Settings
	if pl.profiles == "" {
		pl.profiles = pl.paths.Profiles
	} else {
		pl.managed = false // profiles from elsewhere go beside the built-in ones
	}
	return pl
}

// load reads the profiles: those of HIDI's own directory alone, or the
// built-in ones with those of the directory named beside them.
func (pl place) load() ([]*config.Profile, []config.Problem, error) {
	if pl.managed {
		// The factory profiles and the user's, those of the user's that
		// are turned off left out.
		profiles, problems := config.LoadKinds(pl.paths.Factory, pl.paths.User)
		return config.Enabled(profiles), problems, nil
	}
	return config.Load(pl.profiles)
}

// tell writes the notes and problems for a command that has no log.
func (pl place) tell(w io.Writer, command string) {
	for _, problem := range pl.problems {
		fmt.Fprintf(w, "hidi %s: %s\n", command, problem)
	}
}

// cmdConfig shows where HIDI keeps its files.
func cmdConfig(args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("config", pflag.ContinueOnError)
	root, profiles := placeFlags(fs)
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}
	pl := findPlace(*root, *profiles, false)
	if pl.paths.Root == "" {
		fmt.Fprintln(stdout, "HIDI has no directory here and uses what it comes with.")
		return 0
	}
	fmt.Fprintf(stdout, "HIDI's directory  %s\n", pl.paths.Root)
	fmt.Fprintf(stdout, "Settings          %s\n", pl.paths.Settings)
	fmt.Fprintf(stdout, "Profiles          %s\n", pl.paths.Profiles)
	switch {
	case pl.managed:
		fmt.Fprintln(stdout, "\nThe directory is in use: what is in it is what HIDI does.")
	case settings.Managed(pl.paths):
		fmt.Fprintf(stdout, "\nProfiles are read from %s instead, beside the built-in ones.\n", pl.profiles)
	default:
		fmt.Fprintln(stdout, "\nThe directory is not in use yet. HIDI fills it on its first start,\nunless something is there that HIDI did not make.")
	}
	pl.tell(stderr, "config")
	if len(pl.problems) > 0 {
		return 1
	}
	return 0
}
