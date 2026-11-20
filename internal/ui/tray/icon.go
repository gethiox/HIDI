package tray

import (
	"image"
	"image/color"
)

// The icon is a small piano keyboard on a rounded square. The square is
// green while HIDI is on and grey while it is off; for a HIDI that is not
// running, the whole icon is faint.

// rect is a rectangle with rounded corners, in units of the icon's size.
type rect struct {
	x0, y0, x1, y1 float64
	radius         float64
}

// contains reports whether the point is inside.
func (r rect) contains(x, y float64) bool {
	if x < r.x0 || x >= r.x1 || y < r.y0 || y >= r.y1 {
		return false
	}
	// Inside a corner square, the point has to be within the circle.
	cx, cy := x, y
	if x < r.x0+r.radius {
		cx = r.x0 + r.radius
	} else if x > r.x1-r.radius {
		cx = r.x1 - r.radius
	}
	if y < r.y0+r.radius {
		cy = r.y0 + r.radius
	} else if y > r.y1-r.radius {
		cy = r.y1 - r.radius
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r.radius*r.radius
}

var (
	iconBack  = rect{0.04, 0.04, 0.96, 0.96, 0.22}
	iconWhite = []rect{
		{0.19, 0.24, 0.38, 0.76, 0.04},
		{0.405, 0.24, 0.595, 0.76, 0.04},
		{0.62, 0.24, 0.81, 0.76, 0.04},
	}
	// The black keys are cut out of the white ones.
	iconBlack = []rect{
		{0.33, 0.20, 0.455, 0.54, 0.03},
		{0.545, 0.20, 0.67, 0.54, 0.03},
	}

	colourOn    = color.NRGBA{R: 0x21, G: 0xa3, B: 0x5a, A: 0xff}
	colourOff   = color.NRGBA{R: 0x5c, G: 0x63, B: 0x6e, A: 0xff}
	colourLearn = color.NRGBA{R: 0xe0, G: 0xb4, B: 0x1c, A: 0xff}
	colourKeys  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// iconSamples is the number of points looked at across one pixel, in each
// direction, to smooth the edges.
const iconSamples = 4

// Icon draws the icon for a look, size pixels wide and high.
func Icon(look Look, size int) *image.NRGBA {
	back, strength := colourOff, 1.0
	switch look {
	case LookOn:
		back = colourOn
	case LookLearning:
		back = colourLearn
	case LookStopped:
		strength = 0.45
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			var r, g, b, a float64
			for sy := range iconSamples {
				for sx := range iconSamples {
					x := (float64(px) + (float64(sx)+0.5)/iconSamples) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/iconSamples) / float64(size)
					c, ok := iconColour(x, y, back)
					if !ok {
						continue
					}
					r, g, b, a = r+float64(c.R), g+float64(c.G), b+float64(c.B), a+1
				}
			}
			if a == 0 {
				continue
			}
			total := float64(iconSamples * iconSamples)
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(r/a + 0.5), G: uint8(g/a + 0.5), B: uint8(b/a + 0.5),
				A: uint8(a/total*255*strength + 0.5),
			})
		}
	}
	return img
}

// iconColour returns the colour of the icon at a point, or false outside
// of it.
func iconColour(x, y float64, back color.NRGBA) (color.NRGBA, bool) {
	if !iconBack.contains(x, y) {
		return color.NRGBA{}, false
	}
	for _, key := range iconBlack {
		if key.contains(x, y) {
			return back, true
		}
	}
	for _, key := range iconWhite {
		if key.contains(x, y) {
			return colourKeys, true
		}
	}
	return back, true
}
