//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/assets/fonts"
	"github.com/gethiox/HIDI/internal/assets/icon"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/logging"
	"github.com/gethiox/HIDI/internal/settings"
	"github.com/gethiox/HIDI/internal/update"
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
	colBackground = RGB{0x12, 0x15, 0x1a}
	colPanel      = RGB{0x1b, 0x1f, 0x26}
	colPanelEdge  = RGB{0x2c, 0x32, 0x3d}
	colFaint      = RGB{0x8b, 0x94, 0xa3}
	colOn         = RGB{0x21, 0xa3, 0x5a}
	colOff        = RGB{0x5c, 0x63, 0x6e}
	colAccent     = RGB{0x3d, 0xc9, 0x7b}
	colInput      = RGB{0x3d, 0xc9, 0x7b} // what comes in from the devices: its curve
	colMIDI       = RGB{0xe6, 0xc2, 0x3f} // what goes out as MIDI: its curve
	colInputText  = RGB{0xa5, 0xe8, 0xc0} // a number of what comes in, while it is not zero
	colMIDIText   = RGB{0xf2, 0xdc, 0x8c} // a number of what goes out, while it is not zero
	colUnsaved    = RGB{0xf2, 0xc9, 0x4c} // changes that wait to be saved
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
	cards       glide   // how far the instruments are scrolled
	toggling    chan struct{}

	// active says whether HIDI is on, as of the frame being drawn.
	active bool
	// moving says that something drawn in this frame is on its way
	// somewhere, so that another frame has to follow; dt is the time
	// since the frame before, and drawn when that one was drawn.
	moving bool
	dt     time.Duration
	drawn  time.Time
	// trails says, for every instrument, when its trail last gained a
	// step.
	trails map[string]*trailSeen
	// docs are the profiles that were opened for changing, by their
	// files: what was changed in one and not saved is still there when
	// its page is opened again.
	docs map[string]*editor
	// trying holds, by profile file, the text HIDI is still to be given
	// to play with; tryBusy says that somebody is giving.
	tryMu   sync.Mutex
	trying  map[string]string
	tryBusy bool
	// shown says what is drawn of which instrument.
	shown settings.Shown
	// glow is the picture of a light under a key, wheel that of the gear
	// and icons HIDI's icon while off, on, and in the other half of the
	// blinking of learning mode.
	glow  rl.Texture2D
	wheel rl.Texture2D
	icons [3]rl.Texture2D
	// signs are the logo of OpenRGB in its three looks, by icon.RGBLook.
	signs [3]rl.Texture2D
	// wantScale is the size everything is to be drawn at from the next
	// frame on, as a factor.
	wantScale float32
	// version is the version of this HIDI and newest what is known of
	// newer ones.
	version string
	newest  newest

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
	// The icon of the window, for the title bar and the task bar.
	drawn := icon.Render(64, icon.Program)
	emblem := rl.GenImageColor(64, 64, rl.Blank)
	for y := range 64 {
		for x := range 64 {
			c := drawn.NRGBAAt(x, y)
			rl.ImageDrawPixel(emblem, int32(x), int32(y), rl.NewColor(c.R, c.G, c.B, c.A))
		}
	}
	rl.SetWindowIcon(*emblem)
	rl.UnloadImage(emblem)
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
	if opts.Scale >= settings.MinScale && opts.Scale <= settings.MaxScale {
		uiScale = float32(opts.Scale) / 100
	}
	w.wantScale = uiScale
	w.loadFonts()
	defer w.unloadFonts()
	// What was tried and not saved ends with the window: the files count
	// again.
	defer func() {
		for path, e := range w.docs {
			if e.doc.Dirty() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				hidi.Preview(ctx, path, "")
				cancel()
			}
		}
	}()
	w.glow = loadGlow()
	defer rl.UnloadTexture(w.glow)
	w.wheel = loadGear()
	defer rl.UnloadTexture(w.wheel)
	for i, colours := range []icon.Colours{icon.Off, icon.On, icon.Learning} {
		w.icons[i] = loadIcon(colours)
		defer rl.UnloadTexture(w.icons[i])
	}
	for i, look := range []icon.RGBLook{icon.RGBColour, icon.RGBGrey, icon.RGBYellow} {
		drawn := icon.OpenRGB(96, look)
		w.signs[i] = loadPictureSize(drawn.Bounds().Dx(), drawn.Bounds().Dy(), func(x, y int) rl.Color {
			c := drawn.NRGBAAt(x, y)
			return rl.NewColor(c.R, c.G, c.B, c.A)
		})
		defer rl.UnloadTexture(w.signs[i])
	}
	w.version = opts.Version
	w.newest.check = opts.CheckUpdate
	if opts.Paths.Root != "" {
		if s, err := settings.Load(opts.Paths.Settings); err == nil {
			w.shown = s.Shown
			w.newest.on = s.Window.CheckUpdates
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
		if version != seen || w.gear.open || w.dragging || w.moving || userActive() || rl.IsWindowResized() || rl.IsWindowFocused() != focused {
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
			applyCursor()
			time.Sleep(min(time.Second/time.Duration(pace.target), idlePoll))
			continue
		}
		settle--
		// A new size is taken up between two frames: the fonts are
		// made anew for it.
		if w.wantScale != uiScale {
			w.unloadFonts()
			uiScale = w.wantScale
			w.loadFonts()
		}
		rl.BeginDrawing()
		rl.BeginMode2D(rl.Camera2D{Zoom: uiScale})
		w.draw()
		rl.EndMode2D()
		rl.EndDrawing()
		applyCursor()
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
	scale *= uiScale
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
	// Whole pixels keep the letters sharp: those of the screen, which
	// the lens makes other than the window's own.
	x, y = float32(int(x*uiScale+0.5))/uiScale, float32(int(y*uiScale+0.5))/uiScale
	rl.DrawTextEx(font, s, rl.NewVector2(x, y), size, 0, c)
}

func width(font rl.Font, s string, size float32) float32 {
	return rl.MeasureTextEx(font, s, size, 0).X
}

// wantCursor is the cursor the frame being drawn asks for, and shownCursor
// the one that was last set. The cursor is set once a frame, when all of
// it is drawn, and not by every part that has a wish: a cursor that is
// set and set again flickers, and each setting makes a new one on some
// systems.
var wantCursor, shownCursor int32 = rl.MouseCursorDefault, rl.MouseCursorDefault

func setCursor(c int32) { wantCursor = c }

// applyCursor makes the pointer look as the last frame asked. On Windows
// the system gives the pointer its arrow back whenever it moves, so there
// the wish is repeated after every look at the pointer, as long as it is
// over the window.
func applyCursor() {
	again := runtime.GOOS == "windows" && wantCursor != rl.MouseCursorDefault && rl.IsCursorOnScreen()
	if wantCursor != shownCursor || again {
		rl.SetMouseCursor(wantCursor)
		shownCursor = wantCursor
	}
}

// uiScale is the size everything is drawn at, as a factor: the window is
// laid out in its ordinary measures and drawn through a lens. The fonts
// are made for the size they end up at, so that text stays sharp.
var uiScale float32 = 1

// mouse returns where the pointer is, in the measures the window is laid
// out in.
func mouse() rl.Vector2 {
	p := rl.GetMousePosition()
	return rl.NewVector2(p.X/uiScale, p.Y/uiScale)
}

// scissor holds drawing to r, which is in the window's own measures.
func scissor(r rl.Rectangle) {
	rl.BeginScissorMode(int32(r.X*uiScale), int32(r.Y*uiScale), int32(max(r.Width, 0)*uiScale+0.5), int32(max(r.Height, 0)*uiScale+0.5))
}

// inputOff makes what is drawn deaf to the pointer: it is set while the
// part of the window that lies under the settings is drawn. The window is
// drawn by one goroutine.
var inputOff bool

func clicked(r rl.Rectangle) bool {
	return rl.IsMouseButtonPressed(rl.MouseButtonLeft) && hovered(r)
}

func hovered(r rl.Rectangle) bool {
	// A pointer that left the window in a hurry is last seen inside it;
	// nothing is under a pointer that is not over the window.
	if inputOff || !rl.IsCursorOnScreen() {
		return false
	}
	p := mouse()
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
	scissor(r)
	clip, clipped = r, true
}

func endClip() {
	rl.EndScissorMode()
	clipped = false
}

// beginClipWithin holds drawing to the part of r that the clip in force
// leaves, until endClipWithin puts that clip back.
func beginClipWithin(r rl.Rectangle) {
	outer, was := clip, clipped
	if was {
		r = rl.GetCollisionRec(r, outer)
		rl.EndScissorMode()
	}
	scissor(r)
	clipStack = append(clipStack, struct {
		r  rl.Rectangle
		on bool
	}{outer, was})
	clip, clipped = r, true
}

func endClipWithin() {
	rl.EndScissorMode()
	last := clipStack[len(clipStack)-1]
	clipStack = clipStack[:len(clipStack)-1]
	clip, clipped = last.r, last.on
	if clipped {
		scissor(clip)
	}
}

// clipStack holds the clips that wait for the ones inside them to end.
var clipStack []struct {
	r  rl.Rectangle
	on bool
}

// wheel returns how far the mouse wheel was turned over r.
func wheel(r rl.Rectangle) float32 {
	if !hovered(r) {
		return 0
	}
	return rl.GetMouseWheelMove()
}

func (w *window) draw() {
	sw, sh := float32(rl.GetScreenWidth())/uiScale, float32(rl.GetScreenHeight())/uiScale
	rl.ClearBackground(col(colBackground))
	state := w.model.Snapshot()
	// Whatever lies under the pointer may ask for another cursor.
	wantCursor = rl.MouseCursorDefault
	// What moves by itself moves by the time since the last frame. After
	// a pause in which nothing was drawn that is one frame's worth.
	now := time.Now()
	w.dt = min(now.Sub(w.drawn), 50*time.Millisecond)
	w.drawn, w.moving = now, false
	w.follow(state, now)

	// The log takes its height from the bottom; the rest is for the
	// header and the instruments.
	maxLog := sh - headerH - barH - 40
	if w.logHeight > maxLog {
		w.logHeight = max(maxLog, 0)
	}
	logTop := sh - barH - w.logHeight

	w.drawHeader(state, sw)
	w.drawGear(sw)
	// The settings lie over everything else, which stays in view beside
	// and faintly through them, and takes no clicks meanwhile.
	body := rl.NewRectangle(0, headerH, sw, logTop-headerH)
	inputOff = w.gear.open
	if w.edit != nil {
		w.drawEditor(state, body)
	} else {
		w.drawInstruments(state, body)
	}
	inputOff = false
	if w.gear.open {
		w.drawOverlay(state, body)
	}
	w.drawLog(rl.NewRectangle(0, logTop, sw, sh-logTop))
}

func (w *window) drawHeader(state *ipc.Snapshot, sw float32) {
	// The icon, in the colours of the state as the icon in the tray has
	// them, and the name.
	const iconSize = 30
	picture, strength := w.icons[0], uint8(255)
	switch {
	case state == nil:
		strength = 115
	case state.Active && state.Learning && time.Now().UnixMilli()/500%2 == 1:
		picture = w.icons[2]
	case state.Active:
		picture = w.icons[1]
	}
	if state != nil && state.Active && state.Learning {
		w.moving = true // the blinking goes on
	}
	rl.DrawTexturePro(picture, rl.NewRectangle(0, 0, float32(picture.Width), float32(picture.Height)),
		rl.NewRectangle(margin, (headerH-iconSize)/2, iconSize, iconSize), rl.NewVector2(0, 0), 0, rl.NewColor(255, 255, 255, strength))
	nameX := float32(margin + iconSize + 10)
	text(w.title, "HIDI", nameX, 20, sizeTitle, col(colText))
	x := nameX + width(w.title, "HIDI", sizeTitle) + 18

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
		setCursor(rl.MouseCursorPointingHand)
	}
	rl.DrawRectangleRounded(pill, 1, 16, col(back))
	rl.DrawCircle(int32(knobX), int32(pill.Y+13), 9, col(colText))
	text(w.bold, label, pill.X+pill.Width+10, 23, sizeName, col(colText))
	if clicked(pill) {
		w.toggle()
	}
	// What follows stands where it stands, whichever word is shown.
	hx := pill.X + pill.Width + 10 + max(width(w.bold, "on", sizeName), width(w.bold, "off", sizeName))

	// Learning mode: a smaller switch with a fainter word, yellow while
	// it is on.
	small := rl.NewRectangle(hx+26, 24, 34, 18)
	back, knobX = colOff, small.X+9
	if state.Learning {
		back, knobX = colUnsaved, small.X+small.Width-9
	}
	word := "learning mode"
	area := rl.NewRectangle(small.X, small.Y, small.Width+8+width(w.sans, word, sizeText-1), small.Height)
	if hovered(area) {
		setCursor(rl.MouseCursorPointingHand)
	}
	rl.DrawRectangleRounded(small, 1, 12, col(back))
	rl.DrawCircle(int32(knobX), int32(small.Y+9), 6, col(colText))
	ink := colOff
	if state.Learning {
		ink = colUnsaved
	}
	text(w.sans, word, small.X+small.Width+8, 26, sizeText-1, col(ink))
	if clicked(area) {
		w.ask(func(ctx context.Context) { w.hidi.ToggleLearning(ctx) })
	}

	// Of "hidi 2.0.0 (commit) go1.27 linux/amd64", the version and the
	// commit.
	version := state.Version
	if fields := strings.Fields(version); len(fields) >= 3 && fields[0] == "hidi" {
		version = fields[1] + " " + fields[2]
	}
	if r := []rune(version); len(r) > 40 {
		version = string(r[:40])
	}
	// The gear has the corner; the version stands beside it, and before
	// the version what all instruments together take in and send.
	// In a narrow window, or a large size, what does not fit beside the
	// switches is left out, the least needed first.
	leftEnd := area.X + area.Width + 16
	vx := sw - margin - 44 - width(w.mono, version, sizeMono)
	if vx > leftEnd {
		text(w.mono, version, vx, 28, sizeMono, col(colFaint))
	} else {
		vx = sw - margin - 44 + 22
	}
	in, out := 0, 0
	for _, instrument := range state.Instruments {
		in, out = in+instrument.InputRate, out+instrument.MIDIRate
	}
	rx := vx - 22
	for _, rate := range []struct {
		label string
		value int
		busy  RGB
	}{{"MIDI", out, colMIDIText}, {"input", in, colInputText}} {
		number := fmt.Sprintf("%5d/s", rate.value)
		if rx-width(w.mono, number, sizeMono)-width(w.sans, rate.label, sizeText)-4 < leftEnd {
			break
		}
		ink := colFaint
		if rate.value > 0 {
			ink = rate.busy
		}
		rx -= width(w.mono, number, sizeMono)
		text(w.mono, number, rx, 28, sizeMono, col(ink))
		rx -= width(w.sans, rate.label, sizeText) + 4
		text(w.sans, rate.label, rx, 27, sizeText, col(colFaint))
		rx -= 16
	}
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

// trailKey tells the instruments apart for their trails.
func trailKey(in ipc.Instrument) string { return in.Name + "\x00" + in.Location + "\x00" + in.Type }

// trailSeen is when the window saw a trail gain its newest step.
type trailSeen struct {
	seq uint64
	at  time.Time
}

// follow notes when the trail of every instrument gained a step, so that
// the trails can be drawn where they are between two steps.
func (w *window) follow(state *ipc.Snapshot, now time.Time) {
	if state == nil {
		return
	}
	if w.trails == nil {
		w.trails = map[string]*trailSeen{}
	}
	there := map[string]bool{}
	for _, in := range state.Instruments {
		if in.Trail == nil {
			continue
		}
		key := trailKey(in)
		there[key] = true
		seen := w.trails[key]
		if seen == nil {
			seen = &trailSeen{}
			w.trails[key] = seen
		}
		if seen.seq != in.Trail.Seq {
			seen.seq, seen.at = in.Trail.Seq, now
		}
		// A curve with something in it runs on between the steps.
		if slices.Max(in.Trail.Input) > 0 || slices.Max(in.Trail.MIDI) > 0 {
			w.moving = true
		}
	}
	for key := range w.trails {
		if !there[key] {
			delete(w.trails, key)
		}
	}
}

// drawTrails draws how much an instrument took in and sent over the last
// seconds, as two curves over the area: what came in green, what went out
// as MIDI yellow. They lie behind what is written there.
//
// A trail gains a value ten times a second. Drawn as it is, it would jump
// one place to the left as often, which looks like a low frame rate. So
// it is drawn moved by the part of a step that has passed since the last
// one, and one value at each end is kept out of view: the oldest leaves,
// and the newest comes in, over a whole step.
func (w *window) drawTrails(in ipc.Instrument, area rl.Rectangle) {
	seen := w.trails[trailKey(in)]
	if seen == nil || in.Trail == nil || len(in.Trail.Input) < 4 || len(in.Trail.MIDI) != len(in.Trail.Input) {
		return
	}
	count := len(in.Trail.Input)
	phase := TrailPhase(w.drawn.Sub(seen.at), time.Duration(in.Trail.StepMS)*time.Millisecond)
	// Both curves on one scale, so that they can be compared.
	most := float32(max(slices.Max(in.Trail.Input), slices.Max(in.Trail.MIDI), 20))
	beginClipWithin(area)
	defer endClipWithin()
	floor := area.Y + area.Height
	for i, values := range [][]int{in.Trail.Input, in.Trail.MIDI} {
		if slices.Max(values) == 0 {
			continue
		}
		ink := []RGB{colInput, colMIDI}[i]
		at := func(n int) rl.Vector2 {
			return rl.NewVector2(TrailX(n, count, phase, area.X, area.Width), floor-float32(values[n])/most*(area.Height-4))
		}
		for n := 1; n < count; n++ {
			if values[n-1] == 0 && values[n] == 0 {
				continue
			}
			a, b := at(n-1), at(n)
			// The area under the curve, faint, and the curve itself.
			rl.DrawTriangle(a, rl.NewVector2(a.X, floor), b, colA(ink, 22))
			rl.DrawTriangle(b, rl.NewVector2(a.X, floor), rl.NewVector2(b.X, floor), colA(ink, 22))
			rl.DrawLineEx(a, b, 1.5, colA(ink, 120))
		}
	}
}

// toggle asks HIDI to turn on or off.
func (w *window) toggle() {
	w.ask(func(ctx context.Context) { w.hidi.Toggle(ctx) })
}

// ask asks HIDI for something without making the drawing wait for the
// answer; the new state arrives like every other.
func (w *window) ask(what func(context.Context)) {
	select {
	case w.toggling <- struct{}{}:
	default:
		return // the last request is still on its way
	}
	go func() {
		defer func() { <-w.toggling }()
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
		defer cancel()
		what(ctx)
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
	w.cards.Target -= wheel(area) * 60
	w.cards.Clamp(total - area.Height)
	if w.cards.Step(w.dt) {
		w.moving = true
	}

	beginClip(area)
	defer endClip()
	y := area.Y - float32(int(w.cards.At))
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
	if w.isShown(w.shown.Input, EntryKey(in)) {
		h += w.inputHeight(in, inner) + 10
	}
	if w.isShown(w.shown.Output, EntryKey(in)) {
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
		setCursor(rl.MouseCursorPointingHand)
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
	// Behind the two lines of the card, how busy the instrument was.
	beginClipWithin(rl.NewRectangle(r.X+1, r.Y+1, r.Width-2, cardTop-2))
	w.drawTrails(in, rl.NewRectangle(r.X, r.Y+6, r.Width, cardTop-12))
	endClipWithin()
	rl.DrawRectangleRoundedLinesEx(r, 16/r.Height, 8, 1, col(colPanelEdge))
	x, y := r.X+cardPad, r.Y+12

	// In the corner of the card: the way to the instrument's profile,
	// and the switches for what is drawn of the instrument.
	right := r.X + r.Width - cardPad
	if in.ProfileFile != "" {
		label := "Edit profile"
		right -= width(w.sans, label, sizeText) + 20
		if hit, end := w.button(label, right, r.Y+9); hit {
			w.editProfile(in.ProfileFile)
		} else if w.unsaved(in.ProfileFile) {
			rl.DrawCircleV(rl.NewVector2(end-2, r.Y+11), 4.5, col(colUnsaved))
		}
		right -= 14
	}
	showInput, showOutput := w.isShown(w.shown.Input, EntryKey(in)), w.isShown(w.shown.Output, EntryKey(in))
	right -= w.chipWidth("output")
	if w.chip("output", showOutput, right, r.Y+11) {
		w.shown.Output = flip(w.shown.Output, EntryKey(in))
		w.saveShown()
	}
	right -= w.chipWidth("input") + 6
	if w.chip("input", showInput, right, r.Y+11) {
		w.shown.Input = flip(w.shown.Input, EntryKey(in))
		w.saveShown()
	}
	right -= width(w.sans, "show", sizeText) + 8
	text(w.sans, "show", right, r.Y+13, sizeText, col(colFaint))
	if isKeyboard(in) {
		w.drawSign(in, right-12, r.Y+10)
		right -= 12 + 18
	}
	// The name and what the device is have the room that is left.
	room := right - 14 - x
	name := fit(w.bold, in.Name, sizeName, room)
	text(w.bold, name, x, y, sizeName, col(colText))
	if left := room - width(w.bold, name, sizeName) - 12; left > 40 {
		about := fit(w.sans, in.Type+" · profile "+in.Profile, sizeText, left)
		text(w.sans, about, x+width(w.bold, name, sizeName)+12, y+3, sizeText, col(colFaint))
	}

	// The numbers are set in the fixed-width font with a fixed number of
	// places, so that nothing moves sideways when one of them changes.
	y += 30
	for _, field := range []struct {
		label, value string
		busy         *RGB // something is going on: the number is tinted
	}{
		{"octave", fmt.Sprintf("%+3d", in.Octave), nil},
		{"transpose", fmt.Sprintf("%+4d", in.Transpose), nil},
		{"channel", fmt.Sprintf("%2d", in.Channel), nil},
		{"velocity", fmt.Sprintf("%3d", in.Velocity), nil},
		{"notes", fmt.Sprintf("%2d", in.Sounding), nil},
		{"input", fmt.Sprintf("%4d/s", in.InputRate), tint(in.InputRate, &colInputText)},
		{"MIDI", fmt.Sprintf("%4d/s", in.MIDIRate), tint(in.MIDIRate, &colMIDIText)},
		{"layout", in.Layout, nil},
		{"chord", in.Chord, nil},
		{"", map[bool]string{true: "learning"}[in.Learning], nil},
	} {
		if field.value == "" {
			continue // no chord remembered, not learning
		}
		text(w.sans, field.label, x, y, sizeText, col(colFaint))
		x += width(w.sans, field.label, sizeText) + 6
		ink := colText
		if field.busy != nil {
			ink = *field.busy
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

// drawSign draws the sign of the lights of a keyboard, the logo of
// OpenRGB, with its right edge at right. It is in colour where HIDI
// drives the keyboard's lights through OpenRGB and grey where it does
// not. A click on a grey one shows the lights in the window all the same,
// as they would be: the sign is yellow then, and a second click ends it.
func (w *window) drawSign(in ipc.Instrument, right, y float32) {
	const high = 21
	picture := w.signs[icon.RGBGrey]
	switch {
	case in.Lit:
		picture = w.signs[icon.RGBColour]
	case in.Simulated:
		picture = w.signs[icon.RGBYellow]
	}
	wide := high * float32(picture.Width) / float32(picture.Height)
	area := rl.NewRectangle(right-wide, y, wide, high)
	strength := uint8(255)
	if !in.Lit {
		if hovered(area) {
			setCursor(rl.MouseCursorPointingHand)
		} else if !in.Simulated {
			strength = 170
		}
		if clicked(area) {
			name, kind, location, on := in.Name, in.Type, in.Location, !in.Simulated
			// The lights are shown on the drawn keyboard, so that is
			// brought into view with them.
			if key := EntryKey(in); on && !slices.Contains(w.shown.Input, key) {
				w.shown.Input = append(slices.Clone(w.shown.Input), key)
				w.saveShown()
			}
			w.ask(func(ctx context.Context) { w.hidi.Simulate(ctx, name, kind, location, on) })
		}
	}
	rl.DrawTexturePro(picture, rl.NewRectangle(0, 0, float32(picture.Width), float32(picture.Height)), area, rl.NewVector2(0, 0), 0, rl.NewColor(255, 255, 255, strength))
}

// tint returns the colour for a rate that is not zero, and nil for one
// that is.
func tint(rate int, colour *RGB) *RGB {
	if rate > 0 {
		return colour
	}
	return nil
}

// unsaved reports whether a profile file has changes in the window that
// were not saved.
func (w *window) unsaved(path string) bool {
	e := w.docs[path]
	return e != nil && e.doc.Dirty()
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
	if in.Lit || in.Simulated {
		rest = ParseColour(in.LightRest)
		lights = make(map[string]RGB, len(in.Lights))
		for _, l := range in.Lights {
			lights[l.Name] = ParseColour(l.Colour)
		}
	}
	// light is the colour a key glows in; black for none.
	light := func(name string) RGB {
		if !(in.Lit || in.Simulated) {
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
		// What the key does shows at half strength here: the view is of
		// what is pressed, with the roles as a background to it.
		fill := Mix(colBar, KeyColour(role.Role, role.Note, in.Octave, in.Transpose), 0.5)
		if down[key.Name] && !(in.Lit || in.Simulated) {
			fill = pressed
		}
		rl.DrawRectangleRounded(rect(key), 0.25, 4, col(fill))
	}
	if in.Lit || in.Simulated {
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
		case (in.Lit || in.Simulated) && isDown:
			rl.DrawRectangleRoundedLinesEx(r, 0.25, 4, 2, col(pressed))
			fallthrough
		case (in.Lit || in.Simulated):
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

// loadPicture makes a texture of a picture that is drawn smaller than it
// is, and turned: smoothed, also when it is drawn much smaller.
func loadPicture(size int, at func(x, y int) rl.Color) rl.Texture2D {
	return loadPictureSize(size, size, at)
}

func loadPictureSize(wide, high int, at func(x, y int) rl.Color) rl.Texture2D {
	img := rl.GenImageColor(wide, high, rl.Blank)
	for py := range high {
		for px := range wide {
			rl.ImageDrawPixel(img, int32(px), int32(py), at(px, py))
		}
	}
	tex := rl.LoadTextureFromImage(img)
	rl.UnloadImage(img)
	rl.GenTextureMipmaps(&tex)
	rl.SetTextureFilter(tex, rl.FilterTrilinear)
	return tex
}

// loadGear makes the picture of the gear: white, to be drawn in any
// colour.
func loadGear() rl.Texture2D {
	const size = 128
	alpha := GearAlpha(size)
	return loadPicture(size, func(x, y int) rl.Color { return rl.NewColor(255, 255, 255, alpha[y*size+x]) })
}

// loadIcon makes the picture of HIDI's icon in the given colours.
func loadIcon(colours icon.Colours) rl.Texture2D {
	const size = 128
	img := icon.Render(size, colours)
	return loadPicture(size, func(x, y int) rl.Color {
		c := img.NRGBAAt(x, y)
		return rl.NewColor(c.R, c.G, c.B, c.A)
	})
}

// drawFloppy draws the floppy disk of the buttons that save, each of its
// sixteen points pixel wide and high.
func drawFloppy(x, y, pixel float32, c rl.Color) {
	for row := range 16 {
		for column := 0; column < 16; column++ {
			if !FloppyPoint(column, row) {
				continue
			}
			from := column
			for column+1 < 16 && FloppyPoint(column+1, row) {
				column++
			}
			rl.DrawRectangleRec(rl.NewRectangle(x+float32(from)*pixel, y+float32(row)*pixel, float32(column-from+1)*pixel, pixel), c)
		}
	}
}

// newest is what the window knows about newer releases of HIDI.
type newest struct {
	// on says that the user wants the window to ask, and check is what
	// asks; without it nothing is asked.
	on    bool
	check func(context.Context) (update.Result, error)

	mu      sync.Mutex
	asking  bool
	asked   time.Time
	result  *update.Result
	problem string
}

// look asks for the newest release if it is time to: when the window
// has not asked yet, a day after an answer, and an hour after a failure.
func (n *newest) look(ctx context.Context, now time.Time, done func()) {
	if !n.on || n.check == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	wait := update.Interval
	if n.problem != "" {
		wait = time.Hour
	}
	if n.asking || !n.asked.IsZero() && now.Sub(n.asked) < wait {
		return
	}
	n.asking, n.asked = true, now
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		result, err := n.check(ctx)
		n.mu.Lock()
		n.asking, n.problem = false, ""
		if err != nil {
			n.problem = err.Error()
		} else {
			n.result = &result
		}
		n.mu.Unlock()
		done()
	}()
}

// words returns what the corner of the status bar says about releases,
// whether that is news, and the page a click opens.
func (n *newest) words() (label string, news bool, page string) {
	if !n.on || n.check == nil {
		return "", false, ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case n.result != nil && n.result.Newer:
		return "update available: " + n.result.Newest, true, n.result.Page
	case n.result != nil && update.Known(n.result.Current):
		return "up to date", false, n.result.Page
	case n.result != nil:
		return "newest release: " + n.result.Newest, false, n.result.Page
	case n.problem != "":
		return "no word of updates", false, update.Project + "/releases"
	}
	return "looking for updates", false, update.Project + "/releases"
}

// drawUpdate draws what is known of newer releases at the right end of
// the status bar and returns how much of the bar that takes. A click
// opens the page of the release.
func (w *window) drawUpdate(bar rl.Rectangle) float32 {
	w.newest.look(w.ctx, time.Now(), w.model.Touch)
	label, news, page := w.newest.words()
	if label == "" {
		return 0
	}
	lw := width(w.sans, label, sizeText-1)
	area := rl.NewRectangle(bar.X+bar.Width-margin-lw-6, bar.Y+4, lw+12, bar.Height-8)
	ink := colA(colFaint, 150)
	if news {
		ink = col(colUnsaved)
	}
	if hovered(area) {
		setCursor(rl.MouseCursorPointingHand)
		if !news {
			ink = col(colFaint)
		}
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			if err := openPath(page); err != nil {
				w.settings.message = fmt.Sprintf("%s could not be opened: %v", page, err)
			}
		}
	}
	text(w.sans, label, area.X+6, bar.Y+8, sizeText-1, ink)
	return area.Width + 10
}

// drawLog draws the status bar with the last line of the log and, above
// it, the log itself as far as it is pulled up.
func (w *window) drawLog(area rl.Rectangle) {
	lines := w.model.Log()
	bar := rl.NewRectangle(area.X, area.Y, area.Width, barH)
	open := w.logHeight > 0

	// Pulling the bar up and down changes the height of the log; a click
	// on it opens and closes the log.
	if hovered(bar) || w.dragging {
		setCursor(rl.MouseCursorResizeNS)
	}
	// What is known of newer releases has the right end of the bar; a
	// click there is not a pull at the bar.
	label, _, _ := w.newest.words()
	corner := rl.NewRectangle(bar.X+bar.Width, bar.Y, 0, bar.Height)
	if label != "" {
		cw := width(w.sans, label, sizeText-1) + 12 + margin
		corner = rl.NewRectangle(bar.X+bar.Width-cw, bar.Y, cw, bar.Height)
	}
	if clicked(bar) && !rl.CheckCollisionPointRec(mouse(), corner) {
		w.dragging, w.dragMoved = true, false
	}
	if w.dragging {
		if delta := rl.GetMouseDelta().Y / uiScale; delta != 0 {
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
			case w.logHeight < filterH+2*lineH:
				w.logHeight = 0 // too little to read: closed
			default:
				w.savedHeight = w.logHeight
			}
		}
	}

	rl.DrawRectangleRec(area, col(colBar))
	rl.DrawRectangleRec(rl.NewRectangle(area.X, area.Y, area.Width, 1), col(colPanelEdge))

	// The bar always says the same: the last line, in its colours but
	// faint, so that it informs without drawing the eye, and what is
	// known of newer releases.
	taken := w.drawUpdate(bar)
	if n := len(lines); n > 0 {
		w.drawLine(lines[n-1].Text, margin, area.Y+9, area.Width-2*margin-taken, 120)
	} else {
		text(w.mono, "no log yet", margin, area.Y+9, sizeMono, colA(colFaint, 120))
	}
	if !open {
		return
	}

	// Below the bar, the choice of what is shown, and then the log.
	strip := rl.NewRectangle(area.X, area.Y+barH, area.Width, filterH)
	body := rl.NewRectangle(area.X, strip.Y+filterH, area.Width, area.Height-barH-filterH)
	if body.Height <= 0 {
		return
	}
	rl.DrawRectangleRec(rl.NewRectangle(area.X, strip.Y, area.Width, 1), colA(colPanelEdge, 120))
	w.drawFilter(strip)
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
			setCursor(rl.MouseCursorPointingHand)
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
