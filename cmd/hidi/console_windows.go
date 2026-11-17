package main

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

// leaveOwnConsole closes the console window Windows opens for a program
// that is started by a double click. A console HIDI shares with a terminal
// it was started from is kept, and so is the log written there. It reports
// whether the console was left.
func leaveOwnConsole() bool {
	var list [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(len(list)))
	if n != 1 {
		return false // no console, or one that others use too
	}
	r, _, _ := procFreeConsole.Call()
	return r != 0
}

// hideConsole keeps Windows from opening a console window for a program
// HIDI starts.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
