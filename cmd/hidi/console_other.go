//go:build !windows

package main

import "os/exec"

// leaveOwnConsole does nothing: only Windows opens a console window for a
// program started from the desktop.
func leaveOwnConsole() bool { return false }

func hideConsole(*exec.Cmd) {}
