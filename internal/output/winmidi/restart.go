package winmidi

import (
	"sync"
	"time"
)

// restarts tells, from the process the MIDI service is seen to run in,
// when that process has ended.
type restarts struct {
	pid uint32
}

// see takes the process the service runs in now, 0 for none, and reports
// whether the process seen before has ended.
func (r *restarts) see(pid uint32) bool {
	ended := r.pid != 0 && pid != r.pid
	r.pid = pid
	return ended
}

// How long nothing is asked of a service that did not answer, at first
// and at most. The time doubles each time it is found stuck again.
const (
	firstStuckWait = 10 * time.Second
	maxStuckWait   = 5 * time.Minute
)

// stuckState remembers that a call into Windows did not return. Whatever
// is asked of the service after that would wait behind it, so nothing more
// is asked for a while: ten instruments then fail at once and say why,
// instead of each waiting its turn to find out the same. After the while
// it is tried again, since the service may have been restarted.
type stuckState struct {
	mu    sync.Mutex
	until time.Time
	wait  time.Duration
}

// mark notes that a call did not return at the given time.
func (s *stuckState) mark(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.wait == 0:
		s.wait = firstStuckWait
	case !now.Before(s.until):
		// Tried again after the wait, and stuck again.
		s.wait = min(2*s.wait, maxStuckWait)
	}
	s.until = now.Add(s.wait)
}

// is reports whether the service is to be left alone at the given time.
func (s *stuckState) is(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Before(s.until)
}

// clear forgets everything: the service answered, or is another one.
func (s *stuckState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.until, s.wait = time.Time{}, 0
}
