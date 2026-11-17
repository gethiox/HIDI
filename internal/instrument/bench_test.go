package instrument

import (
	"testing"

	"github.com/gethiox/HIDI/internal/control"
)

func BenchmarkInputSnapshot(b *testing.B) {
	var t inputTracker
	for i := range 104 {
		t.record(control.Event{Code: control.Key(uint16(i + 1)), Value: 1})
	}
	for i := range 15 {
		t.record(control.Event{Code: control.Axis(uint16(i)), Value: 0.5})
	}
	b.ReportAllocs()
	for b.Loop() {
		in := t.snapshot()
		for _, x := range in.Buttons {
			_ = x.Code.String()
		}
	}
}
