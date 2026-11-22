// Package icon draws HIDI's icon: the first five keys of a keyboard, the
// middle white one in three pieces, of which the middle piece joins the
// keys beside it to an H.
//
// The shapes are those of hidi.svg in this directory, the drawing the
// icon was made as. They are kept here as the paths of that file and
// filled at whatever size is asked for, so that the icon is sharp in the
// notification area, in the window and as the icon of the program file
// alike, and can take the colours of HIDI's state.
package icon

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Part is one of the shapes the icon is made of.
type Part int

const (
	// Back is the rounded square everything lies on.
	Back Part = iota
	// Sharp are the two black keys.
	Sharp
	// H are the two outer white keys and the piece of the middle one
	// that joins them: the letter.
	H
	// Rest is what is left of the middle white key, above and below the
	// bar of the letter.
	Rest
	parts
)

// Colours gives every part its colour.
type Colours [parts]color.NRGBA

// The paths of hidi.svg, in the units of that file. The first is the
// background, which the file moves and scales on its own; all of them are
// moved by the file's layers.
var paths = []struct {
	part Part
	d    string
	// scale and dx, dy are the background's own transformation.
	scale, dx, dy float64
}{
	{Back, "m 86.892578,137.67578 c -0.652835,0 -1.164062,0.52784 -1.164062,1.16602 v 22.70507 c 0,0.64785 0.516215,1.16407 1.164062,1.16407 h 23.687502 c 0.63818,0 1.16601,-0.51123 1.16601,-1.16407 V 138.8418 c 0,-0.64295 -0.52307,-1.16602 -1.16601,-1.16602 z", 0.94715764, -104.85695, -68.57379},
	{Sharp, "m -5.672306,62.680833 h -3.7149806 v 11.851225 c 0,0.585343 0.4713966,1.056734 1.0567381,1.056734 h 1.6019931 c 0.5853415,0 1.0562494,-0.471391 1.0562494,-1.056734 z", 1, 0, 0},
	{Sharp, "m -13.586342,62.680833 h -3.71498 v 11.851225 c 0,0.585343 0.471397,1.056734 1.056738,1.056734 h 1.601994 c 0.585341,0 1.056248,-0.471391 1.056248,-1.056734 z", 1, 0, 0},
	{H, "m -5.1985113,62.680833 v 11.851225 c 0,0.845925 -0.6836309,1.530048 -1.5295544,1.530048 h -0.5472128 v 8.436768 h 7.17495122 V 62.680833 Z", 1, 0, 0},
	{Rest, "m -8.3369115,76.062106 h -6.7753655 v 8.436768 h 7.24965 v -8.436768 z", 1, 0, 0},
	{H, "m -9.8601029,71.925698 h -3.2519541 v 2.60636 c 0,0.843595 -0.680487,1.526297 -1.523191,1.530048 h 6.2983365 c -0.8427041,-0.0038 -1.5231914,-0.686453 -1.5231914,-1.530048 z", 1, 0, 0},
	{Rest, "m -13.112057,62.680833 v 9.244865 h 3.2519541 v -9.244865 z", 1, 0, 0},
	{H, "m -15.776786,76.062106 h -0.467309 c -0.845922,0 -1.530043,-0.684123 -1.530043,-1.530048 V 62.680833 h -5.075182 v 21.818041 h 7.072534 z", 1, 0, 0},
}

// The layers of the file move everything by this much, and the picture is
// this large.
const (
	layerX, layerY = 24.345944, -61.16542
	viewW, viewH   = 26.017581, 25.035157
)

type point struct{ x, y float64 }

// outline is one closed line of a shape, in the units of the picture.
type outline []point

// shapes holds the outlines of every path, worked out once.
var shapes = func() [][]outline {
	out := make([][]outline, len(paths))
	for i, p := range paths {
		for _, o := range parse(p.d) {
			for j := range o {
				o[j] = point{o[j].x*p.scale + p.dx + layerX, o[j].y*p.scale + p.dy + layerY}
			}
			out[i] = append(out[i], o)
		}
	}
	return out
}()

// parse reads the path data of an SVG as far as the icon uses it: moves,
// lines, horizontal and vertical lines, cubic curves and closing, each
// absolute or relative. Curves become short lines.
func parse(d string) []outline {
	fields := strings.FieldsFunc(d, func(r rune) bool { return r == ' ' || r == ',' })
	var (
		out      []outline
		cur      outline
		at       point
		command  byte
		i        int
		number   = func() float64 { v, _ := strconv.ParseFloat(fields[i], 64); i++; return v }
		isLetter = func(s string) bool { return len(s) == 1 && strings.ContainsAny(s, "mMlLhHvVcCzZ") }
	)
	for i < len(fields) {
		if isLetter(fields[i]) {
			command = fields[i][0]
			i++
			if command == 'z' || command == 'Z' {
				if len(cur) > 0 {
					out, at = append(out, cur), cur[0]
					cur = nil
				}
				continue
			}
		}
		relative := command >= 'a'
		base := point{}
		if relative {
			base = at
		}
		switch command {
		case 'm', 'M':
			if len(cur) > 0 {
				out = append(out, cur)
			}
			at = point{base.x + number(), base.y + number()}
			cur = outline{at}
			// What follows a move without a letter are lines.
			command = map[bool]byte{true: 'l', false: 'L'}[relative]
		case 'l', 'L':
			at = point{base.x + number(), base.y + number()}
			cur = append(cur, at)
		case 'h':
			at.x += number()
			cur = append(cur, at)
		case 'H':
			at.x = number()
			cur = append(cur, at)
		case 'v':
			at.y += number()
			cur = append(cur, at)
		case 'V':
			at.y = number()
			cur = append(cur, at)
		case 'c', 'C':
			c1 := point{base.x + number(), base.y + number()}
			c2 := point{base.x + number(), base.y + number()}
			end := point{base.x + number(), base.y + number()}
			const steps = 12
			for s := 1; s <= steps; s++ {
				t := float64(s) / steps
				u := 1 - t
				cur = append(cur, point{
					u*u*u*at.x + 3*u*u*t*c1.x + 3*u*t*t*c2.x + t*t*t*end.x,
					u*u*u*at.y + 3*u*u*t*c1.y + 3*u*t*t*c2.y + t*t*t*end.y,
				})
			}
			at = end
		default:
			i++ // nothing the icon has
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// samples is how many lines are looked at across one pixel, to smooth
// the edges.
const samples = 5

// coverage works out how much of every pixel of a size×size picture the
// outlines cover, from 0 to 1. The picture of the file is fitted into the
// square, in its middle.
func coverage(outlines []outline, size int) []float64 {
	scale := float64(size) / math.Max(viewW, viewH)
	offX, offY := (float64(size)-viewW*scale)/2, (float64(size)-viewH*scale)/2
	cover := make([]float64, size*size)
	var crossings []float64
	for row := 0; row < size*samples; row++ {
		y := (float64(row) + 0.5) / samples
		crossings = crossings[:0]
		for _, o := range outlines {
			for i := range o {
				a, b := o[i], o[(i+1)%len(o)]
				ay, by := a.y*scale+offY, b.y*scale+offY
				if (ay <= y) == (by <= y) {
					continue
				}
				ax, bx := a.x*scale+offX, b.x*scale+offX
				crossings = append(crossings, ax+(y-ay)/(by-ay)*(bx-ax))
			}
		}
		sort.Float64s(crossings)
		for i := 0; i+1 < len(crossings); i += 2 {
			from, to := math.Max(crossings[i], 0), math.Min(crossings[i+1], float64(size))
			for px := int(from); px < size && float64(px) < to; px++ {
				part := math.Min(to, float64(px+1)) - math.Max(from, float64(px))
				if part > 0 {
					cover[(row/samples)*size+px] += part / samples
				}
			}
		}
	}
	return cover
}

// Render draws the icon size pixels wide and high in the given colours.
// What the icon does not cover is clear.
func Render(size int, colours Colours) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// Colour and alpha gather with the alpha multiplied in, part over
	// part in the order of the file.
	acc := make([][4]float64, size*size)
	// The background and the black keys are laid on one after the other.
	// The white keys are laid on as one shape, coloured by how much of
	// each point is of the letter: where two pieces of a key meet, the
	// point is theirs together, and nothing of what lies below shows
	// through between them.
	lay := func(px int, c [3]float64, a float64) {
		acc[px][0] = c[0]*a + acc[px][0]*(1-a)
		acc[px][1] = c[1]*a + acc[px][1]*(1-a)
		acc[px][2] = c[2]*a + acc[px][2]*(1-a)
		acc[px][3] = a + acc[px][3]*(1-a)
	}
	rgb := func(c color.NRGBA) [3]float64 { return [3]float64{float64(c.R), float64(c.G), float64(c.B)} }
	letter, rest := make([]float64, size*size), make([]float64, size*size)
	for i, p := range paths {
		cover := coverage(shapes[i], size)
		switch p.part {
		case H:
			for px, part := range cover {
				letter[px] += part
			}
		case Rest:
			for px, part := range cover {
				rest[px] += part
			}
		default:
			c := colours[p.part]
			for px, part := range cover {
				if part > 0 {
					lay(px, rgb(c), math.Min(part, 1)*float64(c.A)/255)
				}
			}
		}
	}
	h, r := colours[H], colours[Rest]
	for px := range letter {
		all := letter[px] + rest[px]
		if all <= 0 {
			continue
		}
		share := letter[px] / all
		mixed := [3]float64{}
		for i, v := range rgb(h) {
			mixed[i] = v*share + rgb(r)[i]*(1-share)
		}
		alpha := (float64(h.A)*share + float64(r.A)*(1-share)) / 255
		lay(px, mixed, math.Min(all, 1)*alpha)
	}
	for px, v := range acc {
		if v[3] <= 0 {
			continue
		}
		img.Pix[4*px+0] = uint8(v[0]/v[3] + 0.5)
		img.Pix[4*px+1] = uint8(v[1]/v[3] + 0.5)
		img.Pix[4*px+2] = uint8(v[2]/v[3] + 0.5)
		img.Pix[4*px+3] = uint8(v[3]*255 + 0.5)
	}
	return img
}

// The colours of the icon by HIDI's state.
var (
	green  = color.NRGBA{0x2f, 0xbf, 0x6b, 0xff}
	yellow = color.NRGBA{0xe6, 0xb9, 0x22, 0xff}
	dark   = color.NRGBA{0x16, 0x19, 0x1e, 0xff}

	// On: the letter green, the rest of the middle key a lighter green.
	On = Colours{Back: {0x3a, 0x40, 0x4a, 0xff}, Sharp: dark, H: green, Rest: {0xa6, 0xe6, 0xc1, 0xff}}
	// Learning: the other half of the blinking of an icon whose HIDI is
	// on and in learning mode.
	Learning = Colours{Back: {0x3a, 0x40, 0x4a, 0xff}, Sharp: dark, H: yellow, Rest: {0xf3, 0xdf, 0x96, 0xff}}
	// Off: grey, and no letter to be seen.
	Off = Colours{Back: {0x5c, 0x63, 0x6e, 0xff}, Sharp: {0x2a, 0x2e, 0x35, 0xff}, H: {0xc9, 0xcd, 0xd3, 0xff}, Rest: {0xc9, 0xcd, 0xd3, 0xff}}
	// Program is the icon of the program file: as the drawing is, with
	// the letter green.
	Program = Colours{Back: {0x2b, 0x30, 0x38, 0xff}, Sharp: {0x0c, 0x0e, 0x11, 0xff}, H: green, Rest: {0xa6, 0xe6, 0xc1, 0xff}}
)

// Fade makes a drawn icon fainter, to a part of its strength, for a HIDI
// that is not running.
func Fade(img *image.NRGBA, strength float64) *image.NRGBA {
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(float64(img.Pix[i])*strength + 0.5)
	}
	return img
}
