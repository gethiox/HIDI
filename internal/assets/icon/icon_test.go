package icon

import (
	"image/color"
	"testing"
)

func TestRender(t *testing.T) {
	for _, size := range []int{16, 32, 256} {
		img := Render(size, On)
		at := func(x, y float64) color.NRGBA { return img.NRGBAAt(int(x*float64(size)), int(y*float64(size))) }
		// The corners are clear, the square is rounded.
		if at(0, 0).A > 200 || at(0.999, 0.999).A > 200 {
			t.Errorf("size %d: the corners are not clear", size)
		}
		// Left and right: the outer keys, which are of the letter. Low in
		// the middle: what is left of the middle key. High between them:
		// a black key.
		if got := at(0.15, 0.6); got != On[H] {
			t.Errorf("size %d: the left key is %v", size, got)
		}
		if got := at(0.85, 0.6); got != On[H] {
			t.Errorf("size %d: the right key is %v", size, got)
		}
		if got := at(0.5, 0.82); got != On[Rest] {
			t.Errorf("size %d: the lower middle is %v", size, got)
		}
		if size >= 32 {
			if got := at(0.36, 0.3); got != On[Sharp] {
				t.Errorf("size %d: the left black key is %v", size, got)
			}
			// The bar of the letter, between the two black keys' feet.
			if got := at(0.5, 0.53); got != On[H] {
				t.Errorf("size %d: the bar of the letter is %v", size, got)
			}
		}
	}
	// Off there is no letter: the keys are all of one colour.
	if Off[H] != Off[Rest] {
		t.Error("the letter shows while HIDI is off")
	}
	faint := Fade(Render(32, Off), 0.45)
	if a := faint.NRGBAAt(5, 20).A; a == 0 || a > 140 {
		t.Errorf("a faint icon has alpha %d", a)
	}
}

func TestParse(t *testing.T) {
	// A square, relative and absolute, with one curve in it.
	got := parse("m 1,1 h 2 V 3 c 0,0 -1,0 -2,0 z M 10,10 L 11,10 11,11 z")
	if len(got) != 2 || len(got[1]) != 3 {
		t.Fatalf("outlines: %v", got)
	}
	first := got[0]
	if first[0] != (point{1, 1}) || first[1] != (point{3, 1}) || first[2] != (point{3, 3}) || first[len(first)-1] != (point{1, 3}) {
		t.Errorf("the first outline: %v", first)
	}
}

func TestOpenRGB(t *testing.T) {
	// Around the wheel: red at the left, then yellow, green, cyan, blue
	// and magenta, clockwise.
	for angle, want := range map[float64]color.NRGBA{
		0: {255, 0, 0, 255}, 60: {255, 255, 0, 255}, 120: {0, 255, 0, 255},
		180: {0, 255, 255, 255}, 240: {0, 0, 255, 255}, 300: {255, 0, 255, 255}, 360: {255, 0, 0, 255},
	} {
		if got := Hue(angle); got != want {
			t.Errorf("Hue(%v) = %v", angle, got)
		}
	}
	img := OpenRGB(218, RGBColour)
	if img.Bounds().Dx() != 165 {
		t.Fatalf("the logo is %v", img.Bounds())
	}
	if got := img.NRGBAAt(20, 82); got.R != 255 || got.G > 5 || got.B > 5 {
		t.Errorf("the left of the wheel is %v", got)
	}
	if got := img.NRGBAAt(145, 82); got.R != 0 || got.G < 250 || got.B < 250 {
		t.Errorf("the right of the wheel is %v", got)
	}
	if got := img.NRGBAAt(82, 82); got != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("the keyhole is %v", got)
	}
	if got := img.NRGBAAt(82, 140); got != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("the lower part of the keyhole is %v", got)
	}
	if got := img.NRGBAAt(82, 190); got.A != 255 || got.R < 150 {
		t.Errorf("the base is %v", got)
	}
	if img.NRGBAAt(2, 2).A != 0 || img.NRGBAAt(20, 200).A != 0 {
		t.Error("the logo is drawn outside of its shape")
	}
	// Grey has no colour in it, yellow one.
	grey := OpenRGB(218, RGBGrey).NRGBAAt(20, 82)
	if grey.R > grey.B || grey.B-grey.R > 40 {
		t.Errorf("the grey wheel is %v", grey)
	}
	if got := OpenRGB(218, RGBYellow).NRGBAAt(20, 82); got.R < 200 || got.B > 120 {
		t.Errorf("the yellow wheel is %v", got)
	}
	if small := OpenRGB(20, RGBColour); small.Bounds().Dx() != 16 || small.NRGBAAt(2, 7).A == 0 {
		t.Errorf("the small logo: %v", small.Bounds())
	}
}
