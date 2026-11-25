package engine

import (
	"cmp"
	"slices"

	"github.com/gethiox/HIDI/internal/control"
)

// KeyRole is what a key of the main part of the device does when it is
// pressed alone, without modifiers.
type KeyRole struct {
	Code   control.Code
	Action Action
}

// Keys returns what every bound key does in the layout that is selected,
// ordered by code, and which of the profile's layouts that is. It is for
// showing the instrument, on the keyboard's own lights for one.
func (e *Engine) Keys() (keys []KeyRole, layout, layouts int) {
	for key, bindings := range e.index[e.layout] {
		if key.part != "" || key.code.Kind != control.KindKey {
			continue
		}
		// The layout's own bindings come first in the index, so the first
		// binding without modifiers is the one a lone press gets.
		for _, b := range bindings {
			if len(b.Source.Modifiers) == 0 && b.Source.Range == RangeWhole {
				// A key that is to do nothing has no part to show.
				if _, none := b.Action.(Nothing); !none {
					keys = append(keys, KeyRole{Code: key.code, Action: b.Action})
				}
				break
			}
		}
	}
	slices.SortFunc(keys, func(a, b KeyRole) int { return cmp.Compare(a.Code.Num, b.Code.Num) })
	return keys, e.layout, len(e.profile.Layouts)
}

// Passed returns the keys the selected layout leaves to the rest of the
// system (see [Passthrough]), ordered by code.
func (e *Engine) Passed() []control.Code {
	var out []control.Code
	for key, bindings := range e.index[e.layout] {
		if key.part != "" || key.code.Kind != control.KindKey {
			continue
		}
		for _, b := range bindings {
			if len(b.Source.Modifiers) == 0 && b.Source.Range == RangeWhole {
				if _, passed := b.Action.(Passthrough); passed {
					out = append(out, key.code)
				}
				break
			}
		}
	}
	slices.SortFunc(out, func(a, b control.Code) int { return cmp.Compare(a.Num, b.Num) })
	return out
}

// Sent is something the selected layout sends by the position of a
// control: a controller, or the pitch bend.
type Sent struct {
	// Source, Part and Range say which control drives it, and which
	// part of its travel.
	Source control.Code
	Part   string
	Range  Range
	// PitchBend says that it is the pitch bend; otherwise Controller is
	// the number of the controller. Channel is the channel it goes out
	// on now, from 0.
	PitchBend  bool
	Controller uint8
	Channel    uint8
	// Value is what was last sent, in MIDI's own units (see
	// [midi.To7] and [midi.To14]), if Known; nothing was sent yet
	// otherwise.
	Value uint32
	Known bool
}

// Controllers returns what the selected layout sends by the position of
// controls, on the channel that is selected: each controller and the
// pitch bend once, in the order of the controls that drive them, axes
// before keys, with the value last sent.
func (e *Engine) Controllers() []Sent {
	var out []Sent
	seen := map[target]bool{}
	for key, bindings := range e.index[e.layout] {
		for _, b := range bindings {
			var t target
			switch a := b.Action.(type) {
			case ControlChange:
				t = target{channel: e.channelFor(a.ChannelOffset), controller: a.Controller}
			case PitchBend:
				t = target{pitchBend: true, channel: e.channelFor(a.ChannelOffset)}
			default:
				continue
			}
			s := Sent{Source: key.code, Part: key.part, Range: b.Source.Range, PitchBend: t.pitchBend, Controller: t.controller, Channel: t.channel}
			if ts := e.targets[t]; ts != nil {
				s.Value, s.Known = ts.value, true
			}
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b Sent) int {
		return cmp.Or(
			-cmp.Compare(a.Source.Kind, b.Source.Kind), // axes before keys
			cmp.Compare(a.Part, b.Part),
			cmp.Compare(a.Source.Num, b.Source.Num),
			cmp.Compare(a.Range, b.Range),
			cmp.Compare(a.Controller, b.Controller),
		)
	})
	// Two controls may drive one target, the halves of a stick that bend
	// the pitch up and down for one: it is listed once, by the first.
	kept := out[:0]
	for _, s := range out {
		t := target{pitchBend: s.PitchBend, channel: s.Channel, controller: s.Controller}
		if !seen[t] {
			seen[t] = true
			kept = append(kept, s)
		}
	}
	return kept
}
