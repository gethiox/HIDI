package logging

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableTerminal switches on the console's handling of ANSI codes, which is
// off for a new process, and reports whether the console has it.
func enableTerminal(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false // not a console
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

// TerminalRows returns how many lines the console window behind f has, or
// 0 if f is no console.
func TerminalRows(f *os.File) int {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 0
	}
	return int(info.Window.Bottom-info.Window.Top) + 1
}
