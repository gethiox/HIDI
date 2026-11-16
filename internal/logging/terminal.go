package logging

import "os"

// TerminalColour reports whether lines written to f should be coloured, and
// prepares the terminal for it where that is needed.
//
// The NO_COLOR and FORCE_COLOR environment variables decide when set to
// anything but an empty string. Otherwise a terminal gets colours and a file
// or a pipe does not.
func TerminalColour(f *os.File) bool {
	return colourChoice(f, os.Getenv)
}

func colourChoice(f *os.File, getenv func(string) string) bool {
	switch {
	case getenv("NO_COLOR") != "":
		return false
	case getenv("FORCE_COLOR") != "":
		enableTerminal(f) // best effort: the user insists
		return true
	}
	return isTerminal(f)
}

// IsTerminal reports whether f is a terminal that understands ANSI codes,
// which a [Footer] needs, and prepares it for them where that is needed.
// Unlike [TerminalColour] it does not ask the variables about colour: a
// footer without colours is still a footer.
func IsTerminal(f *os.File) bool {
	return os.Getenv("TERM") != "dumb" && isTerminal(f)
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return enableTerminal(f)
}
