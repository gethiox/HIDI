package engine_test

import (
	"testing"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

func builtin(b *testing.B, name string) *engine.Engine {
	profiles, err := config.Builtin()
	if err != nil {
		b.Fatal(err)
	}
	for _, p := range profiles {
		if p.Name == name {
			e, err := engine.New(p.Engine)
			if err != nil {
				b.Fatal(err)
			}
			return e
		}
	}
	b.Fatal("no profile " + name)
	return nil
}

func BenchmarkKey(b *testing.B) {
	e := builtin(b, "Default keyboard")
	var out []midi.Message
	b.ReportAllocs()
	for b.Loop() {
		out = e.Handle(control.Event{Code: control.Key(44), Value: 1}, out[:0])
		out = e.Handle(control.Event{Code: control.Key(44), Value: 0}, out[:0])
	}
}

func BenchmarkAxis(b *testing.B) {
	e := builtin(b, "Default gamepad")
	var out []midi.Message
	v := 0.0
	b.ReportAllocs()
	for b.Loop() {
		v += 0.001
		if v > 1 {
			v = 0
		}
		out = e.Handle(control.Event{Code: control.Axis(0), Value: v}, out[:0])
	}
}
