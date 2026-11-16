package logging

import (
	"strconv"
	"strings"
)

// Colour is the colour of a piece of a line: the terminal's default (the
// zero value) or one of the 16 basic colours.
type Colour struct {
	// Code is the basic colour's ANSI foreground code, 30 to 37 or 90 to 97,
	// or 0 for the default colour.
	Code int
}

// Span is a piece of a line in one colour.
type Span struct {
	Text string
	Fg   Colour
}

// Spans splits a line at its ANSI colour codes, for an interface that draws
// the colours itself.
func Spans(s string) []Span {
	var spans []Span
	var fg Colour
	scan(s, func(text string) {
		spans = append(spans, Span{Text: text, Fg: fg})
	}, func(params string) {
		for _, p := range strings.Split(params, ";") {
			n, err := strconv.Atoi(p)
			switch {
			case p == "" || err == nil && (n == 0 || n == 39):
				fg = Colour{}
			case err == nil && (n >= 30 && n <= 37 || n >= 90 && n <= 97):
				fg = Colour{Code: n}
			}
		}
	})
	return spans
}

// Strip returns a line without its ANSI colour codes.
func Strip(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	scan(s, func(text string) { b.WriteString(text) }, func(string) {})
	return b.String()
}

// scan walks a line, calling text for each piece between colour codes and
// code with the parameters of each code ("1;32" for "\x1b[1;32m").
func scan(s string, text, code func(string)) {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			break
		}
		if i > 0 {
			text(s[:i])
		}
		s = s[i+2:]
		j := strings.IndexByte(s, 'm')
		if j < 0 {
			continue // an unfinished code: drop its introducer
		}
		code(s[:j])
		s = s[j+1:]
	}
	if s != "" {
		text(s)
	}
}
