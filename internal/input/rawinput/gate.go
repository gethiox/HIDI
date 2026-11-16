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
	// passed holds the keys, by virtual key code, whose press was let
	// through and whose release is still to come.
	passed map[uint32]bool
	// held holds the keys whose press was swallowed.
	held map[uint32]bool
}

// swallow reports whether a key event is kept from other programs.
func (g *gate) swallow(vk uint32, up bool) bool {
	if g.held == nil {
		g.held = map[uint32]bool{}
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
		return g.grabs.Load() > 0
	}
	switch {
	case g.passed[vk]:
		return false // a repeat of a key the programs know is down
	case g.held[vk]:
		return true // a repeat of a swallowed key
	case g.grabs.Load() > 0:
		g.held[vk] = true
		return true
	}
	g.passed[vk] = true
	return false
}
