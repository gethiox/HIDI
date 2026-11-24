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
