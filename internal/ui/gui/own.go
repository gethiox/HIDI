package gui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/settings"
)

// ErrFactory is what a change to a factory profile is answered with.
var ErrFactory = errors.New("this profile comes with HIDI and is kept as it is; \"Customise\" makes a copy of your own that can be changed")

// Customise gives the user a profile of their own to change: a copy of
// the factory profile file in the directory of the user's profiles, under
// the same name. It returns the copy. A file of that name that is there
// already is the user's and is left as it is; made says whether the copy
// was made now.
func Customise(factoryFile, userDir string) (file string, made bool, err error) {
	file = filepath.Join(userDir, filepath.Base(factoryFile))
	if _, err := os.Stat(file); err == nil {
		return file, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	text, err := os.ReadFile(factoryFile)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return "", false, err
	}
	// Not with the mode of the factory file, which may not be written.
	if err := os.WriteFile(file, text, 0o644); err != nil {
		os.Remove(file)
		return "", false, err
	}
	return file, true, nil
}

// SetProfileEnabled writes into a profile file of the user's whether the
// profile is to be used.
func SetProfileEnabled(file string, on bool) error {
	text, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	changed, err := config.SetEnabled(text, on)
	if err != nil {
		return err
	}
	if _, err := config.Parse(changed); err != nil {
		return err
	}
	return writeWhole(file, changed)
}

// IsFactory reports whether a profile file is one of those HIDI comes
// with, by the directory it is in.
func IsFactory(file string, p settings.Paths) bool {
	return p.Factory != "" && filepath.Dir(file) == filepath.Clean(p.Factory)
}

// pending holds settings that are being changed on the settings sheet:
// as the file has them, and as the sheet shows them. Nothing is written
// until the user saves.
type pending struct {
	disk, shown settings.Settings
}

// load takes in the settings as the file has them now. What the user
// changed and did not save stays as it is shown; everything else follows
// the file. What the window keeps of its own accord, the instruments
// whose input and notes are drawn, always follows the file.
func (p *pending) load(disk settings.Settings) {
	if !p.Dirty() {
		p.shown = clone(disk)
	}
	p.disk = clone(disk)
	p.shown.Shown = clone(disk).Shown
}

// Dirty reports whether something is shown that is not saved.
func (p *pending) Dirty() bool { return !reflect.DeepEqual(normal(p.shown), normal(p.disk)) }

// drop forgets what is not saved.
func (p *pending) drop() { p.shown = clone(p.disk) }

// restart reports whether what is to be saved includes something a
// running HIDI only follows when it is started again.
func (p *pending) restart() bool {
	return p.shown.Input.StartOn != p.disk.Input.StartOn ||
		p.shown.Window.ShowAtStart != p.disk.Window.ShowAtStart ||
		!slices.Equal(p.shown.Output.Connect, p.disk.Output.Connect)
}

// clone copies settings so that the copy shares no list with the
// original.
func clone(s settings.Settings) settings.Settings {
	s.Input.Toggle = slices.Clone(s.Input.Toggle)
	s.Devices.List = slices.Clone(s.Devices.List)
	s.Output.Connect = slices.Clone(s.Output.Connect)
	s.Shown.Input = slices.Clone(s.Shown.Input)
	s.Shown.Output = slices.Clone(s.Shown.Output)
	return s
}

// normal makes settings comparable: a list without entries is the same
// whether it is there or not.
func normal(s settings.Settings) settings.Settings {
	s = clone(s)
	for _, list := range []*[]string{&s.Devices.List, &s.Output.Connect, &s.Shown.Input, &s.Shown.Output} {
		if len(*list) == 0 {
			*list = nil
		}
	}
	if len(s.Input.Toggle) == 0 {
		s.Input.Toggle = nil
	}
	return s
}

// deviceRow is a line of the list of devices on the settings sheet.
type deviceRow struct {
	Name, About string
	// There says that the device is connected.
	There bool
}

// seenDevice is a device HIDI knows of, as the list needs it.
type seenDevice struct {
	Name, Type string
	Connected  bool
}

// deviceRows puts the list of devices in order: those that are connected
// first, then those HIDI has met and that are away, then those only the
// settings name. Within each the order they came in is kept, and a name
// is listed once.
func deviceRows(seen []seenDevice, named []string) []deviceRow {
	var there, away, only []deviceRow
	listed := map[string]bool{}
	// A name may be seen both connected and not: connected counts.
	for _, connected := range []bool{true, false} {
		for _, d := range seen {
			if d.Connected != connected || listed[d.Name] {
				continue
			}
			listed[d.Name] = true
			if connected {
				there = append(there, deviceRow{d.Name, d.Type, true})
			} else {
				away = append(away, deviceRow{d.Name, strings.TrimPrefix(d.Type+", not connected", ", "), false})
			}
		}
	}
	for _, name := range named {
		if !listed[name] {
			listed[name] = true
			only = append(only, deviceRow{name, "not connected", false})
		}
	}
	return append(append(there, away...), only...)
}

// profileLine says of a profile what the list of profiles says after its
// name.
func profileLine(p *config.Profile) string {
	return fmt.Sprintf("%s · for %s", filepath.Base(p.Source), p.Match.String())
}
