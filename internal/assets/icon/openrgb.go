package icon

import (
	"image"
	"image/color"
	"math"
)

// The logo of OpenRGB, as openrgb.svg in this directory draws it: a colour
// wheel as the glass of a light bulb, a keyhole in it, and the base of the
// bulb. The window shows it small, so it is drawn here from the same few
// shapes instead of being read from the file. Measures are those of the
// file, which is 165 wide and 218 high.
const (
	rgbW, rgbH = 165.0, 218.0
	wheelX     = 82.0
	wheelY     = 82.0
	wheelR     = 80.5 // the middle of the rim
	rimHalf    = 1.5  // half the width of the black lines
	holeY      = 81.8
	holeR      = 27.5
)

// RGBLook is how the logo is coloured.
type RGBLook int

const (
	// RGBColour: the wheel in its colours, for lights that HIDI drives.
	RGBColour RGBLook = iota
	// RGBGrey: all of it grey, for a keyboard whose lights HIDI does
	// not drive.
	RGBGrey
	// RGBYellow: the wheel yellow, for lights that are only shown.
	RGBYellow
)

// Hue returns the colour of the wheel at an angle, in degrees clockwise
// from the left as the logo has it: red, then yellow at 60, green at 120,
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

// roundRect reports whether a point is in a rectangle with round corners.
func roundRect(x, y, x0, y0, w, h, r float64) bool {
	if x < x0 || x > x0+w || y < y0 || y > y0+h {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x0+w-r)
	cy := math.Min(math.Max(y, y0+r), y0+h-r)
	return math.Hypot(x-cx, y-cy) <= r
}

// keyhole reports how far inside the keyhole a point is: positive inside,
// negative outside, roughly in units of the picture near the edge.
func keyhole(x, y float64) float64 {
	inCircle := holeR - math.Hypot(x-wheelX, y-holeY)
	// The part that widens downwards, from where it leaves the circle
	// to the base: between two lines.
	const topY, botY, topHalf, botHalf = 107.2, 159.0, 10.5, 25.0
	if y < topY || y > botY {
		return inCircle
	}
	half := topHalf + (botHalf-topHalf)*(y-topY)/(botY-topY)
	// The lines lean; the distance to them is a little less than the
	// horizontal one.
	inFlare := (half - math.Abs(x-wheelX)) * 0.96
	return math.Max(inCircle, inFlare)
}

// rgbAt returns the colour of the logo at a point of the picture; the
// alpha is 0 outside of it.
func rgbAt(x, y float64, look RGBLook) color.NRGBA {
	black := color.NRGBA{0, 0, 0, 255}
	grey := color.NRGBA{0x8b, 0x94, 0xa3, 255}
	if look == RGBGrey {
		// One tone for the lines, so that the shape reads on a dark
		// ground.
		black = color.NRGBA{0x3a, 0x40, 0x4a, 255}
		grey = color.NRGBA{0x6b, 0x72, 0x7e, 255}
	}
	// The base lies over everything.
	if roundRect(x, y, 57, 158, 50, 50, 0) || roundRect(x, y, 49, 157, 66, 14, 5) || roundRect(x, y, 51, 183, 62, 12, 5) ||
		roundRect(x, y, 53, 198, 58, 10, 5) || roundRect(x, y, 67, 203, 30, 14, 7) {
		if look == RGBGrey {
			return grey
		}
		// Black in the drawing; lighter here, where the ground is dark.
		return color.NRGBA{0xb9, 0xc0, 0xcc, 255}
	}
	if d := keyhole(x, y); d > -rimHalf {
		if d < rimHalf {
			return black
		}
		if look == RGBGrey {
			return color.NRGBA{}
		}
		return color.NRGBA{255, 255, 255, 255}
	}
	r := math.Hypot(x-wheelX, y-wheelY)
	switch {
	case r > wheelR+rimHalf:
		return color.NRGBA{}
	case r > wheelR-rimHalf:
		return black
	}
	switch look {
	case RGBGrey:
		return grey
	case RGBYellow:
		return color.NRGBA{0xf2, 0xc9, 0x4c, 255}
	}
	return Hue(math.Atan2(-(y-wheelY), -(x-wheelX)) * 180 / math.Pi)
}

// OpenRGB draws the logo height pixels high, and as wide as its shape
// asks for.
func OpenRGB(height int, look RGBLook) *image.NRGBA {
	width := int(math.Ceil(float64(height) * rgbW / rgbH))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	scale := rgbH / float64(height)
	const n = 4
	for py := range height {
		for px := range width {
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
