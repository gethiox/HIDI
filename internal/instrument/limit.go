package instrument

import (
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// limiter thins out the events of absolute axes. A stick or a lever can
// report a thousand positions a second, far more than a MIDI connection
// should carry or a listener can hear. Each axis passes at most one event
// per interval; of those that arrive in between only the newest is kept,
// and it is passed on when the interval is over, so the last position
// always arrives.
//
// Keys are never held back: their timing is the music. Relative axes are
// not either, because each of their events is a movement and dropping one
// would lose it.
type limiter struct {
	interval time.Duration
	axes     map[axisKey]*axisState
	// waiting counts the axes that hold an event back.
	waiting int
}

type axisKey struct {
	code control.Code
	part string
}

type axisState struct {
	passed  time.Time // when the last event went through
	held    control.Event
	holding bool
}

// newLimiter returns a limiter that lets each axis through rate times a
// second. A rate of zero or less lets everything through.
func newLimiter(rate int) *limiter {
	l := &limiter{axes: map[axisKey]*axisState{}}
	if rate > 0 {
		l.interval = time.Second / time.Duration(rate)
	}
	return l
}

// admit reports whether an event that arrived at now goes through right
// away. If not, the limiter holds it until [limiter.due] returns it.
func (l *limiter) admit(ev control.Event, now time.Time) bool {
	if l.interval <= 0 || ev.Code.Kind != control.KindAxis {
		return true
	}
	key := axisKey{ev.Code, ev.Part}
	st := l.axes[key]
	if st == nil {
		st = &axisState{}
		l.axes[key] = st
	}
	if !st.holding && now.Sub(st.passed) >= l.interval {
		st.passed = now
		return true
	}
	if !st.holding {
		st.holding = true
		l.waiting++
	}
	st.held = ev
	return false
}

// due appends to out the held events whose interval is over at now, and
// returns how long until the next one is, or zero if none is held.
func (l *limiter) due(now time.Time, out []control.Event) (events []control.Event, wait time.Duration) {
	if l.waiting == 0 {
		return out, 0
	}
	for _, st := range l.axes {
		if !st.holding {
			continue
		}
		if remaining := l.interval - now.Sub(st.passed); remaining > 0 {
			if wait == 0 || remaining < wait {
				wait = remaining
			}
			continue
		}
		out = append(out, st.held)
		st.holding, st.passed = false, now
		l.waiting--
	}
	return out, wait
}

// drop forgets every held event, for when the instrument is turned off or
// gets another profile: a position from before must not arrive after.
func (l *limiter) drop() {
	for _, st := range l.axes {
		st.holding = false
	}
	l.waiting = 0
}
