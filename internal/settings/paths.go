package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DirName is the name of HIDI's directories, in the system's place for
// configuration and in its place for what programs note for themselves.
const DirName = "HIDI"

// Paths says where HIDI's files are.
type Paths struct {
	// Root is HIDI's directory: what the user sets and may edit.
	Root string
	// Settings is the file with HIDI's own settings.
	Settings string
	// Profiles is the directory of the profiles. In it are Factory, the
	// profiles HIDI comes with, which HIDI keeps as it has them, and
	// User, the user's own, which come first where both fit a device.
	Profiles      string
	Factory, User string
	// State is the directory for what HIDI notes for itself and nobody
	// edits: which files it wrote, and how the window was left. It is
	// kept apart from Root, which on Windows follows the user from one
	// machine to the next, while this stays on the machine. Empty means
	// that nothing of the kind is kept.
	State string
}

// record is the file in which HIDI notes what it wrote into Root.
func (p Paths) record() string { return filepath.Join(p.State, "written.json") }

// Window is the file in which the window notes how it was left; empty
// where nothing is kept.
func (p Paths) Window() string {
	if p.State == "" {
		return ""
	}
	return filepath.Join(p.State, "window.json")
}

// The files earlier builds of HIDI 2 kept in Root itself: the record of
// what was written, and a list of the devices HIDI had met, which is no
// longer kept at all.
const (
	oldRecord  = ".written.json"
	oldDevices = ".devices.json"
)

// ownState is the name of the state directory inside a Root that is not
// the user's usual one.
const ownState = ".state"

// DefaultRoot returns HIDI's directory for the user: HIDI in the user's
// configuration directory.
func DefaultRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the configuration directory: %w", err)
	}
	return filepath.Join(base, DirName), nil
}

// DefaultState returns the directory for what HIDI notes for itself: on
// Windows HIDI in the local application data, which stays on the machine,
// elsewhere HIDI in $XDG_STATE_HOME, which is ~/.local/state unless set.
func DefaultState() (string, error) {
	if runtime.GOOS == "windows" {
		base, err := os.UserCacheDir() // %LocalAppData%
		if err != nil {
			return "", fmt.Errorf("finding the local application data: %w", err)
		}
		return filepath.Join(base, DirName), nil
	}
	if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, DirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", DirName), nil
}

// PathsIn returns where the files are for a root directory. The user's
// usual directory has its state in the system's place for that. Any other
// directory, named with --config, keeps its state inside itself, so that
// it is complete by itself and two of them do not share what they note.
func PathsIn(root string) Paths {
	state := filepath.Join(root, ownState)
	if usual, err := DefaultRoot(); err == nil && filepath.Clean(root) == usual {
		if dir, err := DefaultState(); err == nil {
			state = dir
		}
	}
	return Paths{
		Root:     root,
		Settings: filepath.Join(root, "settings.toml"),
		Profiles: filepath.Join(root, "profiles"),
		Factory:  filepath.Join(root, "profiles", "factory"),
		User:     filepath.Join(root, "profiles", "user"),
		State:    state,
	}
}
