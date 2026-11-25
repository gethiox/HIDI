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

	kit

	logHeight   float32 // of the open log, 0 when only the bar shows
	savedHeight float32 // what a click on the bar opens the log to
	dragging    bool
	dragMoved   bool
	logScroll   float32 // lines scrolled back from the newest
	cards       glide   // how far the instruments are scrolled
	toggling    chan struct{}

	// active says whether HIDI is on, as of the frame being drawn.
	active bool
	// dt is the time since the frame before, and drawn when that one
	// was drawn.
	dt    time.Duration
	drawn time.Time
	// arrived is when HIDI's state, as of the frame being drawn, came
	// in.
	arrived time.Time
	// docs are the profiles that were opened for changing, by their
	// files: what was changed in one and not saved is still there when
	// its page is opened again.
	docs map[string]*editor
	// profiles hands HIDI the profiles that are tried and not saved, by
	// their files, and tried the settings that are.
	profiles latest[string, string]
	tried    latest[struct{}, string]
	// names says that the controls of game controllers are shown by
	// name, not numbered.
	names bool
	// valueX is how far in the values of the settings begin.
	valueX float32
	// shown says what is drawn of which instrument.
	shown State
	// glow is the picture of a light under a key, cog that of the gear
	// and icons HIDI's icon while off, on, and in the other half of the
	// blinking of learning mode.
	glow  rl.Texture2D
	cog   rl.Texture2D
	icons [3]rl.Texture2D
	// signs are the logo of OpenRGB in its three looks, by icon.RGBLook.
	signs [3]rl.Texture2D
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
	w.scale = 1
	w.profiles.hand = func(file, text string) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		hidi.Preview(ctx, file, text) // no HIDI, nothing to play with
	}
	w.tried.hand = func(_ struct{}, text string) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		hidi.TrySettings(ctx, text) // no HIDI, nothing to follow
	}
	if opts.Scale >= settings.MinScale && opts.Scale <= settings.MaxScale {
		w.scale = float32(opts.Scale) / 100
	}
	w.wantScale = w.scale
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
	w.cog = loadGear()
	defer rl.UnloadTexture(w.cog)
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
	// The settings the window shows: those of the file and, where a HIDI
	// runs that was handed changes which were not saved, those. The
	// running HIDI is what the settings are about; the file is only where
	// they are kept.
	w.settings.refresh(opts.Paths, opts.Ports, time.Now(), true)
	w.shown = LoadState(opts.Paths.Window())
	if w.settings.exists {
		ask, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		if text, err := hidi.Settings(ask); err == nil && text != "" {
			if s, err := settings.Parse([]byte(text)); err == nil {
				w.settings.edit.shown = clone(s)
			}
		}
		cancel()
	}

	w.savedHeight = 220
	if opts.LogHeight > 0 {
		w.logHeight, w.savedHeight = float32(opts.LogHeight), float32(opts.LogHeight)
	}

	pace := newPacer(opts.FPS, opts.BackgroundFPS)
	w.pace = pace
	// What the command line said about the window holds, unless settings
	// are in use that were changed since.
	w.newest.on, w.names = w.settings.edit.shown.Window.CheckUpdates, w.settings.edit.shown.Window.ControlNames
	if w.settings.edit.Dirty() {
		w.useLocal()
	}
	if opts.ShowSettings {
		w.gear.set(true, time.Now())
	}
	if opts.EditProfile != "" {
		w.editProfile(opts.EditProfile, "")
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
			pace.idle()
			rl.PollInputEvents()
			w.applyCursor()
			time.Sleep(min(time.Second/time.Duration(pace.target), idlePoll))
			continue
		}
		settle--
		wide, high := w.begin()
		w.draw(wide, high)
		w.end()
		pace.shown(time.Now())
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

	// unsynced says that frames were seen not to wait for the monitor;
	// see FrameRate. last is when the frame before was shown, zero after
	// a pause, and fast counts frames in a row that came sooner than a
	// waiting frame could.
	unsynced bool
	last     time.Time
	fast     int
}

func newPacer(limit, background int) *pacer { return &pacer{limit: limit, background: background} }

// want returns the limit in force: the one for a window that is not in
// use where that is lower.
func (p *pacer) want() int {
	limit := p.limit
	if p.background > 0 && !rl.IsWindowFocused() && (limit == 0 || p.background < limit) {
		limit = p.background
	}
	return limit
}

func (p *pacer) frame() {
	// The window may have been moved to another monitor.
	if now := time.Now(); now.Sub(p.checked) > time.Second {
		p.checked = now
		refresh := int32(rl.GetMonitorRefreshRate(rl.GetCurrentMonitor()))
		if refresh <= 0 {
			refresh = 60
		}
		if refresh != p.refresh {
			p.refresh, p.unsynced, p.fast = refresh, false, 0
		}
	}
	if want := int32(FrameRate(int(p.refresh), p.want(), !p.unsynced)); want != p.target {
		p.target = want
		rl.SetTargetFPS(want)
	}
}

// shown is told of every frame that was shown, and idle of every pause.
// From the time between two frames it learns whether frames wait for the
// monitor: a limited frame that does comes no sooner than the monitor's
// pictures allow.
func (p *pacer) shown(now time.Time) {
	limit := p.want()
	if !p.last.IsZero() && !p.unsynced && limit > 0 && int32(limit) < p.refresh {
		every := time.Second / time.Duration(limit)
		if now.Sub(p.last) < every*8/10 {
			if p.fast++; p.fast >= 20 {
				p.unsynced = true
			}
		} else {
			p.fast = 0
		}
	}
	p.last = now
}

func (p *pacer) idle() { p.last = time.Time{} }

// draw draws the window, which is sw wide and sh high.
func (w *window) draw(sw, sh float32) {
	rl.ClearBackground(col(colBackground))
	state, arrived := w.model.SnapshotAt()
	w.arrived = arrived
	// What moves by itself moves by the time since the last frame. After
	// a pause in which nothing was drawn that is one frame's worth.
	now := time.Now()
	w.dt = min(now.Sub(w.drawn), 250*time.Millisecond)
	w.drawn = now
	w.follow(state)

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
	w.deaf = w.gear.open
	if w.edit != nil {
		w.drawEditor(state, body)
	} else {
		w.drawInstruments(state, body)
	}
	w.deaf = false
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
	w.text(w.title, "HIDI", nameX, 20, sizeTitle, col(colText))
	x := nameX + width(w.title, "HIDI", sizeTitle) + 18

	if state == nil {
		w.text(w.sans, "HIDI is not running. Start it with \"hidi\".", x, 27, sizeText, col(colFaint))
		return
	}
	// The switch: a pill with a knob, and the state in words.
	pill := rl.NewRectangle(x, 20, 52, 26)
	label := "off"
	if state.Active {
		label = "on"
	}
	if w.toggle(pill, state.Active, colOn, pill) {
		w.switchHIDI()
	}
	w.text(w.bold, label, pill.X+pill.Width+10, 23, sizeName, col(colText))
	// What follows stands where it stands, whichever word is shown.
	hx := pill.X + pill.Width + 10 + max(width(w.bold, "on", sizeName), width(w.bold, "off", sizeName))

	// Learning mode: a smaller switch with a fainter word, yellow while
	// it is on.
	small := rl.NewRectangle(hx+26, 24, 34, 18)
	word := "learning mode"
	area := rl.NewRectangle(small.X, small.Y, small.Width+8+width(w.sans, word, sizeText-1), small.Height)
	ink := colOff
	if state.Learning {
		ink = colUnsaved
	}
	if w.hovered(area) {
		ink = Mix(ink, colText, 0.4)
	}
	if w.toggle(small, state.Learning, colUnsaved, area) {
		w.ask(func(ctx context.Context) { w.hidi.ToggleLearning(ctx) })
	}
	w.text(w.sans, word, small.X+small.Width+8, 26, sizeText-1, col(ink))

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
		w.text(w.mono, version, vx, 28, sizeMono, col(colFaint))
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
		w.text(w.mono, number, rx, 28, sizeMono, col(ink))
		rx -= width(w.sans, rate.label, sizeText) + 4
		w.text(w.sans, rate.label, rx, 27, sizeText, col(colFaint))
		rx -= 16
	}
}

// follow keeps the window drawing while a curve has something in it:
// the curves run on between two steps.
func (w *window) follow(state *ipc.Snapshot) {
	if state == nil {
		return
	}
	for _, in := range state.Instruments {
		if in.Trail != nil && (slices.Max(in.Trail.Input) > 0 || slices.Max(in.Trail.MIDI) > 0) {
			w.moving = true
			return
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
	if in.Trail == nil || len(in.Trail.Input) < 4 || len(in.Trail.MIDI) != len(in.Trail.Input) {
		return
	}
	count := len(in.Trail.Input)
	// How far the time is between two steps: by the age HIDI gave the
	// newest step, and the time since HIDI said so. When a step was
	// first seen here would not do: that is a frame late at the most,
	// and at fifteen frames a second most of a step.
	age := time.Duration(in.Trail.AgeMS*float64(time.Millisecond)) + w.drawn.Sub(w.arrived)
	phase := TrailPhase(age, time.Duration(in.Trail.StepMS)*time.Millisecond)
	// Both curves on one scale, so that they can be compared.
	most := float32(max(slices.Max(in.Trail.Input), slices.Max(in.Trail.MIDI), 20))
	w.clip(area)
	defer w.unclip()
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
func (w *window) switchHIDI() {
	w.ask(func(ctx context.Context) { w.hidi.Toggle(ctx) })
}

// ask asks HIDI for something without making the drawing wait for the
// answer; the new state arrives like every other.
// reload asks the running HIDI to look at its profile files now, after the
// window wrote or removed one: by itself HIDI notices within a second or
// two, which is long to wait for a click to show.
func (w *window) reload() {
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
		defer cancel()
		w.hidi.Reload(ctx)
	}()
}

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
		w.text(w.sans, "No instruments. Connect a keyboard or a game controller.", margin, area.Y+8, sizeText, col(colFaint))
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
	w.cards.Target -= w.wheel(area) * 60
	w.cards.Clamp(total - area.Height)
	if w.cards.Step(w.dt) {
		w.moving = true
	}

	w.clip(area)
	defer w.unclip()
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
	if w.isShown(w.shown.ShowInput, EntryKey(in)) {
		h += w.inputHeight(in, inner) + 10
	}
	if w.isShown(w.shown.ShowOutput, EntryKey(in)) {
		h += w.sendsHeight(in, inner) + pianoH + 12
	}
	return h
}

func (w *window) isShown(list []string, name string) bool {
	return w.opts.ShowAll || slices.Contains(list, name)
}

func (w *window) drawCard(in ipc.Instrument, r rl.Rectangle) {
	rl.DrawRectangleRounded(r, 16/r.Height, 8, col(colPanel))
	// Behind the two lines of the card, how busy the instrument was.
	w.clip(rl.NewRectangle(r.X+1, r.Y+1, r.Width-2, cardTop-2))
	w.drawTrails(in, rl.NewRectangle(r.X, r.Y+6, r.Width, cardTop-12))
	w.unclip()
	rl.DrawRectangleRoundedLinesEx(r, 16/r.Height, 8, w.hair(), col(colPanelEdge))
	x, y := r.X+cardPad, r.Y+12

	// In the corner of the card: the way to the instrument's profile,
	// and the switches for what is drawn of the instrument.
	right := r.X + r.Width - cardPad
	if in.ProfileFile != "" {
		label := "Edit profile"
		right -= width(w.sans, label, sizeText) + 20
		if hit, end := w.button(label, right, r.Y+9); hit {
			w.editProfile(in.ProfileFile, EntryKey(in))
		} else if w.unsaved(in.ProfileFile) {
			rl.DrawCircleV(rl.NewVector2(end-2, r.Y+11), 4.5, col(colUnsaved))
		}
		right -= 14
	}
	showInput, showOutput := w.isShown(w.shown.ShowInput, EntryKey(in)), w.isShown(w.shown.ShowOutput, EntryKey(in))
	right -= w.chipWidth("output")
	if w.chip("output", showOutput, right, r.Y+11) {
		w.shown.ShowOutput = flip(w.shown.ShowOutput, EntryKey(in))
		w.saveShown()
	}
	right -= w.chipWidth("input") + 6
	if w.chip("input", showInput, right, r.Y+11) {
		w.shown.ShowInput = flip(w.shown.ShowInput, EntryKey(in))
		w.saveShown()
	}
	right -= width(w.sans, "show", sizeText) + 8
	w.text(w.sans, "show", right, r.Y+13, sizeText, col(colFaint))
	if isKeyboard(in) {
		w.drawSign(in, right-12, r.Y+10)
		right -= 12 + 18
	}
	// The name and what the device is have the room that is left.
	room := right - 14 - x
	name := fit(w.bold, in.Name, sizeName, room)
	w.text(w.bold, name, x, y, sizeName, col(colText))
	if left := room - width(w.bold, name, sizeName) - 12; left > 40 {
		about := fit(w.sans, in.Type+" · profile "+in.Profile, sizeText, left)
		ax := x + width(w.bold, name, sizeName) + 12
		w.text(w.sans, about, ax, y+3, sizeText, col(colFaint))
		if left -= width(w.sans, about, sizeText) + 12; in.Typing && left > 60 {
			// A keyboard HIDI does not take from the system, and why.
			note := fit(w.sans, "types while it plays: its profile has no hidi_toggle", sizeText, left)
			w.text(w.sans, note, ax+width(w.sans, about, sizeText)+12, y+3, sizeText, col(colUnsaved))
		}
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
		w.text(w.sans, field.label, x, y, sizeText, col(colFaint))
		x += width(w.sans, field.label, sizeText) + 6
		ink := colText
		if field.busy != nil {
			ink = *field.busy
		}
		w.text(w.mono, field.value, x, y+1, sizeMono, col(ink))
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
		w.drawSends(in, r.X+cardPad, y, inner)
		y += w.sendsHeight(in, inner)
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
		if w.hovered(area) {
			w.setCursor(rl.MouseCursorPointingHand)
		} else if !in.Simulated {
			strength = 170
		}
		if w.clicked(area) {
			name, kind, location, on := in.Name, in.Type, in.Location, !in.Simulated
			// The lights are shown on the drawn keyboard, so that is
			// brought into view with them.
			if key := EntryKey(in); on && !slices.Contains(w.shown.ShowInput, key) {
				w.shown.ShowInput = append(slices.Clone(w.shown.ShowInput), key)
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

// saveShown notes what is shown of which instrument for the next time the
// window opens, among what HIDI keeps for itself. It is no setting, and
// the settings file is not touched.
func (w *window) saveShown() { w.shown.Save(w.opts.Paths.Window()) }

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
				w.text(w.mono, label, k.X+(k.W-width(w.mono, label, 9))/2, k.Y+k.H-11, 9, col(colOff))
			}
		}
	}
}

// A keyboard is drawn as one; every other device as the list of what it
// reported.
func isKeyboard(in ipc.Instrument) bool { return in.Type == "keyboard" }

// An axis is drawn as a line of its own: its name, a bar that fills from
// the left, and its value.
const (
	axisBarMin = 64 // the least width of the bar
	axisBarH   = 8
	axisSize   = 12 // of the lettering
	axisGap    = 22 // between two axes beside each other
)

// chipRows counts the rows the buttons take when they are set one after
// another across the width.
func (w *window) chipRows(in ipc.Instrument, inner float32) int {
	if len(in.Buttons) == 0 {
		return 0
	}
	row := newFlow(0, inner, 0, 1, 6)
	for i := range in.Buttons {
		row.put(w.chipWidth(ButtonLabel(in, i, w.names)))
	}
	return row.lines
}

// axisLayout says how the axes of an instrument are set out: how many
// beside each other, how wide each of them, and how wide the names.
func (w *window) axisLayout(in ipc.Instrument, inner float32) (cols int, cell, label float32) {
	for i := range in.Axes {
		label = max(label, width(w.mono, AxisLabel(in, i, w.names), axisSize))
	}
	least := label + 8 + axisBarMin + 8 + width(w.mono, "0.00", axisSize) + axisGap
	cols = max(int((inner+axisGap)/least), 1)
	return cols, (inner + axisGap) / float32(cols), label
}

// sendsLayout says how the controllers an instrument sends are set out,
// as axisLayout does for the axes.
func (w *window) sendsLayout(in ipc.Instrument, inner float32) (cols int, cell, label, value float32) {
	for _, c := range in.Controllers {
		label = max(label, width(w.mono, ControllerLabel(c, in.Channel), axisSize))
	}
	value = width(w.mono, "+100%", axisSize)
	least := label + 8 + axisBarMin + 8 + value + axisGap
	cols = max(int((inner+axisGap)/least), 1)
	return cols, (inner + axisGap) / float32(cols), label, value
}

// sendsHeight is the height the controllers an instrument sends take,
// with the room below them.
func (w *window) sendsHeight(in ipc.Instrument, inner float32) float32 {
	n := len(in.Controllers)
	if n == 0 {
		return 0
	}
	cols, _, _, _ := w.sendsLayout(in, inner)
	return float32((n+cols-1)/cols)*axisRowH + 6
}

// drawSends draws what the instrument sends by the position of its
// controls, as MIDI has it: every controller and the pitch bend with a
// bar and the value last sent, in the order of the controls that drive
// them, which is the order of the axes above.
func (w *window) drawSends(in ipc.Instrument, x, y, inner float32) {
	cols, cell, labelW, valueW := w.sendsLayout(in, inner)
	for i, c := range in.Controllers {
		cx := x + float32(i%cols)*cell
		cy := y + float32(i/cols)*axisRowH
		label := ControllerLabel(c, in.Channel)
		w.text(w.mono, label, cx+labelW-width(w.mono, label, axisSize), cy+3, axisSize, col(colFaint))
		bar := rl.NewRectangle(float32(int(cx+labelW+8)), cy+(axisRowH-axisBarH)/2, cell-axisGap-labelW-16-valueW, axisBarH)
		rl.DrawRectangleRounded(bar, 1, 4, col(colBar))
		from, to := ControllerLevel(c)
		if filled := bar.Width * float32(to-from); filled >= 1 {
			rl.DrawRectangleRounded(rl.NewRectangle(bar.X+bar.Width*float32(from), bar.Y, filled, bar.Height), 1, 4, col(colMIDI))
		}
		if c.Bend {
			// No bend is in the middle.
			rl.DrawRectangleRec(rl.NewRectangle(bar.X+bar.Width/2, bar.Y-2, 1, bar.Height+4), col(colOff))
		}
		value := ControllerValue(c)
		w.text(w.mono, value, bar.X+bar.Width+8+valueW-width(w.mono, value, axisSize), cy+3, axisSize, col(colText))
	}
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
		w.text(w.sans, "Press a button or move a stick to see it here.", x, y+2, sizeText, col(colFaint))
		return
	}
	// The buttons, one after another.
	row := newFlow(x, x+inner, y, chipH+6, 6)
	for i, b := range in.Buttons {
		label := ButtonLabel(in, i, w.names)
		cw := w.chipWidth(label)
		cx, cy := row.put(cw)
		chip := rl.NewRectangle(cx, cy, cw, chipH)
		if b.Down {
			rl.DrawRectangleRounded(chip, 0.4, 6, col(w.pressed()))
			w.text(w.mono, label, cx+7, cy+3, sizeMono, col(colBar))
		} else {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, w.hair(), col(colPanelEdge))
			w.text(w.mono, label, cx+7, cy+3, sizeMono, col(colFaint))
		}
	}
	cy := row.y
	if !row.empty() {
		cy += chipH + 6
	}
	// The axes: the name, a bar for the position, and the number.
	cols, cell, labelW := w.axisLayout(in, inner)
	valueW := width(w.mono, "0.00", axisSize)
	for i, a := range in.Axes {
		ax := x + float32(i%cols)*cell
		ay := cy + float32(i/cols)*axisRowH
		label := AxisLabel(in, i, w.names)
		w.text(w.mono, label, ax+labelW-width(w.mono, label, axisSize), ay+3, axisSize, col(colFaint))
		bar := rl.NewRectangle(float32(int(ax+labelW+8)), ay+(axisRowH-axisBarH)/2, cell-axisGap-labelW-16-valueW, axisBarH)
		rl.DrawRectangleRounded(bar, 1, 4, col(colBar))
		value := float32(min(max(a.Value, 0), 1))
		if filled := bar.Width * value; filled >= 1 && a.Known {
			rl.DrawRectangleRounded(rl.NewRectangle(bar.X, bar.Y, filled, bar.Height), 1, 4, col(w.pressed()))
		}
		// A mark in the middle, where a stick rests.
		rl.DrawRectangleRec(rl.NewRectangle(bar.X+bar.Width/2, bar.Y-2, 1, bar.Height+4), col(colOff))
		// An axis the device listed and has said nothing of stands
		// nowhere that is known.
		shown, ink := fmt.Sprintf("%.2f", a.Value), colText
		if !a.Known {
			shown, ink = "  – ", colFaint
		}
		w.text(w.mono, shown, bar.X+bar.Width+8, ay+3, axisSize, col(ink))
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
			w.text(w.mono, key.Label, r.X+(r.Width-width(w.mono, key.Label, labelSize))/2, r.Y+(r.Height-labelSize)/2, labelSize, col(ink))
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
	w.text(w.sans, "other keys", x, oy, sizeText, col(colFaint))
	ox := x + width(w.sans, "other keys", sizeText) + 10
	for _, name := range others {
		cw := w.chipWidth(name)
		if ox+cw > x+inner {
			break
		}
		rl.DrawRectangleRounded(rl.NewRectangle(ox, oy-2, cw, chipH), 0.4, 6, col(pressed))
		w.text(w.mono, name, ox+7, oy+1, sizeMono, col(colBar))
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
	if w.hovered(area) {
		w.setCursor(rl.MouseCursorPointingHand)
		if !news {
			ink = col(colFaint)
		}
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			if err := openPath(page); err != nil {
				w.settings.message = fmt.Sprintf("%s could not be opened: %v", page, err)
			}
		}
	}
	w.text(w.sans, label, area.X+6, bar.Y+8, sizeText-1, ink)
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
	if w.hovered(bar) || w.dragging {
		w.setCursor(rl.MouseCursorResizeNS)
	}
	// What is known of newer releases has the right end of the bar; a
	// click there is not a pull at the bar.
	label, _, _ := w.newest.words()
	corner := rl.NewRectangle(bar.X+bar.Width, bar.Y, 0, bar.Height)
	if label != "" {
		cw := width(w.sans, label, sizeText-1) + 12 + margin
		corner = rl.NewRectangle(bar.X+bar.Width-cw, bar.Y, cw, bar.Height)
	}
	if w.clicked(bar) && !rl.CheckCollisionPointRec(w.mouse(), corner) {
		w.dragging, w.dragMoved = true, false
	}
	if w.dragging {
		if delta := w.dragged().Y; delta != 0 {
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
		w.text(w.mono, "no log yet", margin, area.Y+9, sizeMono, colA(colFaint, 120))
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
	w.logScroll += w.wheel(body) * 3
	w.logScroll = min(max(w.logScroll, 0), float32(max(len(lines)-visible, 0)))

	w.clip(body)
	defer w.unclip()
	// The newest line sits at the bottom.
	last := len(lines) - int(w.logScroll)
	y := body.Y + body.Height - lineH - 4
	for i := last - 1; i >= 0 && y > body.Y-lineH; i-- {
		w.drawLine(lines[i].Text, margin, y, body.Width-2*margin, 255)
		y -= lineH
	}
	if len(lines) == 0 {
		w.text(w.mono, "nothing at this level yet", margin, body.Y+8, sizeMono, col(colFaint))
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
	w.text(w.sans, "Log", margin, bar.Y+9, sizeText, col(colFaint))
	chosen := w.model.MinLevel()
	levelColours := []RGB{AnsiColour(90), AnsiColour(32), AnsiColour(33), AnsiColour(91)}
	buttons := w.filterButtons(bar)
	label := "show from"
	w.text(w.sans, label, buttons[0].X-width(w.sans, label, sizeText)-10, bar.Y+9, sizeText, col(colFaint))
	for i, b := range buttons {
		if w.hovered(b) {
			w.setCursor(rl.MouseCursorPointingHand)
		}
		if w.clicked(b) {
			w.model.SetMinLevel(i)
			w.logScroll = 0
			chosen = i
		}
		if i >= chosen {
			rl.DrawRectangleRounded(b, 0.4, 6, colA(levelColours[i], 50))
			w.text(w.mono, Levels[i], b.X+8, b.Y+3, sizeMono, col(levelColours[i]))
		} else {
			rl.DrawRectangleRoundedLinesEx(b, 0.4, 6, w.hair(), col(colPanelEdge))
			w.text(w.mono, Levels[i], b.X+8, b.Y+3, sizeMono, col(colOff))
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
		w.text(w.mono, string(s), x, y, sizeMono, colA(AnsiColour(span.Fg.Code), alpha))
		x += float32(len(s)) * charW
		room -= len(s)
	}
}
