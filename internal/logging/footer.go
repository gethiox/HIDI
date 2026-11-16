package logging

import (
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Footer keeps a few lines at the bottom of a terminal while everything
// else written through it scrolls past above them. A [Log] writes its
// lines to a Footer, and [Footer.Set] says what the lines at the bottom
// are; so a running program can show its state below its log.
//
// It needs a terminal that understands ANSI codes; see [TerminalColour].
// Its methods are safe for concurrent use.
type Footer struct {
	mu    sync.Mutex
	out   io.Writer
	lines []string
	// shown is how many lines of the footer are on the terminal now. The
	// cursor stands at the end of the last of them.
	shown int
	buf   []byte
}

// NewFooter returns a footer with no lines that writes to out.
func NewFooter(out io.Writer) *Footer { return &Footer{out: out} }

// Write writes p above the footer. What is written should end with the
// end of a line, as the lines of a [Log] do.
func (f *Footer) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buf = f.erase(f.buf[:0])
	f.buf = append(f.buf, p...)
	f.buf = f.draw(f.buf)
	if _, err := f.out.Write(f.buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Set replaces the lines at the bottom. A line may carry ANSI colour
// codes; one that is longer than the terminal is wide is cut off there.
func (f *Footer) Set(lines []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Equal(lines, f.lines) {
		return
	}
	f.buf = f.erase(f.buf[:0])
	f.lines = slices.Clone(lines)
	f.buf = f.draw(f.buf)
	f.out.Write(f.buf)
}

// Close takes the footer off the terminal, leaving what was written above
// it. Writing through the footer still works afterwards.
func (f *Footer) Close() {
	f.Set(nil)
}

// erase appends what moves the cursor to the start of the footer and
// clears everything from there on.
func (f *Footer) erase(b []byte) []byte {
	if f.shown == 0 {
		return b
	}
	b = append(b, '\r')
	if f.shown > 1 {
		b = append(b, "\x1b["...)
		b = strconv.AppendInt(b, int64(f.shown-1), 10)
		b = append(b, 'A')
	}
	f.shown = 0
	return append(b, "\x1b[J"...)
}

// draw appends the footer. While it is drawn the terminal is told not to
// continue a long line on the next one, which would make the footer
// taller than its number of lines.
func (f *Footer) draw(b []byte) []byte {
	if len(f.lines) == 0 {
		return b
	}
	b = append(b, "\x1b[?7l"...)
	for i, line := range f.lines {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, strings.ReplaceAll(line, "\n", " ")...)
		b = append(b, "\x1b[0m"...)
	}
	f.shown = len(f.lines)
	return append(b, "\x1b[?7h"...)
}
