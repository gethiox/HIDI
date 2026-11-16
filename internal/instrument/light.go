package instrument

import (
	"sync"

	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/midi"
)

// Light is the lights of one instrument's device; *light.Lamp is one.
type Light interface {
	// Show makes the lights show a scene. It must return at once.
	Show(light.Scene)
	// Close ends the use of the lights.
	Close()
}

// external follows the notes other programs send to an instrument: which
// are on, by channel.
type external struct {
	mu    sync.Mutex
	notes [16][2]uint64
}

// take records what a message means for the notes that are on, and
// reports whether anything changed.
func (x *external) take(m midi.Message) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	ch := m.Channel & 0x0f
	before := x.notes[ch]
	switch m.Kind {
	case midi.KindNoteOn:
		n := m.Note()
		if m.Value == 0 {
			x.notes[ch][n/64] &^= 1 << (n % 64) // a note-on without force ends the note
		} else {
			x.notes[ch][n/64] |= 1 << (n % 64)
		}
	case midi.KindNoteOff:
		n := m.Note()
		x.notes[ch][n/64] &^= 1 << (n % 64)
	case midi.KindControlChange:
		// All sound off and all notes off.
		if m.Index == 120 || m.Index == 123 {
			x.notes[ch] = [2]uint64{}
		}
	}
	return x.notes[ch] != before
}

func (x *external) snapshot() [16][2]uint64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.notes
}

func (x *external) clear() {
	x.mu.Lock()
	x.notes = [16][2]uint64{}
	x.mu.Unlock()
}
