package winmidi

import (
	"testing"
	"time"
)

func TestRestarts(t *testing.T) {
	var r restarts
	for i, step := range []struct {
		pid   uint32
		ended bool
	}{
		{0, false},   // not running when first looked at
		{100, false}, // started
		{100, false},
		{0, true}, // ended
		{0, false},
		{200, false}, // started again
		{300, true},  // restarted between two looks
	} {
		if got := r.see(step.pid); got != step.ended {
			t.Errorf("step %d: see(%d) = %v, want %v", i, step.pid, got, step.ended)
		}
	}
}

func TestStuckState(t *testing.T) {
	var s stuckState
	now := time.Unix(1000, 0)
	if s.is(now) {
		t.Fatal("stuck before anything happened")
	}
	s.mark(now)
	if !s.is(now.Add(firstStuckWait - time.Second)) {
		t.Error("asked again before the first wait was over")
	}
	now = now.Add(firstStuckWait)
	if s.is(now) {
		t.Error("still left alone after the first wait")
	}
	// Stuck again on the next try: the wait doubles.
	s.mark(now)
	if !s.is(now.Add(2*firstStuckWait - time.Second)) {
		t.Error("the second wait is not twice the first")
	}
	// Further calls that give up during a wait do not lengthen it.
	s.mark(now.Add(time.Second))
	if s.is(now.Add(2*firstStuckWait + 2*time.Second)) {
		t.Error("a mark during the wait doubled it")
	}
	for range 20 {
		now = now.Add(maxStuckWait)
		s.mark(now)
	}
	if s.is(now.Add(maxStuckWait)) {
		t.Error("the wait grew past its limit")
	}
	s.clear()
	if s.is(now) {
		t.Error("stuck after being cleared")
	}
}
