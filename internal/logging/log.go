package logging

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

// DefaultKeep is how many lines a [Log] keeps in memory unless told
// otherwise. A line takes roughly 250 bytes.
const DefaultKeep = 10000

// ComponentKey is the attribute that names the component a line comes from.
// [Log.Logger] sets it; it is shown in square brackets instead of among the
// values.
const ComponentKey = "component"

// Options configure a [Log].
type Options struct {
	// Output receives the lines of at least Level. Nil writes nowhere; the
	// lines are still kept in memory.
	Output io.Writer
	// Colour writes ANSI colour codes to Output. See [TerminalColour].
	Colour bool
	// Level is the least level written to Output.
	Level slog.Level
	// KeepLevel is the least level kept in memory.
	KeepLevel slog.Level
	// Keep is how many lines are kept in memory; the oldest are dropped.
	// Zero means [DefaultKeep].
	Keep int
}

// Line is one line of the log.
type Line struct {
	Time      time.Time // zero if the line was logged without one
	Level     slog.Level
	Component string
	// Text is the whole line with its ANSI colour codes and without a line
	// end. [Strip] removes the codes and [Spans] splits the line at them.
	Text string
}

// Log writes log lines to an output and keeps the newest in memory. Its
// methods are safe for concurrent use.
type Log struct {
	level     slog.LevelVar
	keepLevel slog.LevelVar

	mu     sync.Mutex
	out    io.Writer
	colour bool
	lines  *ring[Line]
}

// New returns a log.
func New(opts Options) *Log {
	keep := opts.Keep
	if keep == 0 {
		keep = DefaultKeep
	}
	l := &Log{out: opts.Output, colour: opts.Colour, lines: newRing[Line](keep)}
	l.level.Set(opts.Level)
	l.keepLevel.Set(opts.KeepLevel)
	return l
}

// Handler returns the log as a [slog.Handler].
func (l *Log) Handler() slog.Handler { return &handler{log: l} }

// Logger returns the logger of one component of HIDI.
func (l *Log) Logger(component string) *slog.Logger {
	return slog.New(&handler{log: l, component: component})
}

// SetLevel sets the least level written to the output.
func (l *Log) SetLevel(level slog.Level) { l.level.Set(level) }

// Level returns the least level written to the output.
func (l *Log) Level() slog.Level { return l.level.Level() }

// SetKeepLevel sets the least level kept in memory.
func (l *Log) SetKeepLevel(level slog.Level) { l.keepLevel.Set(level) }

// KeepLevel returns the least level kept in memory.
func (l *Log) KeepLevel() slog.Level { return l.keepLevel.Level() }

// SetKeep sets how many lines are kept in memory, dropping the oldest if
// there are more.
func (l *Log) SetKeep(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines.setLimit(n)
}

// Lines kept in memory are numbered by the order they were logged in,
// starting at 0. The memory holds the numbers First to Total-1.

// Total returns how many lines have been kept since the start, including
// the ones dropped since.
func (l *Log) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(l.lines.total)
}

// First returns the number of the oldest line still kept.
func (l *Log) First() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(l.lines.total) - l.lines.len()
}

// Lines returns the lines numbered from to to-1, oldest first, limited to
// those still kept, and the number of the first line returned. An interface
// that remembers the previous Total can ask for just the new lines.
func (l *Log) Lines(from, to int) (lines []Line, first int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	oldest := int(l.lines.total) - l.lines.len()
	from, to = max(from, oldest), min(to, int(l.lines.total))
	if from >= to {
		return nil, from
	}
	lines = make([]Line, 0, to-from)
	for n := from; n < to; n++ {
		lines = append(lines, l.lines.at(n-oldest))
	}
	return lines, from
}

// Last returns the newest kept line of at least the given level.
func (l *Log) Last(level slog.Level) (Line, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := l.lines.len() - 1; i >= 0; i-- {
		if line := l.lines.at(i); line.Level >= level {
			return line, true
		}
	}
	return Line{}, false
}

func (l *Log) enabled(level slog.Level) bool {
	return level >= l.level.Level() || level >= l.keepLevel.Level()
}

func (l *Log) add(line Line) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if line.Level >= l.keepLevel.Level() {
		l.lines.push(line)
	}
	if l.out == nil || line.Level < l.level.Level() {
		return nil
	}
	text := line.Text
	if !l.colour {
		text = Strip(text)
	}
	_, err := io.WriteString(l.out, text+"\n")
	return err
}

// handler is the [slog.Handler] of a [Log].
type handler struct {
	log       *Log
	component string
	attrs     []slog.Attr // with their group prefix applied
	group     string      // prefix of the keys of further attributes
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return h.log.enabled(level)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := *h
	n.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		if a.Key == ComponentKey && h.group == "" {
			n.component = a.Value.String()
			continue
		}
		a.Key = h.group + a.Key
		n.attrs = append(n.attrs, a)
	}
	return &n
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	n := *h
	n.group = h.group + name + "."
	return &n
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	t := r.Time // zero when the caller left it out: the line then has none
	component := h.component
	attrs := h.attrs[:len(h.attrs):len(h.attrs)] // appending must copy
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == ComponentKey && h.group == "" {
			component = a.Value.String()
			return true
		}
		a.Key = h.group + a.Key
		attrs = append(attrs, a)
		return true
	})
	return h.log.add(Line{
		Time:      t,
		Level:     r.Level,
		Component: component,
		Text:      format(t, r.Level, component, r.Message, attrs),
	})
}
