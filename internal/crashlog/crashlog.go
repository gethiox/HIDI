// Package crashlog keeps what the Go runtime prints when a program dies:
// the reason and the stack of every goroutine. That output goes to the
// terminal, which a user may not be looking at or may have closed. With
// this package it also goes to a file, and the next start of the program
// can say that the last one crashed and where the details are.
//
// A crash cannot be handled in the dying program itself beyond this: a
// panic in another goroutine, or a fault inside a call into the operating
// system, ends the process without running any of its code.
package crashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// current is the name of the file the running program writes a crash to.
const current = "crash.log"

// Dir returns the directory crash reports are kept in: hidi in the user's
// cache directory.
func Dir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("finding a place for crash reports: %w", err)
	}
	return filepath.Join(cache, "hidi"), nil
}

// Start arranges for a crash of this program to be written to a file in
// dir. If the previous run left a crash there, it is moved to a file named
// after the time it was found, and that file's path is returned.
//
// Only one program should use a directory at a time; a second one would
// take over the file.
func Start(dir string) (previous string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the directory for crash reports: %w", err)
	}
	path := filepath.Join(dir, current)
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
		kept := filepath.Join(dir, "crash-"+info.ModTime().Format("20060102-150405")+".log")
		// Windows does not rename a file that is open, which this one is
		// if the program holding it is still around; copying works there.
		if os.Rename(path, kept) == nil {
			previous = kept
		} else if data, readErr := os.ReadFile(path); readErr == nil && os.WriteFile(kept, data, 0o600) == nil {
			previous = kept
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return previous, fmt.Errorf("creating the crash report file: %w", err)
	}
	// The runtime keeps its own handle on the file.
	defer f.Close()
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return previous, fmt.Errorf("directing crashes to %s: %w", path, err)
	}
	return previous, nil
}

// Stop ends the recording of crashes and lets go of the file.
func Stop() { debug.SetCrashOutput(nil, debug.CrashOptions{}) }

// Path returns the file a crash of the running program is written to.
func Path(dir string) string { return filepath.Join(dir, current) }

// Report records a panic that was recovered, which the runtime therefore
// does not write itself, and returns what to tell the user: what happened,
// in one line, and where the details are.
func Report(recovered any, dir string) string {
	if f, err := os.OpenFile(Path(dir), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600); err == nil {
		fmt.Fprintf(f, "panic: %v\n\n%s\n", recovered, debug.Stack())
		f.Close()
	}
	return fmt.Sprintf("HIDI stopped because of a bug in it: %v\nThe details were written to %s at %s. Please include that file when you report this.",
		recovered, Path(dir), time.Now().Format("15:04:05"))
}
