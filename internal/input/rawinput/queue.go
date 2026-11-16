package rawinput

import (
	"sync"

	"github.com/gethiox/HIDI/internal/control"
)

// queue carries events from the thread that receives them from Windows to
// the channel an instrument reads. That thread must never wait: Windows
// removes a keyboard hook whose thread does not answer in time. So events
// are put into a list that grows as needed, and a goroutine of the queue
// passes them on at the reader's pace.
type queue struct {
	out chan control.Event

	mu      sync.Mutex
	pending []control.Event
	closed  bool
	wake    chan struct{}
}

func newQueue() *queue {
	q := &queue{out: make(chan control.Event, 64), wake: make(chan struct{}, 1)}
	go q.pump()
	return q
}

// put adds an event. It never blocks. Events put after close are dropped.
func (q *queue) put(ev control.Event) {
	q.mu.Lock()
	if !q.closed {
		q.pending = append(q.pending, ev)
	}
	q.mu.Unlock()
	q.signal()
}

// close ends the queue: what is pending is still delivered, and then the
// output channel is closed.
func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) pump() {
	defer close(q.out)
	for range q.wake {
		for {
			q.mu.Lock()
			batch, closed := q.pending, q.closed
			q.pending = nil
			q.mu.Unlock()
			for _, ev := range batch {
				q.out <- ev
			}
			if closed && len(batch) == 0 {
				return
			}
			if len(batch) == 0 {
				break
			}
		}
	}
}
