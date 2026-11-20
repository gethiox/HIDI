//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/assets/fonts"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/settings"
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
	colFaint      = RGB{0x8b, 0x94, 0xa3}
	colOn         = RGB{0x21, 0xa3, 0x5a}
	colOff        = RGB{0x5c, 0x63, 0x6e}
	colAccent     = RGB{0x3d, 0xc9, 0x7b}
	colBusy       = RGB{0xa5, 0xe8, 0xc0} // a number that says something is going on
	colIdleDown   = RGB{0x9a, 0xa1, 0xac} // a control that is down while HIDI is off
	colKeyInk     = RGB{0xb9, 0xc0, 0xcc} // the lettering of a key that does something
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

	// active says whether HIDI is on, as of the frame being drawn.
	active bool
	// shown says what is drawn of which instrument.
	shown settings.Shown
	// glow is the picture of a light under a key.
	glow rl.Texture2D

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

	// HIDI's state is asked for as often as this monitor can show it, and
	// no more often: every state costs both sides some work.
	if opts.SnapshotPace != nil {
		rate := rl.GetMonitorRefreshRate(rl.GetCurrentMonitor())
		if rate <= 0 {
			rate = 60
		}
		if opts.FPS > 0 && opts.FPS < rate {
			rate = opts.FPS
		}
		opts.SnapshotPace(max(time.Second/time.Duration(rate), 2*time.Millisecond))
	}
	go Follow(ctx, hidi, model)

	w := &window{hidi: hidi, model: model, opts: opts, ctx: ctx, toggling: make(chan struct{}, 1)}
	w.loadFonts()
	defer w.unloadFonts()
	w.glow = loadGlow()
	defer rl.UnloadTexture(w.glow)
	if opts.Paths.Root != "" {
		if s, err := settings.Load(opts.Paths.Settings); err == nil {
			w.shown = s.Shown
		}
	}

	w.savedHeight = 220
	if opts.LogHeight > 0 {
		w.logHeight, w.savedHeight = float32(opts.LogHeight), float32(opts.LogHeight)
	}

	pace := newPacer(opts.FPS, opts.BackgroundFPS)
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
			// Nothing to draw: look at the pointer and the keys, and
			// sleep. The sleep is the system's, which costs nothing; the
			// library's own waits by spinning for part of the time. It is
			// short whatever the frame limit, since a click that begins
			// and ends between two looks is not seen.
			rl.PollInputEvents()
			time.Sleep(min(time.Second/time.Duration(pace.target), idlePoll))
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

// idlePoll is the longest the idle window goes without looking at the
// pointer.
const idlePoll = 10 * time.Millisecond

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
	limit      int // 0 for none
	background int // the limit behind another window, 0 for none of its own
	target     int32
	checked    time.Time
	refresh    int32
}

func newPacer(limit, background int) *pacer { return &pacer{limit: limit, background: background} }

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
	if p.limit > 0 && int32(p.limit) < want {
		want = int32(p.limit)
	}
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

// inputOff makes what is drawn deaf to the pointer: it is set while the
// part of the window that lies under the settings is drawn. The window is
// drawn by one goroutine.
var inputOff bool

func clicked(r rl.Rectangle) bool {
	return rl.IsMouseButtonPressed(rl.MouseButtonLeft) && hovered(r)
}

func hovered(r rl.Rectangle) bool {
	if inputOff {
		return false
	}
	p := rl.GetMousePosition()
	// What is cut off from view is not there for the pointer either: a
	// button scrolled under the log or the header must not take a click
	// meant for what covers it.
	if clipped && !rl.CheckCollisionPointRec(p, clip) {
		return false
	}
	return rl.CheckCollisionPointRec(p, r)
}

// clip is the part of the window that drawing is held to at the moment,
// if clipped is set.
var (
	clip    rl.Rectangle
	clipped bool
)

// beginClip holds drawing, and the pointer, to r until endClip.
func beginClip(r rl.Rectangle) {
	rl.BeginScissorMode(int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height))
	clip, clipped = r, true
}

func endClip() {
	rl.EndScissorMode()
	clipped = false
}

// wheel returns how far the mouse wheel was turned over r.
func wheel(r rl.Rectangle) float32 {
	if !hovered(r) {
		return 0
	}
	return rl.GetMouseWheelMove()
}

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
		// The settings lie over the instruments, which stay in view
		// beside and faintly through them, and take no clicks meanwhile.
		inputOff = true
		w.drawInstruments(state, body)
		inputOff = false
		w.drawOverlay(state, body)
	} else {
		w.drawInstruments(state, body)
	}
	w.drawLog(rl.NewRectangle(0, logTop, sw, sh-logTop))
}

func (w *window) drawHeader(state *ipc.Snapshot, sw float32) {
	text(w.title, "HIDI", margin, 20, sizeTitle, col(colText))
	x := margin + width(w.title, "HIDI", sizeTitle) + 18

	if state == nil {
		text(w.sans, "HIDI is not running. Start it with \"hidi\".", x, 27, sizeText, col(colFaint))
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
	w.active = state.Active
	cardW := area.Width - 2*margin
	heights := make([]float32, len(state.Instruments))
	total := float32(margin) - cardGap
	for i, in := range state.Instruments {
		heights[i] = w.cardHeight(in, cardW-2*cardPad)
		total += heights[i] + cardGap
	}
	w.cardScroll -= wheel(area) * 40
	w.cardScroll = min(max(w.cardScroll, 0), max(total-area.Height, 0))

	beginClip(area)
	defer endClip()
	y := area.Y - w.cardScroll
	for i, in := range state.Instruments {
		if y+heights[i] >= area.Y && y <= area.Y+area.Height {
			w.drawCard(in, rl.NewRectangle(margin, y, cardW, heights[i]))
		}
		y += heights[i] + cardGap
	}
}

const cardPad = 14

// cardHeight is the height of an instrument's card: its two lines, and
// below them what the user asked to see of it.
func (w *window) cardHeight(in ipc.Instrument, inner float32) float32 {
	h := float32(cardTop)
	if w.isShown(w.shown.Input, in.Name) {
		h += w.inputHeight(in, inner) + 10
	}
	if w.isShown(w.shown.Output, in.Name) {
		h += pianoH + 12
	}
	return h
}

func (w *window) isShown(list []string, name string) bool {
	return w.opts.ShowAll || slices.Contains(list, name)
}

// chip draws a small switch with a name, lit when it is on, and reports a
// click on it.
func (w *window) chip(label string, on bool, x, y float32) bool {
	r := rl.NewRectangle(x, y, w.chipWidth(label), chipH)
	if hovered(r) {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
	}
	if on {
		rl.DrawRectangleRounded(r, 0.4, 6, col(colAccent))
		text(w.mono, label, x+7, y+3, sizeMono, col(colBar))
	} else {
		rl.DrawRectangleRoundedLinesEx(r, 0.4, 6, 1, col(colPanelEdge))
		text(w.mono, label, x+7, y+3, sizeMono, col(colFaint))
	}
	return clicked(r)
}

func (w *window) drawCard(in ipc.Instrument, r rl.Rectangle) {
	rl.DrawRectangleRounded(r, 16/r.Height, 8, col(colPanel))
	rl.DrawRectangleRoundedLinesEx(r, 16/r.Height, 8, 1, col(colPanelEdge))
	x, y := r.X+cardPad, r.Y+12

	text(w.bold, in.Name, x, y, sizeName, col(colText))
	about := in.Type + " · profile " + in.Profile
	text(w.sans, about, x+width(w.bold, in.Name, sizeName)+12, y+3, sizeText, col(colFaint))
	// In the corner of the card: the way to the instrument's profile,
	// and the switches for what is drawn of the instrument.
	right := r.X + r.Width - cardPad
	if in.ProfileFile != "" {
		label := "Edit profile"
		right -= width(w.sans, label, sizeText) + 20
		if hit, _ := w.button(label, right, r.Y+9); hit {
			w.editProfile(in.ProfileFile)
		}
		right -= 14
	}
	showInput, showOutput := w.isShown(w.shown.Input, in.Name), w.isShown(w.shown.Output, in.Name)
	right -= w.chipWidth("output")
	if w.chip("output", showOutput, right, r.Y+11) {
		w.shown.Output = flip(w.shown.Output, in.Name)
		w.saveShown()
	}
	right -= w.chipWidth("input") + 6
	if w.chip("input", showInput, right, r.Y+11) {
		w.shown.Input = flip(w.shown.Input, in.Name)
		w.saveShown()
	}
	text(w.sans, "show", right-width(w.sans, "show", sizeText)-8, r.Y+13, sizeText, col(colFaint))

	// The numbers are set in the fixed-width font with a fixed number of
	// places, so that nothing moves sideways when one of them changes.
	y += 30
	for _, field := range []struct {
		label, value string
		busy         bool // something is going on: the number is tinted
	}{
		{"octave", fmt.Sprintf("%+3d", in.Octave), false},
		{"transpose", fmt.Sprintf("%+4d", in.Transpose), false},
		{"channel", fmt.Sprintf("%2d", in.Channel), false},
		{"velocity", fmt.Sprintf("%3d", in.Velocity), false},
		{"notes", fmt.Sprintf("%2d", in.Sounding), false},
		{"input", fmt.Sprintf("%4d/s", in.InputRate), in.InputRate > 0},
		{"MIDI", fmt.Sprintf("%4d/s", in.MIDIRate), in.MIDIRate > 0},
		{"layout", in.Layout, false},
		{"chord", in.Chord, false},
		{"", map[bool]string{true: "learning"}[in.Learning], false},
	} {
		if field.value == "" {
			continue // no chord remembered, not learning
		}
		text(w.sans, field.label, x, y, sizeText, col(colFaint))
		x += width(w.sans, field.label, sizeText) + 6
		ink := colText
		if field.busy {
			ink = colBusy
		}
		text(w.mono, field.value, x, y+1, sizeMono, col(ink))
		x += width(w.mono, field.value, sizeMono) + 20
	}

	// What the device itself reports, then what is played because of it.
	inner := r.Width - 2*cardPad
	y = r.Y + cardTop
	if showInput {
		w.drawInput(in, r.X+cardPad, y, inner)
		y += w.inputHeight(in, inner) + 10
	}
	if showOutput {
		w.drawPiano(in, rl.NewRectangle(r.X+cardPad, y, inner, pianoH))
	}
}

// saveShown keeps what is shown of which instrument in the settings file,
// where there is one. The file is read first: it may have been changed
// since the window last looked.
func (w *window) saveShown() {
	p := w.opts.Paths
	if p.Root == "" || !settings.Managed(p) {
		return
	}
	s, err := settings.Load(p.Settings)
	if err != nil {
		return // a file with mistakes is the user's to mend, not to overwrite
	}
	s.Shown = w.shown
	settings.Save(p.Settings, s)
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

// An axis is drawn as a line of its own: its name, a bar that fills from
// the left, and its value.
const (
	axisBarMin = 64 // the least width of the bar
	axisBarH   = 8
	axisSize   = 12 // of the lettering
	axisGap    = 22 // between two axes beside each other
)

// axisLabel is the name of an axis as it stands before its bar: without
// the "ABS_" every one of them begins with.
func axisLabel(part, name string) string {
	return controlName(part, strings.TrimPrefix(name, "ABS_"))
}

// axisLayout says how the axes of an instrument are set out: how many
// beside each other, how wide each of them, and how wide the names.
func (w *window) axisLayout(in ipc.Instrument, inner float32) (cols int, cell, label float32) {
	for _, a := range in.Axes {
		label = max(label, width(w.mono, axisLabel(a.Part, a.Name), axisSize))
	}
	least := label + 8 + axisBarMin + 8 + width(w.mono, "0.00", axisSize) + axisGap
	cols = max(int((inner+axisGap)/least), 1)
	return cols, (inner + axisGap) / float32(cols), label
}

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
		cols, _, _ := w.axisLayout(in, inner)
		h += float32((n+cols-1)/cols) * axisRowH
	}
	return h
}

// pressed is the colour of a control that is down: the accent while the
// devices play, a grey while they do not.
func (w *window) pressed() RGB {
	if w.active {
		return colAccent
	}
	return colIdleDown
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
			rl.DrawRectangleRounded(chip, 0.4, 6, col(w.pressed()))
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
	// The axes: the name, a bar for the position, and the number.
	cols, cell, labelW := w.axisLayout(in, inner)
	valueW := width(w.mono, "0.00", axisSize)
	for i, a := range in.Axes {
		ax := x + float32(i%cols)*cell
		ay := cy + float32(i/cols)*axisRowH
		label := axisLabel(a.Part, a.Name)
		text(w.mono, label, ax+labelW-width(w.mono, label, axisSize), ay+3, axisSize, col(colFaint))
		bar := rl.NewRectangle(float32(int(ax+labelW+8)), ay+(axisRowH-axisBarH)/2, cell-axisGap-labelW-16-valueW, axisBarH)
		rl.DrawRectangleRounded(bar, 1, 4, col(colBar))
		value := float32(min(max(a.Value, 0), 1))
		if filled := bar.Width * value; filled >= 1 {
			rl.DrawRectangleRounded(rl.NewRectangle(bar.X, bar.Y, filled, bar.Height), 1, 4, col(w.pressed()))
		}
		// A mark in the middle, where a stick rests.
		rl.DrawRectangleRec(rl.NewRectangle(bar.X+bar.Width/2, bar.Y-2, 1, bar.Height+4), col(colOff))
		text(w.mono, fmt.Sprintf("%.2f", a.Value), bar.X+bar.Width+8, ay+3, axisSize, col(colText))
	}
}

// drawKeyboard draws a full-size keyboard. Every key has the colour of
// what it does in the layout that is selected, as in the view that
// changes a profile. Where HIDI sets the lights of the keyboard itself,
// every key also glows in the colour of its light, so that the drawing
// shows what the keyboard shows. Keys that are down stand out, and those
// that are not on the drawing are named in a line below it.
func (w *window) drawKeyboard(in ipc.Instrument, x, y, inner float32) {
	down := map[string]bool{}
	for _, b := range in.Buttons {
		if b.Down {
			down[controlName(b.Part, b.Name)] = true
		}
	}
	roles := make(map[string]ipc.Key, len(in.Keys))
	for _, k := range in.Keys {
		roles[k.Name] = k
	}
	var lights map[string]RGB
	var rest RGB
	if in.Lit {
		rest = ParseColour(in.LightRest)
		lights = make(map[string]RGB, len(in.Lights))
		for _, l := range in.Lights {
			lights[l.Name] = ParseColour(l.Colour)
		}
	}
	// light is the colour a key glows in; black for none.
	light := func(name string) RGB {
		if !in.Lit {
			return RGB{}
		}
		if c, own := lights[name]; own {
			return Glow(c)
		}
		return Glow(rest)
	}
	unit := min(inner/KeyboardUnitsW, keyUnit)
	labelSize := min(float32(10), unit*0.42)
	keys := Keyboard()
	rect := func(key KeyCap) rl.Rectangle {
		return rl.NewRectangle(x+key.X*unit+1, y+key.Y*unit+1, key.W*unit-2, key.H*unit-2)
	}
	pressed := w.pressed()

	// The caps first, then the glow of every light, which adds to what
	// is under it as light does, then what stands on the caps.
	for _, key := range keys {
		role := roles[key.Name]
		fill := KeyColour(role.Role, role.Note, in.Octave, in.Transpose)
		if down[key.Name] && !in.Lit {
			fill = pressed
		}
		rl.DrawRectangleRounded(rect(key), 0.25, 4, col(fill))
	}
	if in.Lit {
		rl.BeginBlendMode(rl.BlendAdditive)
		src := rl.NewRectangle(0, 0, float32(w.glow.Width), float32(w.glow.Height))
		for _, key := range keys {
			if c := light(key.Name); c != (RGB{}) {
				rl.DrawTexturePro(w.glow, src, rect(key), rl.NewVector2(0, 0), 0, col(c))
			}
		}
		rl.EndBlendMode()
	}
	for _, key := range keys {
		r := rect(key)
		isDown := down[key.Name]
		delete(down, key.Name)
		ink := colFaint
		switch c := light(key.Name); {
		case in.Lit && isDown:
			rl.DrawRectangleRoundedLinesEx(r, 0.25, 4, 2, col(pressed))
			fallthrough
		case in.Lit:
			// Dark letters on a bright light, bright ones on a dim one.
			ink = colText
			if (int(c.R)*299+int(c.G)*587+int(c.B)*114)/1000 > 150 {
				ink = colBar
			}
		case isDown:
			ink = colBar
		case roles[key.Name].Role != "":
			ink = colKeyInk
		}
		if key.Label != "" && unit >= 16 {
			text(w.mono, key.Label, r.X+(r.Width-width(w.mono, key.Label, labelSize))/2, r.Y+(r.Height-labelSize)/2, labelSize, col(ink))
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
		rl.DrawRectangleRounded(rl.NewRectangle(ox, oy-2, cw, chipH), 0.4, 6, col(pressed))
		text(w.mono, name, ox+7, oy+1, sizeMono, col(colBar))
		ox += cw + 6
	}
}

// loadGlow makes the picture of a light under a key: brightest in the
// middle and fading to nothing at the edge, as a blurred dot. It is
// stretched over a key and tinted with the colour of the light.
func loadGlow() rl.Texture2D {
	const size = 64
	img := rl.GenImageColor(size, size, rl.Blank)
	for py := range size {
		for px := range size {
			dx, dy := (float64(px)+0.5)/size*2-1, (float64(py)+0.5)/size*2-1
			// A bell over the whole key, and a small bright core.
			d2 := dx*dx + dy*dy
			level := 0.75*math.Exp(-d2/0.5) + 0.25*math.Exp(-d2/0.03)
			// Nothing at the very edge, so that the rounded corners of
			// the cap stay as they are.
			edge := min(1-math.Abs(dx), 1-math.Abs(dy)) * 8
			level *= min(max(edge, 0), 1)
			v := uint8(min(level, 1)*255 + 0.5)
			rl.ImageDrawPixel(img, int32(px), int32(py), rl.NewColor(v, v, v, 255))
		}
	}
	tex := rl.LoadTextureFromImage(img)
	rl.UnloadImage(img)
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	return tex
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
	w.logScroll += wheel(body) * 3
	w.logScroll = min(max(w.logScroll, 0), float32(max(len(lines)-visible, 0)))

	beginClip(body)
	defer endClip()
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
