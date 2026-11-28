package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SocketPath returns where the control socket of the user's HIDI lives:
// HIDI in the directory the system keeps for the user's running programs,
// $XDG_RUNTIME_DIR on Linux, where there is one, and otherwise in HIDI's
// state directory, %LocalAppData%\HIDI on Windows. Nothing of HIDI's is
// then anywhere else.
func SocketPath() (string, error) {
	if run := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(run) {
		return filepath.Join(run, DirName, socketName), nil
	}
	state, err := DefaultState()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, socketName), nil
}

// CrashDir returns the directory crash reports are kept in: HIDI's state
// directory.
func CrashDir() (string, error) { return DefaultState() }

// The names HIDI gives what it keeps in its state directory.
const (
	recordName = "written.json" // the record of what HIDI wrote
	windowName = "window.json"  // how the window was left
	socketName = "control.sock" // the control socket, and beside it its lock
	crashName  = "crash.log"    // package crashlog: the report of a crash
)

// ofState reports whether an entry of a state directory is one HIDI 2
// puts there.
func ofState(e fs.DirEntry) bool {
	name := e.Name()
	if e.IsDir() {
		return false
	}
	switch {
	case name == recordName, name == windowName, name == socketName, name == socketName+".lock", name == crashName:
		return true
	case strings.HasPrefix(name, "crash-") && strings.HasSuffix(name, ".log"):
		return true // an earlier crash report, kept
	}
	return strings.HasPrefix(name, ".tmp-") // a write HIDI was ended in
}

// ClaimState makes the state directory HIDI's before anything is written
// into it: one that holds something HIDI 2 does not put there, of another
// program with the same name or of an earlier HIDI, is moved aside, as
// [MoveAside] says, and returned is where it went. A directory that is
// not there, or holds HIDI's own files only, is left as it is.
func ClaimState(dir string) (moved string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !ofState(e) {
			return MoveAside(dir)
		}
	}
	return "", nil
}

// MoveAside renames a directory HIDI did not make, so that HIDI can make
// its own and nothing in the other is lost: to the name with "-old"
// behind it, or "-old_1", "-old_2" and so on where that is taken.
func MoveAside(dir string) (string, error) {
	dir = filepath.Clean(dir)
	for n := 0; n < 1000; n++ {
		to := dir + "-old"
		if n > 0 {
			to += "_" + strconv.Itoa(n)
		}
		_, err := os.Lstat(to)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Rename(dir, to); err != nil {
				return "", err
			}
			return to, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name to move %s aside to", dir)
}
