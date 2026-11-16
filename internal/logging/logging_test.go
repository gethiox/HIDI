package logging

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

// lastLine returns the newest kept line without colours and without its
// date and time.
func lastLine(t *testing.T, l *Log) string {
	t.Helper()
	line, ok := l.Last(slog.LevelDebug - 100)
	if !ok {
		t.Fatal("nothing was logged")
	}
	text := Strip(line.Text)
	const stamp = "2006-01-02 15:04:05.000 "
	if len(text) < len(stamp) {
		t.Fatalf("line %q is shorter than its time", text)
	}
	if _, err := time.Parse(stamp, text[:len(stamp)]); err != nil {
		t.Fatalf("line %q does not start with its time: %v", text, err)
	}
	return text[len(stamp):]
}

func TestFormat(t *testing.T) {
	var nilErr *os.PathError
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{"message only", func(l *slog.Logger) { l.Info("started") }, " INFO [test] started"},
		{"levels are aligned", func(l *slog.Logger) { l.Error("broken") }, "ERROR [test] broken"},
		{"warning", func(l *slog.Logger) { l.Warn("careful") }, " WARN [test] careful"},
		{"values by kind", func(l *slog.Logger) {
			l.Info("m", "n", 3, "s", "x y", "ok", true, "f", 0.123456, "d", 1500*time.Millisecond)
		}, " INFO [test] m n=3 s='x y' ok=true f=0.1235 d=1.5s"},
		{"error value", func(l *slog.Logger) { l.Info("m", "error", errors.New("bad")) }, " INFO [test] m error='bad'"},
		{"nil value", func(l *slog.Logger) { l.Info("m", "reason", nil) }, " INFO [test] m reason=none"},
		{"nil error pointer", func(l *slog.Logger) { l.Info("m", "error", nilErr) }, " INFO [test] m error='<nil>'"},
		{"stringer", func(l *slog.Logger) { l.Info("m", "level", slog.LevelWarn) }, " INFO [test] m level='WARN'"},
		{"other value", func(l *slog.Logger) { l.Info("m", "list", []int{1, 2}) }, " INFO [test] m list=[1 2]"},
		{"logger values come first", func(l *slog.Logger) { l.With("device", "pad").Info("m", "n", 1) }, " INFO [test] m device='pad' n=1"},
		{"group", func(l *slog.Logger) { l.WithGroup("g").Info("m", "n", 1) }, " INFO [test] m g.n=1"},
		{"group value", func(l *slog.Logger) { l.Info("m", slog.Group("g", "a", 1, "b", "x")) }, " INFO [test] m g.a=1 g.b='x'"},
		{"component from a value", func(l *slog.Logger) { l.Info("m", ComponentKey, "other") }, " INFO [other] m"},
		{"component from With", func(l *slog.Logger) { l.With(ComponentKey, "other").Info("m") }, " INFO [other] m"},
		{"quote in a string", func(l *slog.Logger) { l.Info("m", "s", `it's a\b`) }, ` INFO [test] m s='it\'s a\\b'`},
		{"line break in a value", func(l *slog.Logger) { l.Info("m", "s", "a\nb") }, ` INFO [test] m s='a\nb'`},
		{"escape sequence in a value", func(l *slog.Logger) { l.Info("m", "s", "a\x1b[31mb") }, ` INFO [test] m s='a\x1b[31mb'`},
		{"escape sequence in a message", func(l *slog.Logger) { l.Info("a\x1b[2Jb\r\n") }, ` INFO [test] a\x1b[2Jb\r\n`},
		{"invalid UTF-8", func(l *slog.Logger) { l.Info("m", "s", "a\xffb") }, ` INFO [test] m s='a\xffb'`},
		{"text outside ASCII is kept", func(l *slog.Logger) { l.Info("m", "s", "Zażółć 鍵盤") }, ` INFO [test] m s='Zażółć 鍵盤'`},
		{"invisible character", func(l *slog.Logger) { l.Info("m", "s", "a\u202eb") }, ` INFO [test] m s='a\u202eb'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(Options{})
			tt.log(l.Logger("test"))
			if got := lastLine(t, l); got != tt.want {
				t.Errorf("line\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestNoComponent(t *testing.T) {
	l := New(Options{})
	slog.New(l.Handler()).Info("plain")
	if got := lastLine(t, l); got != " INFO plain" {
		t.Errorf("line %q", got)
	}
}

// A logged value must not be able to end the line and start a forged one,
// nor reach the terminal as an escape sequence.
func TestLineStaysOneLine(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Output: &buf, Colour: true})
	l.Logger("in\nput").Info("device\n2026-01-01 ERROR forged", "na\x1bme", "x\r\ny\x1b]0;title\a", "e", errors.New("a\nb"))
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("output is not one line: %q", out)
	}
	if strings.ContainsAny(Strip(out[:len(out)-1]), "\x1b\r\n\a") {
		t.Errorf("control characters reach the output: %q", out)
	}
}

func TestOutputLevelAndColour(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Output: &buf, Level: slog.LevelInfo, KeepLevel: slog.LevelDebug})
	log := l.Logger("test")
	log.Debug("quiet")
	log.Info("shown")
	if out := buf.String(); strings.Contains(out, "quiet") || !strings.HasSuffix(out, " INFO [test] shown\n") {
		t.Errorf("output %q", out)
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("colour codes without Colour: %q", buf.String())
	}
	if l.Total() != 2 {
		t.Errorf("%d lines kept, want 2: the memory has its own level", l.Total())
	}

	l.SetLevel(slog.LevelDebug)
	log.Debug("now shown")
	if !strings.Contains(buf.String(), "now shown") {
		t.Errorf("output after SetLevel: %q", buf.String())
	}

	buf.Reset()
	c := New(Options{Output: &buf, Colour: true})
	c.Logger("test").Info("m", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "\x1b[") || !strings.HasSuffix(Strip(out), " INFO [test] m k='v'\n") {
		t.Errorf("coloured output %q", out)
	}
}

func TestEnabled(t *testing.T) {
	l := New(Options{Level: slog.LevelWarn, KeepLevel: slog.LevelInfo})
	log := l.Logger("test")
	ctx := t.Context()
	if log.Enabled(ctx, slog.LevelDebug) {
		t.Error("debug is enabled although neither the output nor the memory takes it")
	}
	if !log.Enabled(ctx, slog.LevelInfo) {
		t.Error("info is disabled although the memory keeps it")
	}
	l.SetKeepLevel(slog.LevelError)
	if log.Enabled(ctx, slog.LevelInfo) || !log.Enabled(ctx, slog.LevelWarn) {
		t.Error("after SetKeepLevel: want info disabled and warn enabled")
	}
	log.Warn("output only")
	if l.Total() != 0 {
		t.Error("a line below the keep level was kept")
	}
	if l.Level() != slog.LevelWarn || l.KeepLevel() != slog.LevelError {
		t.Errorf("levels %v, %v", l.Level(), l.KeepLevel())
	}
}

func TestMemory(t *testing.T) {
	l := New(Options{Keep: 10})
	log := l.Logger("test")
	for i := range 25 {
		log.Info("line", "i", i)
	}
	if l.Total() != 25 || l.First() != 15 {
		t.Fatalf("total %d, first %d; want 25 and 15", l.Total(), l.First())
	}
	lines, first := l.Lines(0, 100)
	if first != 15 || len(lines) != 10 {
		t.Fatalf("Lines(0, 100): %d lines from %d", len(lines), first)
	}
	for i, line := range lines {
		if want := fmt.Sprintf("i=%d", 15+i); !strings.HasSuffix(Strip(line.Text), want) {
			t.Errorf("line %d is %q, want it to end %q", i, Strip(line.Text), want)
		}
		if line.Component != "test" || line.Level != slog.LevelInfo || line.Time.IsZero() {
			t.Errorf("line %d: %+v", i, line)
		}
	}
	if lines, first := l.Lines(20, 22); first != 20 || len(lines) != 2 || !strings.HasSuffix(Strip(lines[1].Text), "i=21") {
		t.Errorf("Lines(20, 22): %d lines from %d", len(lines), first)
	}
	if lines, _ := l.Lines(25, 30); lines != nil {
		t.Errorf("Lines past the end: %d lines", len(lines))
	}

	l.SetKeep(3)
	if lines, first := l.Lines(0, 100); first != 22 || len(lines) != 3 || !strings.HasSuffix(Strip(lines[2].Text), "i=24") {
		t.Errorf("after SetKeep(3): %d lines from %d", len(lines), first)
	}
	log.Info("line", "i", 25)
	l.SetKeep(5)
	log.Info("line", "i", 26)
	lines, first = l.Lines(0, 100)
	if first != 23 || len(lines) != 4 || !strings.HasSuffix(Strip(lines[0].Text), "i=23") || !strings.HasSuffix(Strip(lines[3].Text), "i=26") {
		t.Errorf("after growing: %d lines from %d", len(lines), first)
	}
}

func TestLast(t *testing.T) {
	l := New(Options{})
	if _, ok := l.Last(slog.LevelInfo); ok {
		t.Error("Last on an empty log found a line")
	}
	log := l.Logger("test")
	log.Warn("first warning")
	log.Error("the error")
	log.Info("after")
	if line, ok := l.Last(slog.LevelWarn); !ok || !strings.HasSuffix(line.Text, "the error") {
		t.Errorf("Last(warn) = %q, %v", line.Text, ok)
	}
}

func TestConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Output: &buf, Keep: 50})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			log := l.Logger(fmt.Sprint("part", g)).With("g", g)
			for i := range 100 {
				log.Info("line", "i", i)
				l.Lines(l.First(), l.Total())
			}
		})
	}
	wg.Wait()
	if l.Total() != 800 || strings.Count(buf.String(), "\n") != 800 {
		t.Errorf("total %d, %d lines written", l.Total(), strings.Count(buf.String(), "\n"))
	}
}

// A logger derived with With must not share its values with its siblings.
func TestWithDoesNotAlias(t *testing.T) {
	l := New(Options{})
	base := l.Logger("test").With("a", 1)
	one, two := base.With("b", 1), base.With("c", 2)
	one.Info("m", "x", 1)
	if got := lastLine(t, l); got != " INFO [test] m a=1 b=1 x=1" {
		t.Errorf("first: %q", got)
	}
	two.Info("m")
	if got := lastLine(t, l); got != " INFO [test] m a=1 c=2" {
		t.Errorf("second: %q", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestOutputErrorIsReturned(t *testing.T) {
	l := New(Options{Output: failingWriter{}})
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "m", 0)
	if err := l.Handler().Handle(t.Context(), r); err == nil {
		t.Error("a failed write was not reported")
	}
	if l.Total() != 1 {
		t.Error("the line was not kept although only the output failed")
	}
}

// The handler follows the rules log/slog sets for handlers.
func TestSlogConformance(t *testing.T) {
	var l *Log
	slogtest.Run(t, func(*testing.T) slog.Handler {
		l = New(Options{})
		return l.Handler()
	}, func(t *testing.T) map[string]any {
		return parseLine(t, lastLineRaw(t, l))
	})
}

func lastLineRaw(t *testing.T, l *Log) Line {
	t.Helper()
	line, ok := l.Last(slog.LevelDebug - 100)
	if !ok {
		t.Fatal("nothing was logged")
	}
	return line
}

// parseLine turns a line back into the nested map slogtest checks.
func parseLine(t *testing.T, line Line) map[string]any {
	t.Helper()
	text := Strip(line.Text)
	m := map[string]any{slog.LevelKey: line.Level}
	fields := strings.Fields(text)
	rest := fields
	if len(fields) >= 2 {
		if _, err := time.Parse("2006-01-02 15:04:05.000", fields[0]+" "+fields[1]); err == nil {
			m[slog.TimeKey] = line.Time
			rest = fields[2:]
		}
	}
	rest = rest[1:] // the level
	var msg []string
	for _, f := range rest {
		key, value, isAttr := strings.Cut(f, "=")
		if !isAttr {
			msg = append(msg, f)
			continue
		}
		at := m
		parts := strings.Split(key, ".")
		for _, g := range parts[:len(parts)-1] {
			sub, ok := at[g].(map[string]any)
			if !ok {
				sub = map[string]any{}
				at[g] = sub
			}
			at = sub
		}
		at[parts[len(parts)-1]] = strings.Trim(value, "'")
	}
	m[slog.MessageKey] = strings.Join(msg, " ")
	return m
}

func TestSpansAndStrip(t *testing.T) {
	spans := Spans("a\x1b[31mb\x1b[0mc\x1b[1;92md\x1b[39me\x1b[mf")
	want := []Span{{"a", Colour{}}, {"b", Colour{31}}, {"c", Colour{}}, {"d", Colour{92}}, {"e", Colour{}}, {"f", Colour{}}}
	if len(spans) != len(want) {
		t.Fatalf("spans %+v", spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d is %+v, want %+v", i, spans[i], want[i])
		}
	}
	for in, want := range map[string]string{
		"plain":               "plain",
		"x\x1b[1;32my\x1b[0m": "xy",
		"unfinished\x1b[31":   "unfinished31",
		"":                    "",
	} {
		if got := Strip(in); got != want {
			t.Errorf("Strip(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every span of a real line carries text, and together they are the line.
func TestSpansOfALine(t *testing.T) {
	l := New(Options{})
	l.Logger("test").Warn("message", "key", "value", "n", 1)
	line := lastLineRaw(t, l)
	var joined strings.Builder
	colours := map[int]bool{}
	for _, s := range Spans(line.Text) {
		joined.WriteString(s.Text)
		colours[s.Fg.Code] = true
	}
	if joined.String() != Strip(line.Text) {
		t.Errorf("spans join to %q, the line is %q", joined.String(), Strip(line.Text))
	}
	if len(colours) < 5 {
		t.Errorf("only %d colours in a line with time, level, component, key and values", len(colours))
	}
}

func TestComponentColourIsStable(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range []string{"hidi", "input", "instrument", "output", "config", "ui", "ipc"} {
		c := componentColour(name)
		if c != componentColour(name) {
			t.Errorf("colour of %q changes", name)
		}
		if c == red || c == brightRed {
			t.Errorf("%q got red, which is for errors", name)
		}
		seen[c] = true
	}
	if len(seen) < 3 {
		t.Errorf("seven components share %d colours", len(seen))
	}
}

func TestColourChoice(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	if colourChoice(file, env()) {
		t.Error("a file gets colours")
	}
	if colourChoice(nil, env()) {
		t.Error("no file gets colours")
	}
	if !colourChoice(file, env("FORCE_COLOR", "1")) {
		t.Error("FORCE_COLOR is ignored")
	}
	if colourChoice(file, env("FORCE_COLOR", "1", "NO_COLOR", "1")) {
		t.Error("NO_COLOR does not win over FORCE_COLOR")
	}
}

func BenchmarkLog(b *testing.B) {
	l := New(Options{Keep: 1000})
	log := l.Logger("bench").With("device", "Keychron K2")
	b.ReportAllocs()
	for b.Loop() {
		log.Info("instrument changed", "octave", 1, "channel", 3, "layout", "piano")
	}
}
