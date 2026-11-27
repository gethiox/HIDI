package config

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/gethiox/HIDI/internal/engine"
)

// Same reports whether two profiles are the same profile: they select the
// same devices and make an instrument that does the same. Where they come
// from, and the order their bindings are written in, make no difference:
// a line taken out of a file and put back in lands at the end of its
// section, and the profile is what it was.
func Same(a, b *Profile) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(plain(a), plain(b))
}

// plain returns a copy of a profile with everything set aside that does
// not change what the profile is.
func plain(p *Profile) Profile {
	c := *p
	c.Source, c.Kind, c.Digest = "", "", [len(c.Digest)]byte{}
	c.Engine.Global = ordered(c.Engine.Global)
	c.Engine.Layouts = slices.Clone(c.Engine.Layouts)
	for i := range c.Engine.Layouts {
		c.Engine.Layouts[i].Bindings = ordered(c.Engine.Layouts[i].Bindings)
	}
	return c
}

// ordered returns bindings in an order that depends on nothing but what
// they bind.
func ordered(bindings []engine.Binding) []engine.Binding {
	out := slices.Clone(bindings)
	slices.SortStableFunc(out, func(x, y engine.Binding) int {
		kx, ky := fmt.Sprintf("%+v", x.Source), fmt.Sprintf("%+v", y.Source)
		switch {
		case kx < ky:
			return -1
		case kx > ky:
			return 1
		}
		return 0
	})
	return out
}
