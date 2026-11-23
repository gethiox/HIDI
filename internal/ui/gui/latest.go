package gui

import "sync"

// latest hands values to a function that may be slow, a request to HIDI
// for one, without making the giver wait and without a queue: of the
// values given under one key while the function is busy, only the newest
// is handed over. One goroutine does the handing, for as long as
// something waits.
type latest[K comparable, V any] struct {
	// hand is what the values are handed to, one at a time.
	hand func(K, V)

	mu      sync.Mutex
	waiting map[K]V
	busy    bool
}

// put gives a value. It returns at once.
func (l *latest[K, V]) put(key K, value V) {
	l.mu.Lock()
	if l.waiting == nil {
		l.waiting = map[K]V{}
	}
	l.waiting[key] = value
	idle := !l.busy
	l.busy = true
	l.mu.Unlock()
	if !idle {
		return // the one at work takes it along
	}
	go func() {
		for {
			l.mu.Lock()
			var (
				key   K
				value V
				found bool
			)
			for key, value = range l.waiting {
				found = true
				break
			}
			if !found {
				l.busy = false
				l.mu.Unlock()
				return
			}
			delete(l.waiting, key)
			l.mu.Unlock()
			l.hand(key, value)
		}
	}()
}
