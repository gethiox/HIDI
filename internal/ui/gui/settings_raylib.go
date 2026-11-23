//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/config"
	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/settings"
)

// settingsView is what the settings page shows: the settings file and the
// profiles as they are on disk, read anew every so often while the page is
// open, since they may be changed by hand meanwhile.
type settingsView struct {
	loaded time.Time
	exists bool // HIDI's directory is in use
	// edit holds the settings as the file has them and as the sheet
	// shows them; they differ until the user saves.
	edit     pending
	problem  string // what is wrong with the settings file
	profiles []*config.Profile
	broken   []config.Problem
	message  string // the outcome of the last thing the user did here
	scroll   glide
	content  float32 // the height of the page when it was last drawn
	// capture is the toggle combination being taken from the keys the
	// user presses, or nil.
	capture *capture
	// ports are the MIDI ports that exist, and portsErr why they cannot
	// be listed.
	ports    []output.Port
	portsErr string
	// colour is the name of the colour of the lights that is being
	// changed, and slide the slider that is held: 1, 2 or 3 for red,
	// green and blue, 0 for none.
	colour string
	slide  int
	// typing says that the address of the OpenRGB server is being
	// typed.
	typing bool
}

const settingsRefresh = 2 * time.Second

func (v *settingsView) refresh(p settings.Paths, list func() ([]output.Port, error), now time.Time, force bool) {
	if !force && (now.Sub(v.loaded) < settingsRefresh || v.slide != 0) {
		return // a colour that is being changed is not on disk yet
	}
	v.loaded = now
	v.ports, v.portsErr = nil, ""
	if list != nil {
		ports, err := list()
		if err != nil {
			v.portsErr = err.Error()
		}
		for _, port := range ports {
			// HIDI's own instruments are not somewhere to send to.
			if !strings.HasPrefix(port.Name, "HIDI ") {
				v.ports = append(v.ports, port)
			}
		}
	}
	v.exists = p.Root != "" && settings.Managed(p)
	v.problem, v.profiles, v.broken = "", nil, nil
	if !v.exists {
		v.edit.load(settings.Default())
		return
	}
	s, err := settings.Load(p.Settings)
	if err != nil {
		v.problem = err.Error()
	}
	v.edit.load(s)
	v.profiles, v.broken = config.LoadKinds(p.Factory, p.User)
}

// drawGear draws the wheel that opens the settings and handles the click
// on it. It is faint, brighter under the pointer, and turns while the
// settings are open.
func (w *window) drawGear(sw float32) {
	now := time.Now()
	const size = 30
	area := rl.NewRectangle(sw-margin-size, (headerH-size)/2, size, size)
	over := hovered(area)
	if over {
		setCursor(rl.MouseCursorPointingHand)
	}
	if clicked(area) {
		w.gear.set(!w.gear.open, now)
		if w.gear.open {
			w.settings.message, w.settings.capture = "", nil
			w.settings.refresh(w.opts.Paths, w.opts.Ports, now, true)
		}
	}
	angle := float32(w.gear.advance(now))

	ink := colA(colFaint, 110)
	switch {
	case over:
		ink = col(colText)
	case w.gear.open:
		ink = col(colFaint)
	}
	// The gear is a picture, turned; raylib turns clockwise for growing
	// angles.
	picture := w.wheel
	rl.DrawTexturePro(picture, rl.NewRectangle(0, 0, float32(picture.Width), float32(picture.Height)),
		rl.NewRectangle(area.X+size/2, area.Y+size/2, size, size), rl.NewVector2(size/2, size/2), angle, ink)
	// Settings that were changed and not saved wait behind it.
	if !w.gear.open && w.settings.exists && w.settings.edit.Dirty() {
		rl.DrawCircleV(rl.NewVector2(area.X+size-2, area.Y+3), 4.5, col(colUnsaved))
	}
}

// button draws a button and reports a click on it.
func (w *window) button(label string, x, y float32) (clickedNow bool, right float32) {
	bw := width(w.sans, label, sizeText) + 20
	r := rl.NewRectangle(x, y, bw, 24)
	if hovered(r) {
		setCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanelEdge))
	} else {
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanel))
		rl.DrawRectangleRoundedLinesEx(r, 0.35, 6, hair(), col(colPanelEdge))
	}
	text(w.sans, label, x+10, y+5, sizeText, col(colText))
	return clicked(r), x + bw
}

// pill draws an on/off switch and reports a click on it.
func (w *window) pill(on bool, x, y float32) bool {
	return w.pillOver(on, x, y, rl.NewRectangle(x, y, 40, 20))
}

// pillOver is pill with the area that takes the pointer given: the switch
// and, for one with words beside it, the words.
func (w *window) pillOver(on bool, x, y float32, area rl.Rectangle) bool {
	r := rl.NewRectangle(x, y, 40, 20)
	back, knob := colOff, r.X+10
	if on {
		back, knob = colOn, r.X+r.Width-10
	}
	over := hovered(area)
	if over {
		setCursor(rl.MouseCursorPointingHand)
		back = Mix(back, colText, 0.22)
	}
	rl.DrawRectangleRounded(r, 1, 12, col(back))
	rl.DrawCircle(int32(knob), int32(r.Y+10), 7, col(colText))
	return clicked(area)
}

// hot lays a light over something that can be clicked while the pointer
// is on it, and asks for the hand.
func hot(r rl.Rectangle, roundness float32) bool {
	if !hovered(r) {
		return false
	}
	setCursor(rl.MouseCursorPointingHand)
	rl.DrawRectangleRounded(r, roundness, 6, rl.NewColor(255, 255, 255, 26))
	return true
}

func (w *window) open(path string) {
	if err := openPath(path); err != nil {
		w.settings.message = fmt.Sprintf("%s could not be opened: %v", path, err)
	} else {
		w.settings.message = ""
	}
}

// set takes a change to the settings. It is in use at once: this window
// follows what is its own, and the running HIDI is given the settings as
// they are shown. Nothing is written; that is what saving is for.
func (w *window) set(s settings.Settings) {
	w.settings.edit.shown = clone(s)
	w.settings.message = ""
	w.useSettings()
}

// useSettings makes the window and the running HIDI behave by the settings as
// the sheet shows them, saved or not.
func (w *window) useSettings() {
	s := w.settings.edit.shown
	w.pace.limit, w.pace.background = s.Window.FPS, s.Window.BackgroundFPS
	if s.Window.Scale >= settings.MinScale && s.Window.Scale <= settings.MaxScale {
		w.wantScale = float32(s.Window.Scale) / 100
	}
	w.newest.on = s.Window.CheckUpdates
	// Settings with a mistake in them, an address half typed for one,
	// wait until they are right.
	if len(s.Check()) > 0 {
		return
	}
	out := clone(s)
	out.Shown = w.shown
	w.trySettings(string(out.Text()))
}

// trySettings gives the running HIDI settings to behave by. The asking
// is done beside the drawing, the newest text last.
func (w *window) trySettings(text string) {
	w.tryMu.Lock()
	w.tryText, w.tryWaiting = text, true
	idle := !w.trySending
	w.trySending = true
	w.tryMu.Unlock()
	if !idle {
		return // the one at work takes it along
	}
	go func() {
		for {
			w.tryMu.Lock()
			if !w.tryWaiting {
				w.trySending = false
				w.tryMu.Unlock()
				return
			}
			text := w.tryText
			w.tryWaiting = false
			w.tryMu.Unlock()
			ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
			w.hidi.TrySettings(ctx, text) // no HIDI, nothing to follow
			cancel()
		}
	}()
}

// saveSettings writes the settings as they are shown and says what came
// of it.
func (w *window) saveSettings() {
	v := &w.settings
	if !v.edit.Dirty() {
		return
	}
	// What the window keeps of its own accord is as the window has it.
	out := clone(v.edit.shown)
	out.Shown = w.shown
	if err := settings.Save(w.opts.Paths.Settings, out); err != nil {
		v.message = strings.ReplaceAll(err.Error(), "\n", " ")
		return
	}
	v.message = soonNote
	if v.edit.restart() {
		v.message = restartNote
	}
	v.edit.disk = clone(v.edit.shown)
	v.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
}

// What the page says after saving, by when a running HIDI follows.
const (
	soonNote    = "Saved."
	restartNote = "Saved. Whether HIDI starts on and with its window, and the MIDI ports it also sends to, are followed when it is started again."
)

// drawOverlay draws the settings as a sheet over the body of the window:
// smaller than it, so that the instruments show around it, and not quite
// opaque, so that they show through. A click beside the sheet closes it.
func (w *window) drawOverlay(state *ipc.Snapshot, body rl.Rectangle) {
	inset := min(max(body.Width*0.05, 14), 60)
	sheet := rl.NewRectangle(body.X+inset, body.Y+6, body.Width-2*inset, body.Height-18)
	if sheet.Height < 60 {
		sheet = rl.NewRectangle(body.X, body.Y, body.Width, body.Height)
	}
	rl.DrawRectangleRec(body, rl.NewColor(0, 0, 0, 70))
	roundness := 20 / min(sheet.Width, sheet.Height)
	rl.DrawRectangleRounded(sheet, roundness, 8, colA(colBar, 238))
	rl.DrawRectangleRoundedLinesEx(sheet, roundness, 8, hair(), col(colPanelEdge))
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && hovered(body) && !hovered(sheet) {
		w.gear.set(false, time.Now())
		return
	}
	// Saving has the upper right corner of the sheet, over the page that
	// scrolls below it, which is deaf where the buttons are.
	v := &w.settings
	dirty := v.exists && v.problem == "" && v.edit.Dirty()
	const disk = 32
	button := rl.NewRectangle(sheet.X+sheet.Width-disk-22, sheet.Y+12, disk+12, disk+12)
	drop := rl.NewRectangle(button.X-70, button.Y+8, 62, 28)
	corner := rl.NewRectangle(drop.X-8, sheet.Y, sheet.X+sheet.Width-drop.X+8, button.Height+22)
	inputOff = hovered(corner)
	w.drawSettings(state, rl.NewRectangle(sheet.X+2, sheet.Y+2, sheet.Width-4, sheet.Height-4))
	inputOff = false

	ink := col(colText)
	if dirty {
		ink = col(colUnsaved)
	}
	rl.DrawRectangleRounded(button, 0.25, 6, colA(colBar, 235))
	if dirty && hovered(button) {
		setCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(button, 0.25, 6, col(colPanelEdge))
	}
	drawFloppy(button.X+6, button.Y+6, disk/16, ink)
	control := rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)
	if dirty && (clicked(button) || control && rl.IsKeyPressed(rl.KeyS)) {
		w.saveSettings()
	}
	if dirty {
		rl.DrawRectangleRounded(drop, 0.35, 6, colA(colBar, 235))
		if hit, _ := w.button("Revert", drop.X, drop.Y+2); hit {
			v.edit.drop()
			v.message = ""
			w.useSettings()
		}
	}
}

// toggleItem is a setting that is a yes or a no.
type toggleItem struct {
	name, about string
	value       *bool
}

// drawSettings draws the settings page: sections one below the other,
// each under a line across the sheet.
func (w *window) drawSettings(state *ipc.Snapshot, area rl.Rectangle) {
	v := &w.settings
	p := w.opts.Paths
	v.refresh(p, w.opts.Ports, time.Now(), false)

	// The page is as long as it was when last drawn, which is what the
	// scrolling is held to before anything is drawn.
	v.scroll.Target -= wheel(area) * 60
	v.scroll.Clamp(v.content - area.Height)
	if v.scroll.Step(w.dt) {
		w.moving = true
	}
	scrolled := float32(int(v.scroll.At))
	beginClip(area)
	defer endClip()

	x, y := area.X+margin, area.Y+12-scrolled
	right := area.X + area.Width - margin
	defer func() { v.content = y + scrolled - area.Y + 14 }()
	// Where the value of a setting begins, beside its name: less far in
	// on a narrow sheet.
	w.valueX = min(300, (right-x)*0.46)
	// section begins a group of settings: a line from edge to edge of
	// the sheet, and the name of the group.
	section := func(title string) {
		y += 4
		rl.DrawRectangleRec(rl.NewRectangle(area.X, y, area.Width, 1), col(colPanelEdge))
		rl.DrawRectangleRec(rl.NewRectangle(area.X, y+1, area.Width, 34), colA(colPanel, 150))
		text(w.bold, title, x, y+9, sizeName, col(colText))
		y += 50
	}
	// row draws the name of a setting and returns where its value goes.
	row := func(name, about string) float32 {
		text(w.sans, fit(w.sans, name, sizeText, w.valueX-10), x, y+2, sizeText, col(colText))
		if about != "" {
			text(w.sans, fit(w.sans, about, sizeText-2, w.valueX-10), x, y+20, sizeText-2, col(colFaint))
		}
		return x + w.valueX
	}

	text(w.bold, "Settings", x, y, sizeName, col(colText))
	y += 26
	if p.Root == "" {
		text(w.sans, "HIDI has no directory for settings here and uses what it comes with.", x, y, sizeText, col(colFaint))
		return
	}
	if !v.exists {
		text(w.sans, "HIDI's directory is not in use yet. HIDI creates it on its first start.", x, y, sizeText, col(colFaint))
		y += 30
	}
	if v.problem != "" {
		for _, line := range strings.Split("settings.toml has mistakes; the defaults are shown:\n"+v.problem, "\n") {
			text(w.sans, line, x, y, sizeText, col(AnsiColour(91)))
			y += 18
		}
		y += 8
	}
	// The outcome of the last change has a line of its own that is always
	// there, so that nothing below moves when it appears.
	message := v.message
	if message == "" && v.edit.Dirty() && v.exists {
		message = "In use now, and not saved: the disk in the corner keeps it, Revert takes it back."
	}
	text(w.sans, fit(w.sans, message, sizeText, right-x-130), x, y, sizeText, col(AnsiColour(93)))
	y += 28

	editable := v.exists && v.problem == ""
	s := clone(v.edit.shown)
	// switches draws settings that are a yes or a no: two beside each
	// other where the sheet is wide enough for that, one below the other
	// where it is not.
	switches := func(items ...toggleItem) {
		columns := 1
		if right-x >= 620 {
			columns = 2
		}
		cell := (right - x) / float32(columns)
		for i := 0; i < len(items); i += columns {
			high := float32(32)
			for j := i; j < min(i+columns, len(items)); j++ {
				item := items[j]
				if item.about != "" {
					high = 46
				}
				cx := x + float32(j-i)*cell
				room := cell - 50 - 16
				wide := max(width(w.sans, fit(w.sans, item.name, sizeText, room), sizeText), width(w.sans, fit(w.sans, item.about, sizeText-2, room), sizeText-2))
				if w.pillOver(*item.value, cx, y, rl.NewRectangle(cx, y-2, 50+wide, 24)) && editable {
					*item.value = !*item.value
					w.set(s)
				}
				text(w.sans, fit(w.sans, item.name, sizeText, room), cx+50, y+2, sizeText, col(colText))
				if item.about != "" {
					text(w.sans, fit(w.sans, item.about, sizeText-2, room), cx+50, y+20, sizeText-2, col(colFaint))
				}
			}
			y += high
		}
		y += 6
	}
	// choices draws a row of buttons of which one is chosen, and reports
	// the one that was clicked.
	choices := func(vx float32, chosen int, values []int, label func(int) string) (int, bool) {
		picked, hit := 0, false
		for _, value := range values {
			text := label(value)
			if vx+w.chipWidth(text) > right {
				break
			}
			if w.chip(text, chosen == value, vx, y) && editable && chosen != value {
				picked, hit = value, true
			}
			vx += w.chipWidth(text) + 6
		}
		return picked, hit
	}
	percent := func(n int) string { return fmt.Sprintf("%d%%", n) }

	// ---- Turning HIDI on and off.
	section("On and off")
	toggleNames := make([]string, len(s.Input.Toggle))
	for i, c := range s.Input.Toggle {
		toggleNames[i] = strings.TrimPrefix(c.String(), "KEY_")
	}
	vx := row("Toggle combination", "turns HIDI off and on")
	combination := strings.Join(toggleNames, " + ")
	if combination == "" {
		combination = "none"
	}
	if state == nil && v.capture != nil {
		// HIDI went away; what was pressed so far is not a combination.
		v.capture, v.message = nil, "HIDI stopped; the combination was not changed."
	}
	switch {
	case v.capture != nil:
		// The keys are taken from what the keyboards of the running HIDI
		// report, which is what the combination will be looked for in.
		if v.capture.step(keysDown(state)) {
			s.Input.Toggle = nil
			for _, name := range v.capture.keys {
				if code, err := control.ParseCode(name); err == nil {
					s.Input.Toggle = append(s.Input.Toggle, code)
				}
			}
			if len(s.Input.Toggle) != 1 {
				// A tap, and a key of HIDI's alone, are of one key.
				s.Input.ToggleTap, s.Input.ToggleExclusive = false, false
			}
			v.capture = nil
			w.set(s)
			break
		}
		pressed := "Press the keys together, then let go"
		if len(v.capture.keys) > 0 {
			names := make([]string, len(v.capture.keys))
			for i, name := range v.capture.keys {
				names[i] = strings.TrimPrefix(name, "KEY_")
			}
			pressed = strings.Join(names, " + ") + " …"
		}
		text(w.mono, pressed, vx, y+3, sizeMono, col(AnsiColour(93)))
		if hit, _ := w.button("Cancel", vx+width(w.mono, pressed, sizeMono)+14, y-2); hit {
			v.capture = nil
		}
	case state == nil:
		text(w.mono, combination, vx, y+3, sizeMono, col(colText))
		text(w.sans, "start HIDI to change it here", vx+width(w.mono, combination, sizeMono)+14, y+3, sizeText-2, col(colFaint))
	default:
		text(w.mono, combination, vx, y+3, sizeMono, col(colText))
		if hit, _ := w.button("Change", vx+width(w.mono, combination, sizeMono)+14, y-2); hit && editable {
			v.capture, v.message = &capture{}, ""
		}
	}
	y += 48
	switches(
		toggleItem{"The toggle is one key, tapped", "held longer, the key does what it normally does", &s.Input.ToggleTap},
		toggleItem{"The toggle key is HIDI's alone", "one key, such as Caps Lock, never reaches the system", &s.Input.ToggleExclusive},
		toggleItem{"HIDI is on when it starts", "followed at the next start", &s.Input.StartOn},
	)

	// ---- The devices.
	section("Devices")
	switches(
		toggleItem{"Keyboards do not type while HIDI is on", "needs a toggle combination", &s.Input.GrabKeyboards},
		toggleItem{"Game controllers are kept from other programs", "not possible on Windows", &s.Input.GrabGamepads},
		toggleItem{"Keyboards stay grabbed while HIDI is off", "Linux; hides the tapped key from the system", &s.Input.AlwaysGrab},
	)
	y = w.drawDeviceList(state, &s, editable, x, y, right)
	// How far an axis has to move in learning mode.
	vx = row("Learning mode lets an axis through", "when it moved by this much of its range")
	if picked, hit := choices(vx, s.Input.LearnThreshold, []int{5, 10, 20, 30, 50}, percent); hit {
		s.Input.LearnThreshold = picked
		w.set(s)
	}
	y += 46

	// ---- MIDI: the ports that exist, and those the settings name that
	// do not. A click adds or removes one.
	section("MIDI")
	vx = row("Also send to MIDI ports", "every instrument; followed at the next start")
	px, py := vx, y
	portChip := func(label string, on bool) bool {
		cw := w.chipWidth(label)
		if px > vx && px+cw > right {
			px, py = vx, py+chipH+6
		}
		hit := w.chip(label, on, px, py)
		px += cw + 6
		return hit && editable
	}
	for _, port := range v.ports {
		if portChip(port.Name, portChosen(s.Output.Connect, port)) {
			var still string
			s.Output.Connect, still = togglePort(s.Output.Connect, port)
			if still != "" {
				// Nothing to change: the entry may select other ports too.
				v.message = fmt.Sprintf("%q in settings.toml selects this port, and maybe others; change it there.", still)
			} else {
				w.set(s)
			}
		}
	}
	// Without a list nothing can be told to be absent.
	var absent []string
	if v.portsErr == "" {
		absent = absentPorts(s.Output.Connect, v.ports)
	} else {
		absent = s.Output.Connect
	}
	for _, pattern := range absent {
		label := pattern + " (not there now)"
		if v.portsErr != "" {
			label = pattern
		}
		if portChip(label, true) {
			s.Output.Connect = slices.DeleteFunc(slices.Clone(s.Output.Connect), func(p string) bool { return p == pattern })
			w.set(s)
		}
	}
	switch {
	case v.portsErr != "" && px > vx:
		// The reason goes below what is chosen.
		py += chipH + 6
		text(w.sans, fit(w.sans, v.portsErr, sizeText-2, right-vx), vx, py+3, sizeText-2, col(colFaint))
	case px > vx:
	case v.portsErr != "":
		text(w.sans, fit(w.sans, v.portsErr, sizeText-2, right-vx), vx, y+3, sizeText-2, col(colFaint))
	default:
		text(w.mono, "no MIDI ports found", vx, y+3, sizeMono, col(colFaint))
	}
	y = max(py+chipH+26, y+46)

	// ---- The lights.
	section("Lights")
	y = w.drawLights(&s, editable, x, y, right)

	// ---- The window.
	section("Window")
	switches(
		toggleItem{"The window opens when HIDI starts", "otherwise HIDI starts as an icon in the tray", &s.Window.ShowAtStart},
		toggleItem{"Look for a newer HIDI", "asks GitHub once a day; installs nothing", &s.Window.CheckUpdates},
	)
	// The window itself follows these at once.
	vx = row("Size of the interface", "of everything this window draws")
	if picked, hit := choices(vx, s.Window.Scale, []int{85, 100, 125, 150, 175, 200}, percent); hit {
		s.Window.Scale = picked
		w.set(s)
	}
	y += 46
	fps := func(n int) string {
		if n == 0 {
			return "none"
		}
		return fmt.Sprintf("%d", n)
	}
	vx = row("Frame limit", "none keeps to the monitor's refresh rate")
	if picked, hit := choices(vx, s.Window.FPS, []int{0, 30, 60, 120, 144, 240}, fps); hit {
		s.Window.FPS = picked
		w.set(s)
	}
	y += 46
	vx = row("Frame limit while unfocused", "for less work while another window is in use")
	if picked, hit := choices(vx, s.Window.BackgroundFPS, []int{0, 15, 30, 60}, fps); hit {
		s.Window.BackgroundFPS = picked
		w.set(s)
	}
	y += 46

	// ---- Where things are, and the profiles: every one, also those of
	// devices that are not connected, the user's own first, which come
	// before the factory ones where both fit a device.
	section("Profiles")
	text(w.sans, "HIDI's directory", x, y+5, sizeText, col(colText))
	bx := x + 130
	if _, err := os.Stat(p.Root); err == nil {
		hit, end := w.button("Open folder", bx, y)
		if hit {
			w.open(p.Root)
		}
		bx = end + 12
	}
	w.drawPath(p.Root, bx, y+6, right-bx)
	y += 44

	var own, factory []*config.Profile
	mine := map[string]bool{}
	for _, profile := range v.profiles {
		if profile.Kind == config.KindUser {
			own = append(own, profile)
			mine[filepath.Base(profile.Source)] = true
		} else {
			factory = append(factory, profile)
		}
	}
	// about writes what is said of a profile after its name, cut off at
	// its end where the sheet is too narrow.
	about := func(profile *config.Profile, from float32, ink RGB) {
		name := fit(w.sans, profile.Name, sizeText, right-from)
		text(w.sans, name, from, y+5, sizeText, col(ink))
		ax := from + width(w.sans, name, sizeText) + 12
		most := int((right - ax) / width(w.mono, "0", sizeMono))
		if most > 3 {
			text(w.mono, CutEnd(profileLine(profile), most), ax, y+6, sizeMono, col(colFaint))
		}
	}

	text(w.sans, "Yours", x, y, sizeText, col(colText))
	text(w.sans, fit(w.sans, "used before the factory profiles, and yours to change", sizeText-2, right-x-60), x+width(w.sans, "Yours", sizeText)+12, y+2, sizeText-2, col(colFaint))
	y += 26
	if len(own) == 0 {
		text(w.sans, "None yet. \"Customise\" makes one of a factory profile.", x, y, sizeText, col(colFaint))
		y += 26
	}
	for _, profile := range own {
		// The switch says whether the profile is used; it is written
		// into the profile at once.
		if w.pill(profile.Enabled, x, y+2) {
			switch {
			case w.unsaved(profile.Source):
				v.message = "This profile has changes that are not saved; save or drop them first."
			default:
				if err := SetProfileEnabled(profile.Source, !profile.Enabled); err != nil {
					v.message = strings.ReplaceAll(err.Error(), "\n", " ")
				} else {
					v.message = ""
					v.refresh(p, w.opts.Ports, time.Now(), true)
				}
			}
		}
		edit, end := w.button("Edit", x+50, y)
		if edit {
			w.gear.set(false, time.Now()) // the page is under the sheet
			w.editProfile(profile.Source)
		} else if w.unsaved(profile.Source) {
			rl.DrawCircleV(rl.NewVector2(end-2, y+2), 4.5, col(colUnsaved))
		}
		hit, end := w.button("Show in folder", end+6, y)
		if hit {
			w.show(profile.Source)
		}
		ink := colText
		if !profile.Enabled {
			ink = colOff
		}
		about(profile, end+12, ink)
		y += 32
	}
	y += 10

	text(w.sans, "Factory", x, y, sizeText, col(colText))
	text(w.sans, fit(w.sans, "come with HIDI, which keeps them as they are", sizeText-2, right-x-70), x+width(w.sans, "Factory", sizeText)+12, y+2, sizeText-2, col(colFaint))
	y += 26
	if len(factory) == 0 {
		text(w.sans, "None. HIDI writes them when it starts.", x, y, sizeText, col(colFaint))
		y += 26
	}
	for _, profile := range factory {
		view, end := w.button("View", x, y)
		if view {
			w.gear.set(false, time.Now())
			w.editProfile(profile.Source)
		}
		label := "Customise"
		if mine[filepath.Base(profile.Source)] {
			label = "Edit yours"
		}
		hit, end := w.button(label, end+6, y)
		if hit {
			w.customise(profile.Source)
		}
		about(profile, end+12, colText)
		y += 32
	}
	for _, problem := range v.broken {
		hit, end := w.button("Show in folder", x, y)
		if hit {
			w.show(problem.File)
		}
		text(w.sans, filepath.Base(problem.File)+" cannot be used:", end+12, y+5, sizeText, col(AnsiColour(91)))
		y += 28
		for _, line := range strings.Split(problem.Err.Error(), "\n") {
			text(w.mono, line, end+12, y, sizeMono, col(AnsiColour(91)))
			y += 18
		}
		y += 8
	}
}

// show shows a file in the file manager.
func (w *window) show(path string) {
	if err := showInFolder(path); err != nil {
		w.settings.message = fmt.Sprintf("%s could not be shown: %v", path, err)
	} else {
		w.settings.message = ""
	}
}

// customise opens the user's own copy of a factory profile for changing,
// making the copy if there is none.
func (w *window) customise(factoryFile string) {
	own, _, err := Customise(factoryFile, w.opts.Paths.User)
	if err != nil {
		w.settings.message = strings.ReplaceAll(err.Error(), "\n", " ")
		w.gear.set(true, time.Now())
		return
	}
	w.settings.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
	w.gear.set(false, time.Now())
	w.editProfile(own)
}

// checkbox draws a box that is ticked or not and reports a click on it or
// on the text beside it, which is room wide.
func (w *window) checkbox(ticked bool, x, y, room float32) bool {
	box := rl.NewRectangle(x, y+1, 16, 16)
	area := rl.NewRectangle(x-4, y-2, room+8, 22)
	over := hot(area, 0.3)
	if ticked {
		rl.DrawRectangleRounded(box, 0.3, 4, col(colAccent))
		rl.DrawLineEx(rl.NewVector2(x+3.5, y+9), rl.NewVector2(x+7, y+12.5), 2, col(colBar))
		rl.DrawLineEx(rl.NewVector2(x+7, y+12.5), rl.NewVector2(x+12.5, y+5), 2, col(colBar))
	} else {
		edge := colOff
		if over {
			edge = colFaint
		}
		rl.DrawRectangleRoundedLinesEx(box, 0.3, 4, 1.5, col(edge))
	}
	return clicked(area)
}

// drawDeviceList draws the choice of the devices that play: the way the
// list is read, and the list itself, of the devices that are connected,
// those HIDI has met before, and those the settings name.
func (w *window) drawDeviceList(state *ipc.Snapshot, s *settings.Settings, editable bool, x, y, right float32) float32 {
	text(w.sans, "Devices that play", x, y+2, sizeText, col(colText))
	about := "blacklist: all but the ticked ones play"
	if s.Devices.Mode == settings.Whitelist {
		about = "whitelist: only the ticked ones play"
	}
	text(w.sans, about, x, y+20, sizeText-2, col(colFaint))
	vx := x + w.valueX
	for _, mode := range []struct{ value, label string }{
		{settings.Blacklist, "blacklist"},
		{settings.Whitelist, "whitelist"},
	} {
		if w.chip(mode.label, s.Devices.Mode == mode.value, vx, y) && editable && s.Devices.Mode != mode.value {
			s.Devices.Mode = mode.value
			w.set(*s)
		}
		vx += w.chipWidth(mode.label) + 6
	}
	y += 30

	// Connected devices first; what is away, or only named by the
	// settings, stays in view at the end, so that it can be taken off
	// the list.
	var seen []seenDevice
	if state != nil {
		for _, d := range state.Devices {
			seen = append(seen, seenDevice{d.Name, d.Type, d.Connected})
		}
	}
	entries := deviceRows(seen, s.Devices.List)
	vx = x + w.valueX
	if len(entries) == 0 {
		hint := "no devices yet"
		if state == nil {
			hint = "start HIDI to see the devices"
		}
		text(w.mono, hint, vx, y+3, sizeMono, col(colFaint))
		return y + 34
	}
	for _, e := range entries {
		ticked := slices.Contains(s.Devices.List, e.Name)
		ink := colText
		if !e.There {
			ink = colOff
		}
		room := 24 + width(w.sans, e.Name, sizeText) + 10 + width(w.sans, e.About, sizeText-2)
		if w.checkbox(ticked, vx, y, min(room, right-vx)) && editable {
			s.Devices.List = flip(s.Devices.List, e.Name)
			w.set(*s)
		}
		text(w.sans, e.Name, vx+24, y+1, sizeText, col(ink))
		text(w.sans, e.About, vx+24+width(w.sans, e.Name, sizeText)+10, y+3, sizeText-2, col(colOff))
		y += 24
	}
	return y + 16
}

// drawLights draws the settings of the lights: whether the keys are lit,
// and the colours, of which the one that is clicked can be changed.
func (w *window) drawLights(s *settings.Settings, editable bool, x, y, right float32) float32 {
	v := &w.settings
	text(w.sans, "Light the keys through OpenRGB", x, y+2, sizeText, col(colText))
	text(w.sans, "needs an OpenRGB server, a program of its own", x, y+20, sizeText-2, col(colFaint))
	vx := x + w.valueX
	if w.pill(s.Lights.OpenRGB, vx, y) && editable {
		s.Lights.OpenRGB = !s.Lights.OpenRGB
		w.set(*s)
	}
	y += 46

	// Where the server is. The usual place shows faintly while none is
	// named. It can be changed while the lights are off: a HIDI that is
	// talking to a server is not sent elsewhere letter by letter.
	text(w.sans, "OpenRGB server", x, y+2, sizeText, col(colText))
	about := "address and port; turn the lights off to change it"
	if !s.Lights.OpenRGB {
		about = "address and port; empty means the usual place"
	}
	text(w.sans, about, x, y+20, sizeText-2, col(colFaint))
	field := rl.NewRectangle(vx, y-2, min(200, right-vx), 24)
	free := editable && !s.Lights.OpenRGB
	if !free {
		v.typing = false
	}
	if free && hovered(field) {
		setCursor(rl.MouseCursorIBeam)
	}
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		v.typing = free && hovered(field)
	}
	if v.typing {
		address := s.Lights.Address
		for r := rl.GetCharPressed(); r > 0; r = rl.GetCharPressed() {
			if r > 0x20 && r < 0x7f && len(address) < 64 {
				address += string(r)
			}
		}
		if (rl.IsKeyPressed(rl.KeyBackspace) || rl.IsKeyPressedRepeat(rl.KeyBackspace)) && address != "" {
			address = address[:len(address)-1]
		}
		if rl.IsKeyPressed(rl.KeyEnter) || rl.IsKeyPressed(rl.KeyEscape) {
			v.typing = false
		}
		if address != s.Lights.Address {
			s.Lights.Address = address
			w.set(*s)
		}
		w.moving = true // the caret blinks
	}
	rl.DrawRectangleRounded(field, 0.3, 6, col(colBar))
	edge := colPanelEdge
	switch {
	case v.typing:
		edge = colAccent
	case free && hovered(field):
		rl.DrawRectangleRounded(field, 0.3, 6, rl.NewColor(255, 255, 255, 20))
		edge = colFaint
	}
	rl.DrawRectangleRoundedLinesEx(field, 0.3, 6, hair(), col(edge))
	switch {
	case s.Lights.Address == "":
		text(w.mono, settings.DefaultOpenRGBAddress, field.X+8, y+3, sizeMono, colA(colFaint, 110))
	case free:
		text(w.mono, s.Lights.Address, field.X+8, y+3, sizeMono, col(colText))
	default:
		text(w.mono, s.Lights.Address, field.X+8, y+3, sizeMono, col(colFaint))
	}
	if v.typing && time.Now().UnixMilli()/500%2 == 0 {
		rl.DrawRectangleRec(rl.NewRectangle(field.X+9+width(w.mono, s.Lights.Address, sizeMono), y+3, 1, 14), col(colText))
	}
	y += 46

	text(w.sans, "Colours of the keys", x, y+2, sizeText, col(colText))
	text(w.sans, "click one to change it", x, y+20, sizeText-2, col(colFaint))
	// The colours are changed in what the page holds, and written with
	// everything else when the user saves.
	colours := v.edit.shown.Lights.Colours.Named()
	const cellW = 150
	cols := max(int((right-vx)/cellW), 1)
	for i, n := range colours {
		cx, cy := vx+float32(i%cols)*cellW, y+float32(i/cols)*26
		cell := rl.NewRectangle(cx-4, cy-3, cellW-6, 24)
		hot(cell, 0.3)
		if clicked(cell) && editable && v.slide == 0 {
			if v.colour == n.Name {
				v.colour = ""
			} else {
				v.colour = n.Name
			}
		}
		if v.colour == n.Name {
			rl.DrawRectangleRounded(cell, 0.3, 4, col(colPanelEdge))
		}
		swatch := rl.NewRectangle(cx, cy, 18, 18)
		rl.DrawRectangleRounded(swatch, 0.3, 4, col(Glow(RGB(*n.Colour))))
		rl.DrawRectangleRoundedLinesEx(swatch, 0.3, 4, hair(), col(colOff))
		text(w.mono, n.Name, cx+26, cy+2, sizeMono, col(colText))
	}
	y += float32((len(colours)+cols-1)/cols)*26 + 8

	for _, n := range colours {
		if n.Name != v.colour {
			continue
		}
		text(w.sans, n.About, vx, y+2, sizeText-2, col(colFaint))
		text(w.mono, settings.FormatColour(*n.Colour), vx+width(w.sans, n.About, sizeText-2)+12, y+1, sizeMono, col(colText))
		y += 24
		for i, part := range []struct {
			label string
			value *uint8
			ink   RGB
		}{{"R", &n.Colour.R, RGB{0xe0, 0x5a, 0x5a}}, {"G", &n.Colour.G, RGB{0x4c, 0xc2, 0x7a}}, {"B", &n.Colour.B, RGB{0x5a, 0x9b, 0xe0}}} {
			text(w.mono, part.label, vx, y+2, sizeMono, col(colFaint))
			track := rl.NewRectangle(vx+20, y+7, 200, 6)
			grip := rl.NewRectangle(track.X-8, y, track.Width+16, 20)
			hot(grip, 0.5)
			if clicked(grip) && editable {
				v.slide = i + 1
			}
			if v.slide == i+1 {
				if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
					at := (mouse().X - track.X) / track.Width
					*part.value = uint8(min(max(at, 0), 1)*255 + 0.5)
				} else {
					v.slide = 0
				}
			}
			at := track.X + track.Width*float32(*part.value)/255
			rl.DrawRectangleRounded(track, 1, 4, col(colPanelEdge))
			rl.DrawRectangleRounded(rl.NewRectangle(track.X, track.Y, at-track.X, track.Height), 1, 4, col(part.ink))
			rl.DrawCircleV(rl.NewVector2(at, track.Y+3), 7, col(colText))
			text(w.mono, fmt.Sprintf("%3d", *part.value), track.X+track.Width+16, y+2, sizeMono, col(colText))
			y += 24
		}
		defaults := light.DefaultColours()
		for _, d := range defaults.Named() {
			if d.Name == n.Name && *d.Colour != *n.Colour {
				if hit, _ := w.button("Back to the default", vx, y+2); hit && editable {
					*n.Colour = *d.Colour
				}
				y += 30
			}
		}
		y += 6
	}
	return y + 14
}

// drawPath writes a path, or other text in the fixed-width font, cut off
// at the start when it is too long: the end of a path says more.
func (w *window) drawPath(s string, x, y, room float32) {
	if most := int(room / width(w.mono, "0", sizeMono)); most > 1 {
		s = CutStart(s, most)
	}
	text(w.mono, s, x, y, sizeMono, col(colFaint))
}
