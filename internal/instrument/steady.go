package instrument

import (
	"math"
	"time"

	"github.com/gethiox/HIDI/internal/control"
)

// steadier keeps the trembling of a resting axis from being passed on. A
// lever left alone rarely reports one position: it wavers between two
// neighbouring ones, and every waver would become a MIDI message, without
// end.
//
// An axis is either resting or moving. A resting axis has a position it
// rests at, and what it reports within the threshold of that position is
// dropped. Once it is further away it is moving, and then everything it
// reports is passed on, in full detail. It rests again when it has stayed
// within the threshold of one position for a while.
//
// Moving the axis slowly does not get past this unnoticed, as it would
// with an average: the rest position stays where it is, so a slow movement
// leaves it behind like any other. All that is lost is the first threshold
// of the way, which arrives with the first position passed on.
type steadier struct {
	threshold float64
	axes      map[axisKey]*steadyState
}

type steadyState struct {
	// rest is the position the axis rests at, or, while it moves, the
	// position from which the next clear movement is measured.
	rest float64
	// passed is the position that was passed on last.
	passed float64
	moving bool
	// moved is when the axis last moved clearly.
	moved time.Time
}

// settleTime is how long a moving axis has to stay in one place to count
// as resting again.
const settleTime = 250 * time.Millisecond

// newSteadier returns a steadier in which a resting axis has to move by
// more than threshold, a fraction of its whole travel, to count as moved.
// A threshold of zero or less passes everything.
func newSteadier(threshold float64) *steadier {
	return &steadier{threshold: threshold, axes: map[axisKey]*steadyState{}}
}

// admit reports whether an event that arrived at now is passed on.
func (s *steadier) admit(ev control.Event, now time.Time) bool {
	if s.threshold <= 0 || ev.Code.Kind != control.KindAxis {
		return true
	}
	key := axisKey{ev.Code, ev.Part}
	v := ev.Value
	st := s.axes[key]
	if st == nil {
		// The first position is where the axis rests, and is passed on so
		// that the instrument knows where the axis stands.
		s.axes[key] = &steadyState{rest: v, passed: v}
		return true
	}
	if st.moving && now.Sub(st.moved) > settleTime {
		st.moving, st.rest = false, st.passed
	}
	switch {
	case math.Abs(v-st.rest) > s.threshold:
		st.moving, st.rest, st.moved = true, v, now
	case st.moving:
	case (v <= 0 || v >= 1) && v != st.passed:
		// The ends of the travel always arrive: an axis that came to rest
		// just short of one can still be pushed all the way.
	default:
		return false
	}
	st.passed = v
	return true
}
