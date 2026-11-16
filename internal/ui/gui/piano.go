package gui

// Key is one key of the drawn keyboard.
type Key struct {
	Note       int
	Black      bool
	X, Y, W, H float32
}

// blackKey tells which of the twelve notes of an octave are black keys.
var blackKey = [12]bool{1: true, 3: true, 6: true, 8: true, 10: true}

// IsBlack reports whether a note is a black key.
func IsBlack(note int) bool { return blackKey[((note%12)+12)%12] }

// Piano lays out the keys for the notes first to last inside a rectangle.
// The white keys share the width evenly; the black ones lie over the gaps
// between them. The white keys come first in the result, so that drawing
// in order puts the black ones on top. first and last are moved outwards
// to white keys.
func Piano(first, last int, x, y, w, h float32) []Key {
	if IsBlack(first) {
		first--
	}
	if IsBlack(last) {
		last++
	}
	whites := 0
	for n := first; n <= last; n++ {
		if !IsBlack(n) {
			whites++
		}
	}
	if whites == 0 || w <= 0 {
		return nil
	}
	whiteW := w / float32(whites)
	blackW, blackH := whiteW*0.62, h*0.6

	keys := make([]Key, 0, last-first+1)
	var blacks []Key
	i := 0
	for n := first; n <= last; n++ {
		if IsBlack(n) {
			// Centred on the line between the white keys around it.
			blacks = append(blacks, Key{Note: n, Black: true, X: x + float32(i)*whiteW - blackW/2, Y: y, W: blackW, H: blackH})
			continue
		}
		keys = append(keys, Key{Note: n, X: x + float32(i)*whiteW, Y: y, W: whiteW, H: h})
		i++
	}
	return append(keys, blacks...)
}
