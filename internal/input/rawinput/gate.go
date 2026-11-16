package rawinput

import "sync/atomic"

// gate decides which keys the keyboard hook keeps from other programs.
//
// While a keyboard is grabbed, keys are swallowed. The exception is the
// release of a key whose press the other programs saw before the grab
// began: without it they would take the key for held, and the Enter that
// started HIDI would repeat forever. The same holds the other way round: a
// key pressed while grabbed is swallowed until it is released, even if the
// grab ended in between, so that no program sees a release without a
// press.
type gate struct {
	// grabs counts the grabbed keyboards. It is the only field other
	// threads touch.
	grabs atomic.Int32
	// hides counts the keyboards that are passed on except for some keys,
	// and hidden holds those keys by their Linux code. A hidden key is
	// swallowed like any key of a grabbed keyboard.
	hides  atomic.Int32
	hidden atomic.Pointer[map[uint16]bool]
	// passed holds the keys, by virtual key code, whose press was let
	// through and whose release is still to come.
	passed map[uint32]bool
	// held holds the keys whose press was swallowed.
	held map[uint32]bool
}

// swallow reports whether a key event is kept from other programs. It
// decides by grabs alone, for a key whose code is not known.
func (g *gate) swallow(vk uint32, up bool) bool { return g.swallowKey(vk, 0, false, up) }

// kept reports whether a key that goes down now is to be swallowed. known
// says that code is the key's Linux code.
func (g *gate) kept(code uint16, known bool) bool {
	if g.grabs.Load() > 0 {
		return true
	}
	if !known || g.hides.Load() == 0 {
		return false
	}
	hidden := g.hidden.Load()
	return hidden != nil && (*hidden)[code]
}

// swallowKey is swallow for a key whose Linux code may be known.
func (g *gate) swallowKey(vk uint32, code uint16, known, up bool) bool {
	if g.held == nil {
		g.held = map[uint32]bool{}
	}
	if g.passed == nil {
		g.passed = map[uint32]bool{}
	}
	if up {
		switch {
		case g.passed[vk]:
			delete(g.passed, vk)
			return false
		case g.held[vk]:
			delete(g.held, vk)
			return true
		}
		// A release without a known press: decide by the present state.
		return g.kept(code, known)
	}
	switch {
	case g.passed[vk]:
		return false // a repeat of a key the programs know is down
	case g.held[vk]:
		return true // a repeat of a swallowed key
	case g.kept(code, known):
		g.held[vk] = true
		return true
	}
	g.passed[vk] = true
	return false
}
