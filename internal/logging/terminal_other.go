//go:build !windows

package logging

import "os"

// enableTerminal reports whether the terminal behind f understands ANSI
// colour codes. Terminals on these systems do.
func enableTerminal(*os.File) bool { return true }
