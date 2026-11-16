//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/assets/fonts"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
)

// Measures of the window, in pixels before scaling for a dense display.
const (
	windowW, windowH = 920, 780
	minW, minH       = 600, 380
	margin           = 16
	headerH          = 64
	barH             = 30 // the status bar with the last log line
	filterH          = 34 // the strip with the level filter, when the log is open
	cardTop          = 70 // name and numbers of an instrument
	pianoH           = 44
	cardGap          = 12
	keyUnit          = 24 // the most an ordinary key of the drawn keyboard measures
	chipH            = 20
	axisRowH         = 20
	lineH            = 18 // of the log

	sizeTitle = 24
	sizeName  = 17
	sizeText  = 14
	sizeMono  = 13

	// The keyboard shows every MIDI note.
	firstNote, lastNote = 0, 127

	// screenshotFrames is how many frames are drawn before the picture
	// is taken, so that HIDI's state has arrived.
	screenshotDelay = 1500 * time.Millisecond
)

// The colours of the window.
var (
	colBackground = RGB{0x1b, 0x1f, 0x26}
	colPanel      = RGB{0x25, 0x2a, 0x33}
	colPanelEdge  = RGB{0x33, 0x3a, 0x46}
	colBar        = RGB{0x14, 0x17, 0x1c}
	colFaint      = RGB{0x8b, 0x94, 0xa3}
	colOn         = RGB{0x21, 0xa3, 0x5a}
	colOff        = RGB{0x5c, 0x63, 0x6e}
	colAccent     = RGB{0x3d, 0xc9, 0x7b}
	colWhiteKey   = RGB{0xdd, 0xe1, 0xe7}
	colBlackKey   = RGB{0x16, 0x19, 0x1e}
)

type window struct {
	hidi  Controller
	model *Model
	opts  Options
	ctx   context.Context

	sans, bold, title, mono rl.Font

	logHeight   float32 // of the open log, 0 when only the bar shows
	savedHeight float32 // what a click on the bar opens the log to
	dragging    bool
	dragMoved   bool
	logScroll   float32 // lines scrolled back from the newest
	cardScroll  float32
	toggling    chan struct{}

	gear     gear
	settings settingsView
	edit     *editor // the profile being changed, if any
	pace     *pacer
}

func col(c RGB) rl.Color               { return rl.NewColor(c.R, c.G, c.B, 255) }
func colA(c RGB, alpha uint8) rl.Color { return rl.NewColor(c.R, c.G, c.B, alpha) }

// Run opens the window and returns when it is closed or ctx is cancelled.
// It must be called from the program's main goroutine.
func Run(ctx context.Context, hidi Controller, opts Options) error {
	// The windowing system wants all its calls from one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := &Model{}
	model.SetMinLevel(1) // from INFO up
	go Follow(ctx, hidi, model)

	rl.SetTraceLogLevel(rl.LogError)
	// Drawing waits for the monitor, whatever its refresh rate is. The
	// library that opens the window does not keep the system or the
	// display awake.
	rl.SetConfigFlags(rl.FlagVsyncHint | rl.FlagWindowResizable | rl.FlagWindowHighdpi | rl.FlagMsaa4xHint)
	rl.InitWindow(windowW, windowH, "HIDI")
	if !rl.IsWindowReady() {
		return fmt.Errorf("the window could not be opened; HIDI needs a display with OpenGL 3.3")
	}
	defer rl.CloseWindow()
	rl.SetWindowMinSize(minW, minH)
	rl.SetExitKey(0) // Escape is a key of the instrument, not a way out

	w := &window{hidi: hidi, model: model, opts: opts, ctx: ctx, toggling: make(chan struct{}, 1)}
	w.loadFonts()
	defer w.unloadFonts()
	w.savedHeight = 220
	if opts.LogHeight > 0 {
		w.logHeight, w.savedHeight = float32(opts.LogHeight), float32(opts.LogHeight)
	}

	pace := newPacer(opts.BackgroundFPS)
	w.pace = pace
	if opts.ShowSettings {
		w.gear.set(true, time.Now())
	}
	if opts.EditProfile != "" {
		w.editProfile(opts.EditProfile)
	}
	started := time.Now()
	var seen uint64
	settle := 0
	focused := true
	for !rl.WindowShouldClose() && ctx.Err() == nil {
		pace.frame()
		// The window is drawn anew only when something it shows may have
		// changed: HIDI's state or log, or something the user did. A
		// window that just sits there costs next to nothing. A few more
		// frames follow every change, since the picture on screen
		// alternates between two buffers.
		version := model.Version()
		// While the settings are open the gear turns, and the page follows
		// the files on disk.
		if version != seen || w.gear.open || w.dragging || userActive() || rl.IsWindowResized() || rl.IsWindowFocused() != focused {
			seen, focused, settle = version, rl.IsWindowFocused(), 3
		}
		shoot := opts.Screenshot != "" && time.Since(started) > screenshotDelay
		if shoot {
			settle = 1 // the picture is taken of a frame drawn now
		}
		if settle == 0 {
			rl.PollInputEvents()
			rl.WaitTime(1 / float64(pace.target))
			continue
		}
		settle--
		rl.BeginDrawing()
		w.draw()
		rl.EndDrawing()
		if shoot {
			rl.TakeScreenshot(opts.Screenshot)
			break
		}
	}
	return nil
}

// userActive reports whether the user is doing something with the window.
func userActive() bool {
	if d := rl.GetMouseDelta(); d.X != 0 || d.Y != 0 || rl.GetMouseWheelMove() != 0 {
		return true
	}
	for _, button := range []rl.MouseButton{rl.MouseButtonLeft, rl.MouseButtonRight, rl.MouseButtonMiddle} {
		if rl.IsMouseButtonDown(button) || rl.IsMouseButtonReleased(button) {
			return true
		}
	}
	return false
}

// pacer decides how fast the window is drawn. In front it follows the
// monitor it is on, whatever that monitor's refresh rate is: drawing waits
// for the monitor, and where the system does not make it wait, as on a
// display without one, the same rate is kept by sleeping. Behind another
// window the rate may be held lower.
type pacer struct {
	background int
	target     int32
	checked    time.Time
	refresh    int32
}

func newPacer(background int) *pacer { return &pacer{background: background} }

func (p *pacer) frame() {
	// The window may have been moved to another monitor.
	if now := time.Now(); now.Sub(p.checked) > time.Second {
		p.checked = now
		p.refresh = int32(rl.GetMonitorRefreshRate(rl.GetCurrentMonitor()))
		if p.refresh <= 0 {
			p.refresh = 60
		}
	}
	// A little above the monitor's rate, so that the sleeping never
	// makes a frame miss the monitor it would have caught.
	want := p.refresh + p.refresh/20 + 1
	if p.background > 0 && !rl.IsWindowFocused() && int32(p.background) < want {
		want = int32(p.background)
	}
	if want != p.target {
		p.target = want
		rl.SetTargetFPS(want)
	}
}

// glyphs are the characters the fonts are prepared for: Latin with its
// accented letters, and the signs the window uses.
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
// display, so that text is sharp on a dense one.
func (w *window) loadFonts() {
	scale := rl.GetWindowScaleDPI().X
	if scale < 1 {
		scale = 1
	}
	load := func(data []byte, size float32) rl.Font {
		font := rl.LoadFontFromMemory(".ttf", data, int32(size*scale+0.5), glyphs())
		rl.SetTextureFilter(font.Texture, rl.FilterBilinear)
		return font
	}
	w.sans = load(fonts.Sans, sizeText)
	w.bold = load(fonts.Bold, sizeName)
	w.title = load(fonts.Bold, sizeTitle)
	w.mono = load(fonts.Mono, sizeMono)
}

func (w *window) unloadFonts() {
	for _, f := range []rl.Font{w.sans, w.bold, w.title, w.mono} {
		rl.UnloadFont(f)
	}
}

func text(font rl.Font, s string, x, y, size float32, c rl.Color) {
	// Whole pixels keep the letters sharp.
	rl.DrawTextEx(font, s, rl.NewVector2(float32(int(x)), float32(int(y))), size, 0, c)
}

func width(font rl.Font, s string, size float32) float32 {
	return rl.MeasureTextEx(font, s, size, 0).X
}

func clicked(r rl.Rectangle) bool {
	return rl.IsMouseButtonPressed(rl.MouseButtonLeft) && rl.CheckCollisionPointRec(rl.GetMousePosition(), r)
}

func hovered(r rl.Rectangle) bool { return rl.CheckCollisionPointRec(rl.GetMousePosition(), r) }

func (w *window) draw() {
	sw, sh := float32(rl.GetScreenWidth()), float32(rl.GetScreenHeight())
	rl.ClearBackground(col(colBackground))
	state := w.model.Snapshot()
	// Whatever lies under the pointer may ask for another cursor.
	rl.SetMouseCursor(rl.MouseCursorDefault)

	// The log takes its height from the bottom; the rest is for the
	// header and the instruments.
	maxLog := sh - headerH - barH - 40
	if w.logHeight > maxLog {
		w.logHeight = max(maxLog, 0)
	}
	logTop := sh - barH - w.logHeight

	w.drawHeader(state, sw)
	w.drawGear(sw)
	if body := rl.NewRectangle(0, headerH, sw, logTop-headerH); w.edit != nil {
		w.drawEditor(state, body)
	} else if w.gear.open {
		w.drawSettings(body)
	} else {
		w.drawInstruments(state, body)
	}
	w.drawLog(rl.NewRectangle(0, logTop, sw, sh-logTop))
}

func (w *window) drawHeader(state *ipc.Snapshot, sw float32) {
	text(w.title, "HIDI", margin, 20, sizeTitle, col(colText))
	x := margin + width(w.title, "HIDI", sizeTitle) + 18

	if state == nil {
		text(w.sans, "HIDI is not running. Start it with \"hidi run\".", x, 27, sizeText, col(colFaint))
		return
	}
	// The switch: a pill with a knob, and the state in words.
	pill := rl.NewRectangle(x, 20, 52, 26)
	back, knobX, label := colOff, pill.X+13, "off"
	if state.Active {
		back, knobX, label = colOn, pill.X+pill.Width-13, "on"
	}
	if hovered(pill) {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
	}
	rl.DrawRectangleRounded(pill, 1, 16, col(back))
	rl.DrawCircle(int32(knobX), int32(pill.Y+13), 9, col(colText))
	text(w.bold, label, pill.X+pill.Width+10, 23, sizeName, col(colText))
	if clicked(pill) {
		w.toggle()
	}

	hint := "the devices type"
	if state.Active {
		hint = "the devices play"
	}
	text(w.sans, hint, pill.X+pill.Width+10+width(w.bold, label, sizeName)+12, 26, sizeText, col(colFaint))

	// Of "hidi 2.0.0 (commit) go1.27 linux/amd64", the version and the
	// commit.
	version := state.Version
	if fields := strings.Fields(version); len(fields) >= 3 && fields[0] == "hidi" {
		version = fields[1] + " " + fields[2]
	}
	if r := []rune(version); len(r) > 40 {
		version = string(r[:40])
	}
	// The gear has the corner; the version stands beside it.
	text(w.mono, version, sw-margin-44-width(w.mono, version, sizeMono), 28, sizeMono, col(colFaint))
}

// toggle asks HIDI to turn on or off, without making the drawing wait for
// the answer; the new state arrives like every other.
func (w *window) toggle() {
	select {
	case w.toggling <- struct{}{}:
	default:
		return // the last request is still on its way
	}
	go func() {
		defer func() { <-w.toggling }()
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
		defer cancel()
		w.hidi.Toggle(ctx)
	}()
}

func (w *window) drawInstruments(state *ipc.Snapshot, area rl.Rectangle) {
	if state == nil || area.Height <= 0 {
		return
	}
	if len(state.Instruments) == 0 {
		text(w.sans, "No instruments. Connect a keyboard or a game controller.", margin, area.Y+8, sizeText, col(colFaint))
		return
	}
	cardW := area.Width - 2*margin
	heights := make([]float32, len(state.Instruments))
	total := float32(margin) - cardGap
	for i, in := range state.Instruments {
		heights[i] = cardTop + w.inputHeight(in, cardW-2*cardPad) + 10 + pianoH + 12
		total += heights[i] + cardGap
	}
	if hovered(area) {
		w.cardScroll -= rl.GetMouseWheelMove() * 40
	}
	w.cardScroll = min(max(w.cardScroll, 0), max(total-area.Height, 0))

	rl.BeginScissorMode(int32(area.X), int32(area.Y), int32(area.Width), int32(area.Height))
	defer rl.EndScissorMode()
	y := area.Y - w.cardScroll
	for i, in := range state.Instruments {
		if y+heights[i] >= area.Y && y <= area.Y+area.Height {
			w.drawCard(in, rl.NewRectangle(margin, y, cardW, heights[i]))
		}
		y += heights[i] + cardGap
	}
}

const cardPad = 14

func (w *window) drawCard(in ipc.Instrument, r rl.Rectangle) {
	rl.DrawRectangleRounded(r, 16/r.Height, 8, col(colPanel))
	rl.DrawRectangleRoundedLinesEx(r, 16/r.Height, 8, 1, col(colPanelEdge))
	x, y := r.X+cardPad, r.Y+12

	text(w.bold, in.Name, x, y, sizeName, col(colText))
	about := in.Type + " · profile " + in.Profile
	text(w.sans, about, x+width(w.bold, in.Name, sizeName)+12, y+3, sizeText, col(colFaint))
	// The way to the instrument's profile, in the corner of its card.
	if in.ProfileFile != "" {
		label := "Edit profile"
		if hit, _ := w.button(label, r.X+r.Width-cardPad-width(w.sans, label, sizeText)-20, r.Y+9); hit {
			w.editProfile(in.ProfileFile)
		}
	}

	// The numbers are set in the fixed-width font with a fixed number of
	// places, so that nothing moves sideways when one of them changes.
	y += 30
	for _, field := range []struct{ label, value string }{
		{"octave", fmt.Sprintf("%+3d", in.Octave)},
		{"transpose", fmt.Sprintf("%+4d", in.Transpose)},
		{"channel", fmt.Sprintf("%2d", in.Channel)},
		{"velocity", fmt.Sprintf("%3d", in.Velocity)},
		{"notes", fmt.Sprintf("%2d", in.Sounding)},
		{"input", fmt.Sprintf("%4d/s", in.InputRate)},
		{"MIDI", fmt.Sprintf("%4d/s", in.MIDIRate)},
		{"layout", in.Layout},
		{"chord", in.Chord},
		{"", map[bool]string{true: "learning"}[in.Learning]},
	} {
		if field.value == "" {
			continue // no chord remembered, not learning
		}
		text(w.sans, field.label, x, y, sizeText, col(colFaint))
		x += width(w.sans, field.label, sizeText) + 6
		text(w.mono, field.value, x, y+1, sizeMono, col(colText))
		x += width(w.mono, field.value, sizeMono) + 20
	}

	// What the device itself reports, then what is played because of it.
	inner := r.Width - 2*cardPad
	inputH := w.inputHeight(in, inner)
	w.drawInput(in, r.X+cardPad, r.Y+cardTop, inner)
	w.drawPiano(in, rl.NewRectangle(r.X+cardPad, r.Y+cardTop+inputH+10, inner, pianoH))
}

// drawPiano draws the keyboard of notes, with those that are on lit.
func (w *window) drawPiano(in ipc.Instrument, r rl.Rectangle) {
	on := map[int]bool{}
	for _, n := range in.Notes {
		on[n] = true
	}
	for _, k := range Piano(firstNote, lastNote, r.X, r.Y, r.Width, r.Height) {
		rect := rl.NewRectangle(k.X, k.Y, k.W, k.H)
		switch {
		case on[k.Note]:
			rl.DrawRectangleRec(rect, col(colAccent))
		case k.Black:
			rl.DrawRectangleRec(rect, col(colBlackKey))
		default:
			rl.DrawRectangleRec(rect, col(colWhiteKey))
		}
		if !k.Black {
			rl.DrawRectangleLinesEx(rect, 0.5, col(colPanel))
			if k.Note%12 == 0 && k.W >= 9 {
				// Every C carries its octave; note 60 is C3 in HIDI.
				label := fmt.Sprintf("%d", k.Note/12-2)
				text(w.mono, label, k.X+(k.W-width(w.mono, label, 9))/2, k.Y+k.H-11, 9, col(colOff))
			}
		}
	}
}

// A keyboard is drawn as one; every other device as the list of what it
// reported.
func isKeyboard(in ipc.Instrument) bool { return in.Type == "keyboard" }

// controlName is the name of a control with the part it belongs to.
func controlName(part, name string) string {
	if part != "" {
		return part + ":" + name
	}
	return name
}

func (w *window) chipWidth(label string) float32 { return width(w.mono, label, sizeMono) + 14 }

// chipRows counts the rows the buttons take when they are set one after
// another across the width.
func (w *window) chipRows(in ipc.Instrument, inner float32) int {
	if len(in.Buttons) == 0 {
		return 0
	}
	rows, x := 1, float32(0)
	for _, b := range in.Buttons {
		cw := w.chipWidth(controlName(b.Part, b.Name))
		if x > 0 && x+cw > inner {
			rows, x = rows+1, 0
		}
		x += cw + 6
	}
	return rows
}

// axisColumns is how many axes fit beside each other.
func axisColumns(inner float32) int { return max(int(inner/380), 1) }

// inputHeight is the height the raw input of a device takes.
func (w *window) inputHeight(in ipc.Instrument, inner float32) float32 {
	if isKeyboard(in) {
		unit := min(inner/KeyboardUnitsW, keyUnit)
		return unit*KeyboardUnitsH + 24 // and the line of other keys
	}
	if len(in.Buttons) == 0 && len(in.Axes) == 0 {
		return 22
	}
	h := float32(w.chipRows(in, inner)) * (chipH + 6)
	if n := len(in.Axes); n > 0 {
		cols := axisColumns(inner)
		h += float32((n+cols-1)/cols) * axisRowH
	}
	return h
}

func (w *window) drawInput(in ipc.Instrument, x, y, inner float32) {
	if isKeyboard(in) {
		w.drawKeyboard(in, x, y, inner)
		return
	}
	if len(in.Buttons) == 0 && len(in.Axes) == 0 {
		text(w.sans, "Press a button or move a stick to see it here.", x, y+2, sizeText, col(colFaint))
		return
	}
	// The buttons, one after another.
	cx, cy := x, y
	for _, b := range in.Buttons {
		label := controlName(b.Part, b.Name)
		cw := w.chipWidth(label)
		if cx > x && cx+cw > x+inner {
			cx, cy = x, cy+chipH+6
		}
		chip := rl.NewRectangle(cx, cy, cw, chipH)
		if b.Down {
			rl.DrawRectangleRounded(chip, 0.4, 6, col(colAccent))
			text(w.mono, label, cx+7, cy+3, sizeMono, col(colBar))
		} else {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 1, col(colPanelEdge))
			text(w.mono, label, cx+7, cy+3, sizeMono, col(colFaint))
		}
		cx += cw + 6
	}
	if len(in.Buttons) > 0 {
		cy += chipH + 6
	}
	// The axes: name, a bar for the position, and the number.
	cols := axisColumns(inner)
	colW := inner / float32(cols)
	nameW := float32(0)
	for _, a := range in.Axes {
		nameW = max(nameW, width(w.mono, controlName(a.Part, a.Name), sizeMono))
	}
	valueW := width(w.mono, "0.000", sizeMono)
	for i, a := range in.Axes {
		ax := x + float32(i%cols)*colW
		ay := cy + float32(i/cols)*axisRowH
		text(w.mono, controlName(a.Part, a.Name), ax, ay+2, sizeMono, col(colFaint))
		bar := rl.NewRectangle(ax+nameW+10, ay+6, colW-nameW-valueW-44, 8)
		rl.DrawRectangleRounded(bar, 1, 4, col(colBar))
		value := float32(min(max(a.Value, 0), 1))
		rl.DrawRectangleRounded(rl.NewRectangle(bar.X, bar.Y, bar.Width*value, bar.Height), 1, 4, col(colAccent))
		// A mark in the middle, where a stick rests.
		rl.DrawRectangleRec(rl.NewRectangle(bar.X+bar.Width/2, bar.Y-2, 1, bar.Height+4), col(colOff))
		text(w.mono, fmt.Sprintf("%.3f", a.Value), bar.X+bar.Width+10, ay+2, sizeMono, col(colText))
	}
}

// drawKeyboard draws a full-size keyboard with the keys that are down lit.
// Keys that are down and not on the drawing are named in a line below it.
func (w *window) drawKeyboard(in ipc.Instrument, x, y, inner float32) {
	down := map[string]bool{}
	for _, b := range in.Buttons {
		if b.Down {
			down[controlName(b.Part, b.Name)] = true
		}
	}
	unit := min(inner/KeyboardUnitsW, keyUnit)
	labelSize := min(float32(10), unit*0.42)
	for _, key := range Keyboard() {
		rect := rl.NewRectangle(x+key.X*unit+1, y+key.Y*unit+1, key.W*unit-2, key.H*unit-2)
		lit := down[key.Name]
		delete(down, key.Name)
		ink := colFaint
		if lit {
			rl.DrawRectangleRounded(rect, 0.25, 4, col(colAccent))
			ink = colBar
		} else {
			rl.DrawRectangleRounded(rect, 0.25, 4, col(colBar))
		}
		if key.Label != "" && unit >= 16 {
			text(w.mono, key.Label, rect.X+(rect.Width-width(w.mono, key.Label, labelSize))/2, rect.Y+(rect.Height-labelSize)/2, labelSize, col(ink))
		}
	}
	// What is left is down and has no key on the drawing.
	if len(down) == 0 {
		return
	}
	others := make([]string, 0, len(down))
	for name := range down {
		others = append(others, name)
	}
	slices.Sort(others)
	oy := y + unit*KeyboardUnitsH + 5
	text(w.sans, "other keys", x, oy, sizeText, col(colFaint))
	ox := x + width(w.sans, "other keys", sizeText) + 10
	for _, name := range others {
		cw := w.chipWidth(name)
		if ox+cw > x+inner {
			break
		}
		rl.DrawRectangleRounded(rl.NewRectangle(ox, oy-2, cw, chipH), 0.4, 6, col(colAccent))
		text(w.mono, name, ox+7, oy+1, sizeMono, col(colBar))
		ox += cw + 6
	}
}

// drawLog draws the status bar with the last line of the log and, above
// it, the log itself as far as it is pulled up.
func (w *window) drawLog(area rl.Rectangle) {
	lines := w.model.Log()
	bar := rl.NewRectangle(area.X, area.Y, area.Width, barH)
	open := w.logHeight > 0

	// Pulling the bar up and down changes the height of the log; a click
	// on it opens and closes the log.
	mouse := rl.GetMousePosition()
	if hovered(bar) || w.dragging {
		rl.SetMouseCursor(rl.MouseCursorResizeNS)
	}
	if clicked(bar) && !w.overFilter(bar, mouse) {
		w.dragging, w.dragMoved = true, false
	}
	if w.dragging {
		if delta := rl.GetMouseDelta().Y; delta != 0 {
			w.dragMoved = true
			w.logHeight = max(w.logHeight-delta, 0)
		}
		if rl.IsMouseButtonReleased(rl.MouseButtonLeft) {
			w.dragging = false
			switch {
			case !w.dragMoved && open:
				w.savedHeight, w.logHeight = w.logHeight, 0
			case !w.dragMoved:
				w.logHeight = w.savedHeight
			case w.logHeight < 40:
				w.logHeight = 0 // too little to read: closed
			default:
				w.savedHeight = w.logHeight
			}
		}
	}

	rl.DrawRectangleRec(area, col(colBar))
	rl.DrawRectangleRec(rl.NewRectangle(area.X, area.Y, area.Width, 1), col(colPanelEdge))
	// A grip in the middle of the bar says that it can be pulled.
	rl.DrawRectangleRounded(rl.NewRectangle(area.X+area.Width/2-18, area.Y+4, 36, 3), 1, 4, col(colPanelEdge))

	if !open {
		// The last line: in its colours, but faint, so that it informs
		// without drawing the eye.
		if n := len(lines); n > 0 {
			w.drawLine(lines[n-1].Text, margin, area.Y+9, area.Width-2*margin, 120)
		} else {
			text(w.mono, "no log yet", margin, area.Y+9, sizeMono, colA(colFaint, 120))
		}
		return
	}

	w.drawFilter(bar)
	body := rl.NewRectangle(area.X, area.Y+filterH, area.Width, area.Height-filterH)
	if body.Height <= 0 {
		return
	}
	visible := int(body.Height / lineH)
	if hovered(body) {
		w.logScroll += rl.GetMouseWheelMove() * 3
	}
	w.logScroll = min(max(w.logScroll, 0), float32(max(len(lines)-visible, 0)))

	rl.BeginScissorMode(int32(body.X), int32(body.Y), int32(body.Width), int32(body.Height))
	defer rl.EndScissorMode()
	// The newest line sits at the bottom.
	last := len(lines) - int(w.logScroll)
	y := body.Y + body.Height - lineH - 4
	for i := last - 1; i >= 0 && y > body.Y-lineH; i-- {
		w.drawLine(lines[i].Text, margin, y, body.Width-2*margin, 255)
		y -= lineH
	}
	if len(lines) == 0 {
		text(w.mono, "nothing at this level yet", margin, body.Y+8, sizeMono, col(colFaint))
	}
}

// filterButtons returns where the buttons of the level filter are.
func (w *window) filterButtons(bar rl.Rectangle) []rl.Rectangle {
	var out []rl.Rectangle
	x := bar.X + bar.Width - margin
	for i := len(Levels) - 1; i >= 0; i-- {
		bw := width(w.mono, Levels[i], sizeMono) + 16
		x -= bw
		out = append([]rl.Rectangle{rl.NewRectangle(x, bar.Y+7, bw, 20)}, out...)
		x -= 6
	}
	return out
}

func (w *window) overFilter(bar rl.Rectangle, p rl.Vector2) bool {
	if w.logHeight <= 0 {
		return false
	}
	for _, b := range w.filterButtons(bar) {
		if rl.CheckCollisionPointRec(p, b) {
			return true
		}
	}
	return false
}

// drawFilter draws the choice of the least severe level to show: the
// chosen level and those above it are lit.
func (w *window) drawFilter(bar rl.Rectangle) {
	text(w.sans, "Log", margin, bar.Y+9, sizeText, col(colFaint))
	chosen := w.model.MinLevel()
	levelColours := []RGB{AnsiColour(90), AnsiColour(32), AnsiColour(33), AnsiColour(91)}
	buttons := w.filterButtons(bar)
	label := "show from"
	text(w.sans, label, buttons[0].X-width(w.sans, label, sizeText)-10, bar.Y+9, sizeText, col(colFaint))
	for i, b := range buttons {
		if hovered(b) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if clicked(b) {
			w.model.SetMinLevel(i)
			w.logScroll = 0
			chosen = i
		}
		if i >= chosen {
			rl.DrawRectangleRounded(b, 0.4, 6, colA(levelColours[i], 50))
			text(w.mono, Levels[i], b.X+8, b.Y+3, sizeMono, col(levelColours[i]))
		} else {
			rl.DrawRectangleRoundedLinesEx(b, 0.4, 6, 1, col(colPanelEdge))
			text(w.mono, Levels[i], b.X+8, b.Y+3, sizeMono, col(colOff))
		}
	}
}

// drawLine draws a log line in the colours HIDI gave it, cut off at the
// width there is.
func (w *window) drawLine(line string, x, y, maxW float32, alpha uint8) {
	// Every character of the fixed-width font is as wide as this one.
	charW := width(w.mono, "0", sizeMono)
	room := int(maxW / charW)
	for _, span := range logging.Spans(line) {
		if room <= 0 {
			return
		}
		s := []rune(span.Text)
		if len(s) > room {
			s = append(s[:max(room-1, 0)], '…')
		}
		text(w.mono, string(s), x, y, sizeMono, colA(AnsiColour(span.Fg.Code), alpha))
		x += float32(len(s)) * charW
		room -= len(s)
	}
}
