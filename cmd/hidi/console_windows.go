package main

import (
	"os"
	"os/exec"
	"slices"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
	procAttachConsole         = kernel32.NewProc("AttachConsole")
	procAllocConsole          = kernel32.NewProc("AllocConsole")
)

// attachParentProcess is ATTACH_PARENT_PROCESS.
const attachParentProcess = ^uintptr(0)

// consoleFlag asks for a console window of HIDI's own.
const consoleFlag = "--console"

// findConsole gives HIDI somewhere to write on Windows, and returns the
// command line without the option that is meant for it.
//
// A released hidi.exe is built as a program without a console, so that a
// double click opens no black window. Such a program starts with nowhere
// to write. If it was started from a command line, it joins the console
// of that command line, and what it writes appears there. With --console
// it opens a console window of its own, which also suits a shortcut.
// Output that was sent to a file or to another program is left alone.
//
// One thing a program without a console cannot have: the command line
// does not wait for it. The prompt comes back at once, and HIDI writes
// below it. "start /wait hidi ..." makes cmd wait.
func findConsole(args []string) []string {
	own := slices.Contains(args, consoleFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == consoleFlag })
	var list [2]uint32
	if n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(len(list))); n != 0 {
		return args // built with a console, or given one by whoever started HIDI
	}
	attached, _, _ := procAttachConsole.Call(attachParentProcess)
	if attached == 0 && own {
		attached, _, _ = procAllocConsole.Call()
	}
	if attached == 0 {
		return args
	}
	// Where the program has nothing to write to or read from, it gets the
	// console; what was given to it otherwise stays.
	for _, std := range []struct {
		which  uint32
		name   string
		target **os.File
	}{
		{windows.STD_INPUT_HANDLE, "CONIN$", &os.Stdin},
		{windows.STD_OUTPUT_HANDLE, "CONOUT$", &os.Stdout},
		{windows.STD_ERROR_HANDLE, "CONOUT$", &os.Stderr},
	} {
		if h, err := windows.GetStdHandle(std.which); err == nil && h != 0 && h != windows.InvalidHandle {
			if kind, err := windows.GetFileType(h); err == nil && kind != windows.FILE_TYPE_UNKNOWN {
				continue
			}
		}
		name, _ := windows.UTF16PtrFromString(std.name)
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			continue
		}
		windows.SetStdHandle(std.which, h)
		*std.target = os.NewFile(uintptr(h), std.name)
	}
	return args
}

// leaveOwnConsole closes the console window Windows opens for a program
// with a console that is started by a double click, as a hidi.exe built
// without "-H windowsgui" is. A console HIDI shares with a terminal it was
// started from is kept, and so is the log written there. It reports
// whether the console was left.
func leaveOwnConsole() bool {
	if slices.Contains(os.Args, consoleFlag) {
		return false // asked for
	}
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
