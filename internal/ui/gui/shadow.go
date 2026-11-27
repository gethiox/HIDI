package gui

import (
	"slices"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/engine"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/midi"
)

// Shadow plays what a device does through an engine of its own, made of a
// profile as its page shows it, saved or not, and says what comes of it:
// which controls count as held, what the controllers and the pitch bend
// stand at, and where each axis rests.
//
// It is the engine HIDI plays with, given the same input. So the page
// shows what would be sent, with the dead zones, the trigger points and
// the hysteresis as they really work, whether HIDI is on or off, and
// nothing about any of that is reckoned a second time for the eye.
type Shadow struct {
	eng *engine.Engine
	// What the engine was made of: it is made anew when one of these is
	// another.
	profile  *config.Profile
	layout   string
	controls []control.Input
	// fed is what the engine was last told of each control.
	fed map[control.Code]float64
}

// Follow brings the shadow up to what the page shows and the device does
// now: the profile of doc for the device doc is shown for, the layout,
// the hysteresis of the settings as a part of an axis's travel, and the
// controls of the main part of the device with where they stand. A nil
// device changes nothing that is held.
func (s *Shadow) Follow(doc *ProfileDoc, layout string, hysteresis float64, device *ipc.Instrument) {
	if s.eng == nil || s.profile != doc.Profile || s.layout != layout || !slices.Equal(s.controls, doc.controls) {
		s.profile, s.layout, s.controls = doc.Profile, layout, slices.Clone(doc.controls)
		s.eng, s.fed = nil, map[control.Code]float64{}
		// The one layout that is shown, so that a command of the
		// device that steps through the layouts leaves the shadow
		// where the page is.
		made := doc.Profile.For(doc.controls)
		for _, l := range made.Layouts {
			if l.Name == layout {
				made.Layouts, made.Defaults.Layout = []engine.Layout{l}, layout
				break
			}
		}
		if eng, err := engine.New(made); err == nil {
			s.eng = eng
		}
	}
	if s.eng == nil || device == nil {
		return
	}
	s.eng.SetHysteresis(hysteresis)
	feed := func(name, part string, value float64) {
		code, err := control.ParseCode(name)
		if err != nil || part != "" {
			return
		}
		if was, told := s.fed[code]; told && was == value {
			return
		}
		s.fed[code] = value
		s.eng.Handle(control.Event{Code: code, Value: value}, nil)
	}
	for _, b := range device.Buttons {
		value := 0.0
		if b.Down {
			value = 1
		}
		feed(b.Name, b.Part, value)
	}
	for _, a := range device.Axes {
		if a.Known { // an axis that has said nothing stands nowhere
			feed(a.Name, a.Part, a.Value)
		}
	}
}

// Held reports whether a spot counts as pressed for what it is bound to.
func (s *Shadow) Held(at Spot) bool {
	return s.eng != nil && s.eng.Held(at.Code, "", at.Half)
}

// Level says which part of a bar the value fills that a spot sends, as
// fractions of the bar's width: from its left end for a controller, from
// the middle to the side it is bent to for the pitch bend. It reports
// false for a spot that sends neither, and before anything was sent.
func (s *Shadow) Level(at Spot) (from, to float64, ok bool) {
	if s.eng == nil {
		return 0, 0, false
	}
	sent, sends := s.eng.Sends(at.Code, "", at.Half)
	if !sends || !sent.Known {
		return 0, 0, false
	}
	if sent.PitchBend {
		bend := float64(midi.To14(sent.Value)) / 16383
		return min(bend, 0.5), max(bend, 0.5), true
	}
	return 0, float64(midi.To7(sent.Value)) / 127, true
}

// Neutral returns where an axis rests as the layout reads it, from 0 to
// 1, and false for an axis nothing reads.
func (s *Shadow) Neutral(code control.Code) (float64, bool) {
	if s.eng == nil {
		return 0, false
	}
	return s.eng.Neutral(code, "")
}

// Deadzones returns the parts of an axis's travel that its dead zones
// read as rest, as the engine plays them: the profile's dead zone, or the
// axis's own where it names one.
func (s *Shadow) Deadzones(code control.Code) [][2]float64 {
	if s.eng == nil {
		return nil
	}
	return s.eng.Deadzones(code, "")
}
