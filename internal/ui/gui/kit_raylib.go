//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"math"
	"runtime"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/assets/fonts"
)

// kit is what the window is drawn with: the fonts, the size of the
// interface, the pointer and what it may touch, and the widgets made of
// those. The window is drawn anew for every frame, from top to bottom, by
// one goroutine: a widget is a method that draws itself where it is told
// and reports what the pointer did to it in this frame. Nothing is kept
// between frames but what a widget's caller keeps.
//
// Everything is laid out in the window's own points, which are the
// pixels of an ordinary display at the ordinary size of the interface.
type kit struct {
	sans, bold, title, mono rl.Font

	// scale is the size everything is drawn at, as a factor, and
	// wantScale the one to draw at from the next frame on. The fonts are
	// made for the size they end up at, so that text stays sharp; they
	// can only be made anew between two frames.
	scale, wantScale float32

	// cursor is the cursor the frame being drawn asks for, and shown the
	// one that was last set. The cursor is set once a frame, when all of
	// it is drawn, and not by every part that has a wish: a cursor that
	// is set and set again flickers, and each setting makes a new one on
	// some systems.
	cursor, shown int32

	// deaf makes what is drawn deaf to the pointer: it is set while a
	// part of the window is drawn that lies under another.
	deaf bool

	// moving says that something drawn in this frame is on its way
	// somewhere, a caret that blinks for one, so that another frame has
	// to follow though nothing else changed.
	moving bool

	// blocked makes everything deaf while a question waits for its
	// answer; see confirm. asking is that question.
	blocked bool
	asking  *question
	hints   hints
	// wide and high are the size of the window in its own points, as of
	// the frame being drawn.
	wide, high float32

	// clips are the parts of the window that drawing, and the pointer,
	// are held to, each inside the one before; the last is in force.
	clips []rl.Rectangle
}

// The sizes of the fonts, in points.
const (
	sizeTitle = 24
	sizeName  = 17
	sizeText  = 14
	sizeMono  = 13
)

// glyphs returns the characters the fonts are made with.
func glyphs() []rune {
	var out []rune
	for r := rune(0x20); r <= 0x17f; r++ {
		if r >= 0x7f && r < 0xa0 {
			continue
		}
		out = append(out, r)
	}
	return append(out, []rune("–—−‘’“”…·•←→↑↓▲▼♯♭")...)
}

// loadFonts prepares the fonts at the sizes used, in the pixels of the
// display and at the size of the interface.
func (k *kit) loadFonts() {
	scale := rl.GetWindowScaleDPI().X
	if scale < 1 {
		scale = 1
	}
	scale *= k.scale
	load := func(data []byte, size float32) rl.Font {
		font := rl.LoadFontFromMemory(".ttf", data, int32(size*scale+0.5), glyphs())
		rl.SetTextureFilter(font.Texture, rl.FilterBilinear)
		return font
	}
	k.sans = load(fonts.Sans, sizeText)
	k.bold = load(fonts.Bold, sizeName)
	k.title = load(fonts.Bold, sizeTitle)
	k.mono = load(fonts.Mono, sizeMono)
}

func (k *kit) unloadFonts() {
	for _, f := range []rl.Font{k.sans, k.bold, k.title, k.mono} {
		rl.UnloadFont(f)
	}
}

// begin starts a frame and returns the size of the window in its own
// points. A new size of the interface is taken up here, between two
// frames.
func (k *kit) begin() (wide, high float32) {
	if k.wantScale != k.scale {
		k.unloadFonts()
		k.scale = k.wantScale
		k.loadFonts()
	}
	rl.BeginDrawing()
	// The size of the interface is laid over the scaling of the display,
	// which BeginDrawing has set up. raylib's camera mode would not do:
	// it throws the scaling of the display away, and on a dense display
	// everything was drawn small in a corner while the pointer was still
	// measured in the window's own points.
	rl.Scalef(k.scale, k.scale, 1)
	// Whatever lies under the pointer may ask for another cursor.
	k.cursor = rl.MouseCursorDefault
	k.deaf, k.moving, k.clips = false, false, k.clips[:0]
	k.blocked = k.asking != nil
	k.wide, k.high = float32(rl.GetScreenWidth())/k.scale, float32(rl.GetScreenHeight())/k.scale
	return k.wide, k.high
}

// end ends a frame: what lies over everything is drawn, and the frame
// shown.
func (k *kit) end() {
	k.drawQuestion()
	k.drawHint(time.Now())
	rl.EndDrawing()
	k.applyCursor()
}

// setCursor asks for a cursor for the frame being drawn.
func (k *kit) setCursor(c int32) { k.cursor = c }

// applyCursor makes the pointer look as the last frame asked. On Windows
// the system gives the pointer its arrow back whenever it moves, so there
// the wish is repeated after every look at the pointer, as long as it is
// over the window.
func (k *kit) applyCursor() {
	again := runtime.GOOS == "windows" && k.cursor != rl.MouseCursorDefault && rl.IsCursorOnScreen()
	if k.cursor != k.shown || again {
		rl.SetMouseCursor(k.cursor)
		k.shown = k.cursor
	}
}

// text writes s with its upper left corner at x, y.
func (k *kit) text(font rl.Font, s string, x, y, size float32, c rl.Color) {
	// Whole pixels keep the letters sharp: those of the screen, which
	// the size of the interface makes other than the window's own.
	x, y = float32(int(x*k.scale+0.5))/k.scale, float32(int(y*k.scale+0.5))/k.scale
	rl.DrawTextEx(font, s, rl.NewVector2(x, y), size, 0, c)
}

// width returns how wide a text is.
func width(font rl.Font, s string, size float32) float32 {
	return rl.MeasureTextEx(font, s, size, 0).X
}

// fit cuts a text off at its end so that it is no wider than room.
func fit(font rl.Font, s string, size, room float32) string {
	if width(font, s, size) <= room {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && width(font, string(r)+"…", size) > room {
		r = r[:len(r)-1]
	}
	if len(r) == 0 {
		return ""
	}
	return string(r) + "…"
}

// hair is the width of a thin line: one of the window's points, and not
// less than a pixel where everything is drawn smaller, since a line
// thinner than that breaks up.
func (k *kit) hair() float32 { return max(1, 1/k.scale) }

// mouse returns where the pointer is, in the window's points.
func (k *kit) mouse() rl.Vector2 {
	p := rl.GetMousePosition()
	return rl.NewVector2(p.X/k.scale, p.Y/k.scale)
}

// dragged returns how far the pointer moved since the last frame.
func (k *kit) dragged() rl.Vector2 {
	d := rl.GetMouseDelta()
	return rl.NewVector2(d.X/k.scale, d.Y/k.scale)
}

// hovered reports whether the pointer is on r and may touch it.
func (k *kit) hovered(r rl.Rectangle) bool {
	// A pointer that left the window in a hurry is last seen inside it;
	// nothing is under a pointer that is not over the window.
	if k.deaf || k.blocked || !rl.IsCursorOnScreen() {
		return false
	}
	p := k.mouse()
	// What is cut off from view is not there for the pointer either: a
	// button scrolled under the log or the header must not take a click
	// meant for what covers it.
	if n := len(k.clips); n > 0 && !rl.CheckCollisionPointRec(p, k.clips[n-1]) {
		return false
	}
	return rl.CheckCollisionPointRec(p, r)
}

// clicked reports whether the left button went down on r in this frame.
func (k *kit) clicked(r rl.Rectangle) bool {
	return rl.IsMouseButtonPressed(rl.MouseButtonLeft) && k.hovered(r)
}

// wheel returns how far the mouse wheel was turned over r.
func (k *kit) wheel(r rl.Rectangle) float32 {
	if !k.hovered(r) {
		return 0
	}
	return rl.GetMouseWheelMove()
}

// ctrlHeld reports whether a Ctrl key is held.
func ctrlHeld() bool {
	return rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)
}

// clip holds drawing, and the pointer, to the part of r that the clip in
// force leaves, until unclip.
func (k *kit) clip(r rl.Rectangle) {
	if n := len(k.clips); n > 0 {
		r = rl.GetCollisionRec(r, k.clips[n-1])
		rl.EndScissorMode()
	}
	k.clips = append(k.clips, r)
	k.scissor(r)
}

// unclip ends the last clip and puts the one before it back in force.
func (k *kit) unclip() {
	rl.EndScissorMode()
	k.clips = k.clips[:len(k.clips)-1]
	if n := len(k.clips); n > 0 {
		k.scissor(k.clips[n-1])
	}
}

// scissor tells raylib to draw inside r only. raylib counts in the points
// of the window before the size of the interface is laid on.
//
// Each edge is brought to a whole pixel by itself, and the size is what
// lies between them. Rounding the corner and the size apart lets the far
// edge move by a pixel as the near one moves, which shows as a tremble
// where the far edge is meant to stand still: a bar that fills from its
// middle outwards, for one.
func (k *kit) scissor(r rl.Rectangle) {
	edge := func(v float32) int32 { return int32(math.Round(float64(v * k.scale))) }
	left, top := edge(r.X), edge(r.Y)
	right, bottom := edge(r.X+max(r.Width, 0)), edge(r.Y+max(r.Height, 0))
	rl.BeginScissorMode(left, top, right-left, bottom-top)
}
