package settings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SocketPath returns where the control socket of the user's HIDI lives:
// HIDI in the directory the system keeps for the user's running programs,
// $XDG_RUNTIME_DIR on Linux, where there is one, and otherwise in HIDI's
// state directory, %LocalAppData%\HIDI on Windows. Nothing of HIDI's is
// then anywhere else.
func SocketPath() (string, error) {
	if run := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(run) {
		return filepath.Join(run, DirName, "control.sock"), nil
	}
	state, err := DefaultState()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "control.sock"), nil
}

// CrashDir returns the directory crash reports are kept in: HIDI's state
// directory.
func CrashDir() (string, error) { return DefaultState() }

// OwnName gives a directory of HIDI's the case of its name where the
// system tells no cases apart and it is there in another: earlier builds
// made %LocalAppData%\hidi, and Windows would go on calling the one
// directory so, whatever name it is asked for. Where cases are told
// apart, as on Linux, the two are two directories, and nothing is done.
// A directory that cannot be renamed is left as it is: its name is a
// matter of looks.
func OwnName(dir string) error {
	dir = filepath.Clean(dir)
	parent, name := filepath.Split(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == name || !strings.EqualFold(e.Name(), name) {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			return nil // another directory, on a system that tells cases apart
		}
		// In two steps: renaming to the same name in another case is
		// not taken for a change everywhere.
		old := filepath.Join(parent, e.Name())
		step := old + ".renaming"
		if err := os.Rename(old, step); err != nil {
			return err
		}
		return os.Rename(step, dir)
	}
	return nil
}
