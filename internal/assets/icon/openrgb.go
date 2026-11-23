package icon

import (
	"image"
	"image/color"
	"math"
)

// The sign HIDI uses for OpenRGB, as openrgb.svg in this directory draws
// it: a ring of colour with a slice cut out at the bottom, which with the
// hole in the middle makes a keyhole. The window shows it small, so it is
// drawn here from the same shape instead of being read from the file.
// Measures are those of the file, which is 164 wide and high.
const (
	rgbSize  = 164.0
	wheelX   = 82.0
	wheelY   = 82.0
	wheelR   = 80.0
	holeR    = 27.0
	sliceDeg = 24.0 // half the opening of the slice, seen from the middle
)

// RGBLook is how the sign is coloured.
type RGBLook int

const (
	// RGBColour: the ring in its colours, for lights that HIDI drives.
	RGBColour RGBLook = iota
	// RGBGrey: the ring grey, for a keyboard whose lights HIDI does not
	// drive.
	RGBGrey
	// RGBYellow: the ring yellow, for lights that are only shown.
	RGBYellow
)

// Hue returns the colour of the ring at an angle, in degrees clockwise
// from the left as the sign has it: red, then yellow at 60, green at 120,
// and so on around.
func Hue(angle float64) color.NRGBA {
	h := math.Mod(angle, 360)
	if h < 0 {
		h += 360
	}
	x := uint8((1-math.Abs(math.Mod(h/60, 2)-1))*255 + 0.5)
	switch int(h / 60) {
	case 0:
		return color.NRGBA{255, x, 0, 255}
	case 1:
		return color.NRGBA{x, 255, 0, 255}
	case 2:
		return color.NRGBA{0, 255, x, 255}
	case 3:
		return color.NRGBA{0, x, 255, 255}
	case 4:
		return color.NRGBA{x, 0, 255, 255}
	}
	return color.NRGBA{255, 0, x, 255}
}

// inSign reports whether a point of the picture is part of the ring.
func inSign(x, y float64) bool {
	dx, dy := x-wheelX, y-wheelY
	r := math.Hypot(dx, dy)
	if r > wheelR || r < holeR {
		return false
	}
	// The slice: within so many degrees of straight down.
	return math.Abs(math.Atan2(dx, dy))*180/math.Pi > sliceDeg
}

// rgbAt returns the colour of the sign at a point of the picture; the
// alpha is 0 outside of it.
func rgbAt(x, y float64, look RGBLook) color.NRGBA {
	if !inSign(x, y) {
		return color.NRGBA{}
	}
	switch look {
	case RGBGrey:
		return color.NRGBA{0x6b, 0x72, 0x7e, 255}
	case RGBYellow:
		return color.NRGBA{0xf2, 0xc9, 0x4c, 255}
	}
	return Hue(math.Atan2(-(y-wheelY), -(x-wheelX)) * 180 / math.Pi)
}

// OpenRGB draws the sign size pixels wide and high.
func OpenRGB(size int, look RGBLook) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := rgbSize / float64(size)
	const n = 4
	for py := range size {
		for px := range size {
			var r, g, b, a float64
			for sy := range n {
				for sx := range n {
					c := rgbAt((float64(px)+(float64(sx)+0.5)/n)*scale, (float64(py)+(float64(sy)+0.5)/n)*scale, look)
					if c.A == 0 {
						continue
					}
					r, g, b, a = r+float64(c.R), g+float64(c.G), b+float64(c.B), a+1
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8(r/a + 0.5), uint8(g/a + 0.5), uint8(b/a + 0.5), uint8(a/(n*n)*255 + 0.5)})
		}
	}
	return img
}
