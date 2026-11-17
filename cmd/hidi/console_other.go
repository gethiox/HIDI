//go:build !windows

package main

import (
	"os"
	"os/exec"
)

// leaveOwnConsole does nothing: only Windows opens a console window for a
// program started from the desktop.
func leaveOwnConsole() bool { return false }

// prepareWindow sets up the process of the window: when HIDI ends, it is
// asked to close the way a user would, so that it closes properly.
func prepareWindow(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
}
