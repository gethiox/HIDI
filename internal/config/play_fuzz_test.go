package config

import (
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/midi"
)

// fuzzControls is what the device of FuzzPlay says it has: the buttons of
// a pad and of a stick, axes, and two hats, so that what a profile's rule
// makes is played like what the profile names.
func fuzzControls() []control.Input {
	var out []control.Input
	for n := range uint16(15) {
		out = append(out, control.Input{Code: control.Key(0x120 + n)}, control.Input{Code: control.Key(0x130 + n)})
	}
	for n := range uint16(8) {
		out = append(out, control.Input{Code: control.Axis(n)})
	}
	for n := range uint16(4) {
		out = append(out, control.Input{Code: control.Axis(0x10 + n), Hat: true})
	}
	return out
}

// FuzzPlay plays the profiles HIDI comes with by whatever the fuzzer
// makes of a device: keys and axes going up and down in any order, the
// commands among them. Whatever is played, every message is a message,
// the engine's count of sounding notes agrees with the notes it turned on
// and off, and once everything is let go of and the engine is reset,
// nothing sounds.
func FuzzPlay(f *testing.F) {
	profiles, err := Builtin()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(uint8(0), []byte{0, 255, 1, 255, 0, 0, 1, 0})
	f.Add(uint8(1), []byte{3, 200, 9, 10, 3, 0, 40, 255, 41, 255, 40, 0})
	f.Add(uint8(4), []byte{7, 128, 7, 255, 7, 0, 2, 255})
	f.Fuzz(func(t *testing.T, which uint8, play []byte) {
		p := profiles[int(which)%len(profiles)]
		made := p.For(fuzzControls())
		eng, err := engine.New(made)
		if err != nil {
			t.Fatal(err)
		}
		// The controls the profile binds, in a fixed order.
		type source struct {
			Code control.Code
			Part string
		}
		var sources []source
		seen := map[source]bool{}
		add := func(bs []engine.Binding) {
			for _, b := range bs {
				s := source{b.Source.Code, b.Source.Part}
				if !seen[s] {
					seen[s] = true
					sources = append(sources, s)
				}
			}
		}
		add(made.Global)
		for _, l := range made.Layouts {
			add(l.Bindings)
		}
		if len(sources) == 0 {
			return
		}

		on := map[[2]uint8]int{} // channel and note -> how often it is on
		var out []midi.Message
		check := func(what string) {
			for _, m := range out {
				if m.Kind < midi.KindNoteOff || m.Kind > midi.KindPitchBend || m.Channel > 15 || m.Index > 127 {
					t.Fatalf("%s: not a message: %+v", what, m)
				}
				key := [2]uint8{m.Channel, m.Index}
				switch m.Kind {
				case midi.KindNoteOn:
					on[key]++
				case midi.KindNoteOff:
					// The panic command turns every note off, on or not.
					if on[key] > 0 {
						on[key]--
					}
				}
			}
			sounding := 0
			for _, n := range on {
				sounding += n
			}
			if got := eng.State().Sounding; got != sounding {
				t.Fatalf("%s: the engine counts %d sounding notes, the messages say %d", what, got, sounding)
			}
		}
		for i := 0; i+1 < len(play); i += 2 {
			s := sources[int(play[i])%len(sources)]
			ev := control.Event{Code: s.Code, Part: s.Part, Value: float64(play[i+1]) / 255}
			out = eng.Handle(ev, out[:0])
			check("playing")
		}
		// Everything is let go of: keys up, axes to the middle and to
		// rest.
		for _, s := range sources {
			for _, v := range []float64{0.5, 0} {
				out = eng.Handle(control.Event{Code: s.Code, Part: s.Part, Value: v}, out[:0])
				check("letting go")
			}
		}
		out = eng.Reset(out[:0])
		check("resetting")
		if n := eng.State().Sounding; n != 0 {
			t.Fatalf("%d notes sound after everything was let go of and the engine reset", n)
		}
	})
}
