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

// prepareWindow sets up the process of the window. It gets no console.
// The window itself must not be asked to start hidden, which the same
// structure could: the library that opens it would obey. When HIDI ends
// the process is ended outright, since Windows has no interrupt to send
// to one process; the window has nothing to save.
func prepareWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
