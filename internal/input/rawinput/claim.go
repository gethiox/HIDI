package rawinput

// claims decides which keyboard a swallowed key belongs to.
//
// Windows does not report a key through Raw Input once the keyboard hook
// has swallowed it, so the hook is where HIDI learns of the keys of a
// grabbed keyboard. The hook does not know the keyboard. A swallowed key
// is therefore given to the keyboard last seen typing while keys still
// passed, and before any was seen, to the first keyboard that keeps its
// keys from other programs.
type claims[D comparable] struct {
	// typed is the keyboard Raw Input last reported a key of.
	typed    D
	hasTyped bool
	// order holds the keyboards as they arrived.
	order []D
	// down holds who was given the press of each key that is still down,
	// so that the release goes the same way.
	down map[uint16]D
}

func (c *claims[D]) add(d D) { c.order = append(c.order, d) }

func (c *claims[D]) remove(d D) {
	for i, other := range c.order {
		if other == d {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	if c.hasTyped && c.typed == d {
		var none D
		c.typed, c.hasTyped = none, false
	}
	for code, other := range c.down {
		if other == d {
			delete(c.down, code)
		}
	}
}

// saw notes that Raw Input reported a key of d.
func (c *claims[D]) saw(d D) { c.typed, c.hasTyped = d, true }

// owner returns the keyboard a swallowed key event goes to. takes tells
// whether a keyboard keeps keys from other programs at the moment.
func (c *claims[D]) owner(code uint16, pressed bool, takes func(D) bool) (D, bool) {
	if c.down == nil {
		c.down = map[uint16]D{}
	}
	if d, ok := c.down[code]; ok {
		if !pressed {
			delete(c.down, code)
		}
		return d, true
	}
	d, ok := c.choose(takes)
	if ok && pressed {
		c.down[code] = d
	}
	return d, ok
}

func (c *claims[D]) choose(takes func(D) bool) (D, bool) {
	if c.hasTyped && takes(c.typed) {
		return c.typed, true
	}
	for _, d := range c.order {
		if takes(d) {
			return d, true
		}
	}
	if c.hasTyped {
		return c.typed, true
	}
	if len(c.order) > 0 {
		return c.order[0], true
	}
	var none D
	return none, false
}
