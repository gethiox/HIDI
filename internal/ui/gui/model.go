package gui

import (
	"strings"
	"sync"

	"github.com/gethiox/HIDI/internal/ipc"
)

// Levels are the levels a log line can have, least severe first, as HIDI
// names them.
var Levels = []string{"DEBUG", "INFO", "WARN", "ERROR"}

// levelRank orders the levels. A level with an offset, such as "DEBUG-4"
// or "INFO+2", ranks with its base.
func levelRank(level string) int {
	for i := len(Levels) - 1; i >= 0; i-- {
		if strings.HasPrefix(level, Levels[i]) {
			return i
		}
	}
	return 0
}

// maxLogLines is how much of the log the window keeps.
const maxLogLines = 2000

// Model is what the window shows. The goroutine that follows HIDI writes
// it, the one that draws reads it.
type Model struct {
	mu       sync.Mutex
	state    *ipc.Snapshot // nil while HIDI is not running
	lines    []ipc.LogLine
	minLevel int
	// version counts the changes, so that the drawing side can tell
	// whether what it prepared is still current.
	version uint64
}

// SetSnapshot takes in the state of HIDI; nil for a HIDI that is not
// running.
func (m *Model) SetSnapshot(s *ipc.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	m.version++
}

// Snapshot returns the state of HIDI, or nil while it is not running.
func (m *Model) Snapshot() *ipc.Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// AddLog takes in a log line. A HIDI that is found again sends the end of
// its log anew; lines the window already has are not added twice.
func (m *Model) AddLog(line ipc.LogLine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.lines); n > 0 {
		last := m.lines[n-1]
		if line.Seq <= last.Seq && !line.Time.After(last.Time) {
			return
		}
	}
	m.lines = append(m.lines, line)
	if len(m.lines) > maxLogLines {
		m.lines = append(m.lines[:0], m.lines[len(m.lines)-maxLogLines:]...)
	}
	m.version++
}

// SetMinLevel chooses the least severe level the log shows, as an index
// into [Levels].
func (m *Model) SetMinLevel(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minLevel = min(max(level, 0), len(Levels)-1)
	m.version++
}

// MinLevel returns the least severe level the log shows.
func (m *Model) MinLevel() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.minLevel
}

// Version changes whenever anything in the model does.
func (m *Model) Version() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

// Log returns the lines the filter lets through, oldest first.
func (m *Model) Log() []ipc.LogLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ipc.LogLine, 0, len(m.lines))
	for _, l := range m.lines {
		if levelRank(l.Level) >= m.minLevel {
			out = append(out, l)
		}
	}
	return out
}
