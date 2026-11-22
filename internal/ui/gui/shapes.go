package gui

import "math"

// The gear that opens the settings is made the way one is drawn with a
// pair of compasses: a disc, teeth laid on it whose sides slope towards
// their tops, the whole cut to a circle that rounds those tops, and a
// hole taken out of the middle. Measures are parts of the radius.
const (
	gearTeeth    = 8
	gearTip      = 0.97 // the circle the tops of the teeth are cut to
	gearRoot     = 0.70 // the disc the teeth stand on
	gearHole     = 0.32
	gearFootHalf = 0.215 // half the width of a tooth where it stands
	gearTopHalf  = 0.115 // and at its top
)

// inGear reports whether a point is part of the gear; the middle of the
// gear is 0, 0.
func inGear(x, y float64) bool {
	r := math.Hypot(x, y)
	if r > gearTip || r < gearHole {
		return false
	}
	if r <= gearRoot {
		return true
	}
	// The nearest tooth: the angle from its middle line.
	const step = 2 * math.Pi / gearTeeth
	a := math.Atan2(y, x)
	a -= math.Round(a/step) * step
	along, across := r*math.Cos(a), math.Abs(r*math.Sin(a))
	rise := (along - gearRoot) / (gearTip - gearRoot)
	half := gearFootHalf + (gearTopHalf-gearFootHalf)*min(max(rise, 0), 1)
	return across <= half
}

// GearAlpha draws the gear size pixels wide and high and returns how much
// of every pixel it covers, from 0 to 255, row after row.
func GearAlpha(size int) []uint8 {
	const samples = 4
	out := make([]uint8, size*size)
	for py := range size {
		for px := range size {
			hit := 0
			for sy := range samples {
				for sx := range samples {
					x := (float64(px)+(float64(sx)+0.5)/samples)/float64(size)*2 - 1
					y := (float64(py)+(float64(sy)+0.5)/samples)/float64(size)*2 - 1
					if inGear(x, y) {
						hit++
					}
				}
			}
			out[py*size+px] = uint8(hit * 255 / (samples * samples))
		}
	}
	return out
}

// floppy is the picture of a floppy disk for the buttons that save: the
// icon raygui calls ICON_FILE_SAVE_CLASSIC, sixteen by sixteen points, a
// bit for each, two rows to a number. raygui is by Ramon Santamaria and
// under the zlib licence.
var floppy = [8]uint32{0x3ffe0000, 0x44226422, 0x400247e2, 0x5ffa4002, 0x57ea500a, 0x500a500a, 0x40025ffa, 0x00007ffe}

// FloppyPoint reports whether the point in column x of row y of the
// picture is drawn.
func FloppyPoint(x, y int) bool {
	if x < 0 || x > 15 || y < 0 || y > 15 {
		return false
	}
	return floppy[y/2]>>(uint(y%2)*16+uint(x))&1 == 1
}

// CutEnd shortens a text to at most most characters by cutting off its
// end, and CutStart by cutting off its start, which suits a path: its end
// says more. What is cut is marked.
func CutEnd(s string, most int) string {
	r := []rune(s)
	if most < 1 || len(r) <= most {
		return s
	}
	return string(r[:most-1]) + "…"
}

func CutStart(s string, most int) string {
	r := []rune(s)
	if most < 1 || len(r) <= most {
		return s
	}
	return "…" + string(r[len(r)-most+1:])
}
