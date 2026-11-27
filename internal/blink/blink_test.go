package blink

import (
	"testing"
	"time"
)

func TestHalves(t *testing.T) {
	start := time.Unix(1000, 0) // a whole number of halves since 1970
	if Lit(start) || !Lit(start.Add(Phase)) || Lit(start.Add(2*Phase)) {
		t.Error("the halves do not take turns")
	}
	if Lit(start.Add(Phase-time.Nanosecond)) != Lit(start) {
		t.Error("a half ended before its time")
	}
	if got := Next(start.Add(Phase / 3)); !got.Equal(start.Add(Phase)) {
		t.Errorf("the next half begins at %v, want %v", got, start.Add(Phase))
	}
	if got := Next(start); !got.Equal(start.Add(Phase)) {
		t.Errorf("from the start of a half, the next begins at %v", got)
	}
}
