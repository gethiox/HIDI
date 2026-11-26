package rawinput

import (
	"sync"
	"sync/atomic"
)

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
	// open holds the keys that are let through although a keyboard is
	// grabbed, by their Linux code; nil when there are none.
	open atomic.Pointer[map[uint16]bool]
	// keys holds the hidden and the open keys each keyboard asked for, by
	// the keyboard: Windows cannot tell keyboards apart in the hook, so
	// hidden and open are what all of them ask for together, and one
	// keyboard that stops asking takes nothing from the others.
	keys struct {
		sync.Mutex
		hidden, open map[any]map[uint16]bool
	}
	// passed holds the keys whose press was let through and whose release
	// is still to come, by what the hook tells them apart by.
	passed map[uint32]bool
	// held holds the keys whose press was swallowed.
	held map[uint32]bool
}

// hide sets the keys a keyboard keeps from other programs while it is
// passed on, and open those it lets through while it is grabbed; nil
// ends that for the keyboard. The hook goes by what every keyboard asks
// for together.
func (g *gate) hide(keyboard any, keys map[uint16]bool) {
	g.keys.Lock()
	defer g.keys.Unlock()
	g.hidden.Store(merge(&g.keys.hidden, keyboard, keys))
}

func (g *gate) letThrough(keyboard any, keys map[uint16]bool) {
	g.keys.Lock()
	defer g.keys.Unlock()
	g.open.Store(merge(&g.keys.open, keyboard, keys))
}

// merge puts the keys of one keyboard among those of the others and
// returns all of them together, or nil for none.
func merge(by *map[any]map[uint16]bool, keyboard any, keys map[uint16]bool) *map[uint16]bool {
	if *by == nil {
		*by = map[any]map[uint16]bool{}
	}
	if keys == nil {
		delete(*by, keyboard)
	} else {
		(*by)[keyboard] = keys
	}
	if len(*by) == 0 {
		return nil
	}
	all := map[uint16]bool{}
	for _, set := range *by {
		for code := range set {
			all[code] = true
		}
	}
	return &all
}

// swallow reports whether a key event is kept from other programs. It
// decides by grabs alone, for a key whose code is not known.
func (g *gate) swallow(vk uint32, up bool) bool { return g.swallowKey(vk, 0, false, up) }

// kept reports whether a key that goes down now is to be swallowed. known
// says that code is the key's Linux code.
func (g *gate) kept(code uint16, known bool) bool {
	if g.grabs.Load() > 0 {
		if open := g.open.Load(); known && open != nil && (*open)[code] {
			return false
		}
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
		// A release without a known press, of a key that went down
		// before HIDI looked: the other programs saw that press and get
		// the release too. Kept from them, the key would stay down for
		// them, and a Ctrl held while HIDI started would turn every
		// click into a Ctrl+click. A release without a press harms
		// nobody.
		return false
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
