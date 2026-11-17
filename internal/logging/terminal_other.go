//go:build !windows

package logging

import (
	"os"

	"golang.org/x/sys/unix"
)

// enableTerminal reports whether the terminal behind f understands ANSI
// colour codes. Terminals on these systems do.
func enableTerminal(*os.File) bool { return true }

// TerminalRows returns how many lines the terminal behind f has, or 0 if
// f is no terminal or does not tell.
func TerminalRows(f *os.File) int {
	size, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(size.Row)
}
