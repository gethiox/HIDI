package logging

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ANSI colour codes used in a line.
const (
	reset         = "\x1b[0m"
	red           = "\x1b[31m"
	green         = "\x1b[32m"
	yellow        = "\x1b[33m"
	blue          = "\x1b[34m"
	magenta       = "\x1b[35m"
	cyan          = "\x1b[36m"
	gray          = "\x1b[90m"
	brightRed     = "\x1b[91m"
	brightGreen   = "\x1b[92m"
	brightYellow  = "\x1b[93m"
	brightBlue    = "\x1b[94m"
	brightMagenta = "\x1b[95m"
	brightCyan    = "\x1b[96m"
)

// componentColours are the colours a component's name can get. Red is left
// out: it marks errors.
var componentColours = []string{
	green, yellow, blue, magenta, cyan,
	brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan,
}

// format builds one line, with colours.
func format(t time.Time, level slog.Level, component, message string, attrs []slog.Attr) string {
	var b strings.Builder
	head(&b, t, level, component, message)
	for _, a := range attrs {
		writeAttr(&b, a)
	}
	return b.String()
}

// brief formats a line up to its message and no further: the time, the
// level, the component and the message, in colour, without what the
// caller gave beside them. It is the line for where there is room for one
// sentence, the bar at the bottom of the window.
func brief(t time.Time, level slog.Level, component, message string) string {
	var b strings.Builder
	head(&b, t, level, component, message)
	return b.String()
}

// head writes what every line begins with: the time, the level, the
// component and the message.
func head(b *strings.Builder, t time.Time, level slog.Level, component, message string) {
	if !t.IsZero() {
		b.WriteString(cyan + t.Format("2006-01-02") + " " + green + t.Format("15:04:05") + gray + t.Format(".000") + reset + " ")
	}
	b.WriteString(levelColour(level) + fmt.Sprintf("%5s", level.String()) + reset + " ")
	if component != "" {
		b.WriteString(componentColour(component) + "[")
		writeEscaped(b, component)
		b.WriteString("]" + reset + " ")
	}
	writeEscaped(b, message)
}

func levelColour(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return gray
	case l < slog.LevelWarn:
		return green
	case l < slog.LevelError:
		return yellow
	}
	return brightRed
}

// componentColour picks a component's colour from its name, the same one
// every time.
func componentColour(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return componentColours[h.Sum32()%uint32(len(componentColours))]
}

// writeAttr writes " key=value". A group is written as one value for each of
// its members, with dotted keys; an empty attribute is left out.
func writeAttr(b *strings.Builder, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, member := range v.Group() {
			if a.Key != "" {
				member.Key = a.Key + "." + member.Key
			}
			writeAttr(b, member)
		}
		return
	}
	if a.Equal(slog.Attr{}) {
		return
	}
	b.WriteString(" " + magenta)
	writeEscaped(b, a.Key)
	b.WriteString(reset + "=")
	switch v.Kind() {
	case slog.KindString:
		b.WriteString(green)
		writeQuoted(b, v.String())
	case slog.KindInt64, slog.KindUint64, slog.KindBool:
		b.WriteString(blue + v.String())
	case slog.KindFloat64:
		b.WriteString(blue + strconv.FormatFloat(v.Float64(), 'g', 4, 64))
	case slog.KindDuration:
		b.WriteString(cyan + v.Duration().Round(time.Microsecond).String())
	case slog.KindTime:
		b.WriteString(cyan + v.Time().Format(time.RFC3339))
	default:
		switch x := v.Any().(type) {
		case nil:
			b.WriteString(gray + "none")
		case error:
			b.WriteString(red)
			writeQuoted(b, safeText(x.Error))
		case fmt.Stringer:
			b.WriteString(green)
			writeQuoted(b, safeText(x.String))
		default:
			b.WriteString(gray)
			writeEscaped(b, fmt.Sprint(x))
		}
	}
	b.WriteString(reset)
}

// safeText calls the Error or String method of a logged value. Such a
// method panics when the value is a nil pointer of a type that does not
// expect one; logging it must not take the program down.
func safeText(method func() string) (text string) {
	defer func() {
		if recover() != nil {
			text = "<nil>"
		}
	}()
	return method()
}

// writeQuoted writes s between single quotes, escaped so that the closing
// quote is unambiguous.
func writeQuoted(b *strings.Builder, s string) {
	b.WriteByte('\'')
	for len(s) > 0 {
		i := strings.IndexAny(s, `'\`)
		if i < 0 {
			break
		}
		writeEscaped(b, s[:i])
		b.WriteByte('\\')
		b.WriteByte(s[i])
		s = s[i+1:]
	}
	writeEscaped(b, s)
	b.WriteByte('\'')
}

// writeEscaped writes s with everything that is not printable text escaped
// the way Go writes it in a string (\n, \x1b, ‎ ...). Line breaks would
// let logged text pass for a line of its own, and an escape character would
// let it control the terminal.
func writeEscaped(b *strings.Builder, s string) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(b, `\x%02x`, s[i])
		case unicode.IsPrint(r):
			b.WriteString(s[i : i+size])
		default:
			q := strconv.QuoteRune(r) // quoted: '\n'
			b.WriteString(q[1 : len(q)-1])
		}
		i += size
	}
}

// Printable returns s with everything that is not printable text escaped,
// as the log writes it. The name a device gives itself is whatever the
// device says, and must not reach a terminal as it is: it could move the
// cursor or clear the screen.
func Printable(s string) string {
	var b strings.Builder
	writeEscaped(&b, s)
	return b.String()
}
