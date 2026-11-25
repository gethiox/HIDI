//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// address is the field the address of the OpenRGB server is typed
	// into.
	address typing
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
	over := w.hovered(area)
	if over {
		w.setCursor(rl.MouseCursorPointingHand)
	}
	if w.clicked(area) {
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
	picture := w.cog
	rl.DrawTexturePro(picture, rl.NewRectangle(0, 0, float32(picture.Width), float32(picture.Height)),
		rl.NewRectangle(area.X+size/2, area.Y+size/2, size, size), rl.NewVector2(size/2, size/2), angle, ink)
	// Settings that were changed and not saved wait behind it.
	if !w.gear.open && w.settings.exists && w.settings.edit.Dirty() {
		rl.DrawCircleV(rl.NewVector2(area.X+size-2, area.Y+3), 4.5, col(colUnsaved))
	}
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
	w.useLocal()
	s := w.settings.edit.shown
	// Settings with a mistake in them, an address half typed for one,
	// wait until they are right.
	if len(s.Check()) > 0 {
		return
	}
	w.tried.put(struct{}{}, string(s.Text()))
}

// useLocal makes this window go by the settings that are its own.
func (w *window) useLocal() {
	s := w.settings.edit.shown
	w.pace.limit, w.pace.background = s.Window.FPS, s.Window.BackgroundFPS
	if s.Window.Scale >= settings.MinScale && s.Window.Scale <= settings.MaxScale {
		w.wantScale = float32(s.Window.Scale) / 100
	}
	w.newest.on = s.Window.CheckUpdates
	w.names = s.Window.ControlNames
}

// saveSettings writes the settings as they are shown into the settings
// file, and does nothing else: they are in use since they were changed.
func (w *window) saveSettings() {
	v := &w.settings
	if !v.edit.Dirty() {
		return
	}
	if err := settings.Save(w.opts.Paths.Settings, v.edit.shown); err != nil {
		v.message = strings.ReplaceAll(err.Error(), "\n", " ")
		return
	}
	// Nothing else: the settings are in use already, and saving only
	// keeps them.
	v.message = "Saved."
	v.edit.disk = clone(v.edit.shown)
}

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
	rl.DrawRectangleRoundedLinesEx(sheet, roundness, 8, w.hair(), col(colPanelEdge))
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && w.hovered(body) && !w.hovered(sheet) {
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
	w.deaf = w.hovered(corner)
	w.drawSettings(state, rl.NewRectangle(sheet.X+2, sheet.Y+2, sheet.Width-4, sheet.Height-4))
	w.deaf = false

	ink := col(colText)
	if dirty {
		ink = col(colUnsaved)
	}
	rl.DrawRectangleRounded(button, 0.25, 6, colA(colBar, 235))
	if dirty && w.hovered(button) {
		w.setCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(button, 0.25, 6, col(colPanelEdge))
	}
	drawFloppy(button.X+6, button.Y+6, disk/16, ink)
	control := ctrlHeld()
	if dirty && (w.clicked(button) || control && rl.IsKeyPressed(rl.KeyS)) {
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
	// only names the system the setting applies to, as runtime.GOOS
	// has it; empty for every system. Elsewhere the setting is shown
	// faintly and cannot be changed.
	only string
}

// sheet is the settings page while it is drawn: where the next thing
// goes, and the settings as they are shown, which the things drawn change.
type sheet struct {
	*window
	area rl.Rectangle
	// x and right are the edges of what is drawn, and y where the next
	// thing goes.
	x, y, right float32
	// editable says that the settings can be changed: there is a file
	// for them, without mistakes.
	editable bool
	// s are the settings as shown; changed hands them on.
	s *settings.Settings
}

// changed takes the settings over as the sheet has changed them.
func (sh *sheet) changed() { sh.window.set(*sh.s) }

// section begins a group of settings: a line from edge to edge of the
// sheet, and the name of the group.
func (sh *sheet) section(title string) {
	sh.y += 4
	rl.DrawRectangleRec(rl.NewRectangle(sh.area.X, sh.y, sh.area.Width, 1), col(colPanelEdge))
	rl.DrawRectangleRec(rl.NewRectangle(sh.area.X, sh.y+1, sh.area.Width, 34), colA(colPanel, 150))
	sh.text(sh.bold, title, sh.x, sh.y+9, sizeName, col(colText))
	sh.y += 50
}

// row draws the name of a setting and returns where its value goes.
func (sh *sheet) row(name, about string) float32 {
	sh.text(sh.sans, fit(sh.sans, name, sizeText, sh.valueX-10), sh.x, sh.y+2, sizeText, col(colText))
	if about != "" {
		sh.text(sh.sans, fit(sh.sans, about, sizeText-2, sh.valueX-10), sh.x, sh.y+20, sizeText-2, col(colFaint))
	}
	return sh.x + sh.valueX
}

// switches draws settings that are a yes or a no: two beside each other
// where the sheet is wide enough for that, one below the other where it
// is not.
func (sh *sheet) switches(items ...toggleItem) {
	columns := 1
	if sh.right-sh.x >= 620 {
		columns = 2
	}
	cell := (sh.right - sh.x) / float32(columns)
	for i := 0; i < len(items); i += columns {
		high := float32(32)
		for j := i; j < min(i+columns, len(items)); j++ {
			item := items[j]
			if item.about != "" {
				high = 46
			}
			cx := sh.x + float32(j-i)*cell
			room := cell - 50 - 16
			name, about := fit(sh.sans, item.name, sizeText, room), fit(sh.sans, item.about, sizeText-2, room)
			wide := max(width(sh.sans, name, sizeText), width(sh.sans, about, sizeText-2))
			if item.only != "" && item.only != runtime.GOOS {
				// Not for this system: shown, and not to be changed.
				sh.frozen(rl.NewRectangle(cx, sh.y, 40, 20), *item.value)
				sh.text(sh.sans, name, cx+50, sh.y+2, sizeText, col(colOff))
				if about != "" {
					sh.text(sh.sans, about, cx+50, sh.y+20, sizeText-2, col(colOff))
				}
				continue
			}
			// The words beside a switch take the click too.
			if sh.toggle(rl.NewRectangle(cx, sh.y, 40, 20), *item.value, colOn, rl.NewRectangle(cx, sh.y-2, 50+wide, 24)) && sh.editable {
				*item.value = !*item.value
				sh.changed()
			}
			sh.text(sh.sans, name, cx+50, sh.y+2, sizeText, col(colText))
			if about != "" {
				sh.text(sh.sans, about, cx+50, sh.y+20, sizeText-2, col(colFaint))
			}
		}
		sh.y += high
	}
	sh.y += 6
}

// choices draws a row of buttons of which one is chosen, beginning at
// vx, and reports the one that was clicked.
func (sh *sheet) choices(vx float32, chosen int, values []int, label func(int) string) (int, bool) {
	picked, hit := 0, false
	for _, value := range values {
		text := label(value)
		if vx+sh.chipWidth(text) > sh.right {
			break
		}
		if sh.chip(text, chosen == value, vx, sh.y) && sh.editable && chosen != value {
			picked, hit = value, true
		}
		vx += sh.chipWidth(text) + 6
	}
	return picked, hit
}

func percent(n int) string { return fmt.Sprintf("%d%%", n) }

// drawSettings draws the settings page: sections one below the other,
// each under a line across the sheet.
func (w *window) drawSettings(state *ipc.Snapshot, area rl.Rectangle) {
	v := &w.settings
	p := w.opts.Paths
	v.refresh(p, w.opts.Ports, time.Now(), false)

	// The page is as long as it was when last drawn, which is what the
	// scrolling is held to before anything is drawn.
	v.scroll.Target -= w.wheel(area) * 60
	v.scroll.Clamp(v.content - area.Height)
	if v.scroll.Step(w.dt) {
		w.moving = true
	}
	scrolled := float32(int(v.scroll.At))
	w.clip(area)
	defer w.unclip()

	shown := clone(v.edit.shown)
	sh := &sheet{
		window: w, area: area,
		x: area.X + margin, y: area.Y + 12 - scrolled, right: area.X + area.Width - margin,
		editable: v.exists && v.problem == "",
		s:        &shown,
	}
	defer func() { v.content = sh.y + scrolled - area.Y + 14 }()
	// Where the value of a setting begins, beside its name: less far in
	// on a narrow sheet.
	w.valueX = min(300, (sh.right-sh.x)*0.46)

	w.text(w.bold, "Settings", sh.x, sh.y, sizeName, col(colText))
	sh.y += 26
	if p.Root == "" {
		w.text(w.sans, "HIDI has no directory for settings here and uses what it comes with.", sh.x, sh.y, sizeText, col(colFaint))
		return
	}
	if !v.exists {
		w.text(w.sans, "HIDI's directory is not in use yet. HIDI creates it on its first start.", sh.x, sh.y, sizeText, col(colFaint))
		sh.y += 30
	}
	if v.problem != "" {
		for _, line := range strings.Split("settings.toml has mistakes; the defaults are shown:\n"+v.problem, "\n") {
			w.text(w.sans, line, sh.x, sh.y, sizeText, col(AnsiColour(91)))
			sh.y += 18
		}
		sh.y += 8
	}
	// The outcome of the last change has a line of its own that is always
	// there, so that nothing below moves when it appears.
	message := v.message
	if message == "" && v.edit.Dirty() && v.exists {
		message = "In use now, and not saved: the disk in the corner keeps it, Revert takes it back."
	}
	w.text(w.sans, fit(w.sans, message, sizeText, sh.right-sh.x-130), sh.x, sh.y, sizeText, col(AnsiColour(93)))
	sh.y += 28

	sh.onOff(state)
	sh.windowSettings()
	sh.devices(state)
	sh.midi()
	sh.lights()
	sh.profiles()
}

// onOff draws what turns HIDI on and off.
func (sh *sheet) onOff(state *ipc.Snapshot) {
	w, v := sh.window, &sh.window.settings
	sh.section("State control")
	toggleNames := make([]string, len(sh.s.Input.Toggle))
	for i, c := range sh.s.Input.Toggle {
		toggleNames[i] = strings.TrimPrefix(c.String(), "KEY_")
	}
	vx := sh.row("Toggle combination", "turns HIDI off and on")
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
			sh.s.Input.Toggle = nil
			for _, name := range v.capture.keys {
				if code, err := control.ParseCode(name); err == nil {
					sh.s.Input.Toggle = append(sh.s.Input.Toggle, code)
				}
			}
			if len(sh.s.Input.Toggle) != 1 {
				// A tap, and a key of HIDI's alone, are of one key.
				sh.s.Input.ToggleTap, sh.s.Input.ToggleExclusive = false, false
			}
			v.capture = nil
			sh.changed()
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
		w.text(w.mono, pressed, vx, sh.y+3, sizeMono, col(AnsiColour(93)))
		if hit, _ := w.button("Cancel", vx+width(w.mono, pressed, sizeMono)+14, sh.y-2); hit {
			v.capture = nil
		}
	case state == nil:
		w.text(w.mono, combination, vx, sh.y+3, sizeMono, col(colText))
		w.text(w.sans, "start HIDI to change it here", vx+width(w.mono, combination, sizeMono)+14, sh.y+3, sizeText-2, col(colFaint))
	default:
		w.text(w.mono, combination, vx, sh.y+3, sizeMono, col(colText))
		if hit, _ := w.button("Change", vx+width(w.mono, combination, sizeMono)+14, sh.y-2); hit && sh.editable {
			v.capture, v.message = &capture{}, ""
		}
	}
	sh.y += 48
	sh.switches(
		toggleItem{"Pass the toggle key through to the system when held", "a tap toggles HIDI; held longer, the key does what it normally does", &sh.s.Input.ToggleTap, ""},
		toggleItem{"The toggle key is HIDI's alone", "it never reaches the system, tapped or held", &sh.s.Input.ToggleExclusive, ""},
		toggleItem{"HIDI is on when it starts", "", &sh.s.Input.StartOn, ""},
	)
}

// devices draws what is done with the devices, and which of them play.
func (sh *sheet) devices(state *ipc.Snapshot) {
	w := sh.window
	sh.section("Devices")
	sh.switches(
		toggleItem{"Keyboards do not type while HIDI is on", "needs a toggle combination", &sh.s.Input.GrabKeyboards, ""},
		toggleItem{"Game controllers are kept from other programs", "Linux only: Windows cannot", &sh.s.Input.GrabGamepads, "linux"},
		toggleItem{"Keyboards stay grabbed while HIDI is off", "Linux only: hides the tapped key from the system", &sh.s.Input.AlwaysGrab, "linux"},
	)
	sh.y = w.drawDeviceList(state, sh.s, sh.editable, sh.x, sh.y, sh.right)
	// How far an axis has to move in learning mode.
	vx := sh.row("Learning mode lets an axis through", "when it moved by this much of its range")
	if picked, hit := sh.choices(vx, sh.s.Input.LearnThreshold, []int{5, 10, 20, 30, 50}, percent); hit {
		sh.s.Input.LearnThreshold = picked
		sh.changed()
	}
	sh.y += 46
}

// midi draws where else the instruments send: the ports that exist, and
// those the settings name that do not. A click adds or removes one.
func (sh *sheet) midi() {
	w, v := sh.window, &sh.window.settings
	sh.section("MIDI")
	vx := sh.row("Also send to MIDI ports", "every instrument")
	ports := newFlow(vx, sh.right, sh.y, chipH+6, 6)
	portChip := func(label string, on bool) bool {
		px, py := ports.put(w.chipWidth(label))
		return w.chip(label, on, px, py) && sh.editable
	}
	for _, port := range v.ports {
		if portChip(port.Name, portChosen(sh.s.Output.Connect, port)) {
			var still string
			sh.s.Output.Connect, still = togglePort(sh.s.Output.Connect, port)
			if still != "" {
				// Nothing to change: the entry may select other ports too.
				v.message = fmt.Sprintf("%q in settings.toml selects this port, and maybe others; change it there.", still)
			} else {
				sh.changed()
			}
		}
	}
	// Without a list nothing can be told to be absent.
	var absent []string
	if v.portsErr == "" {
		absent = absentPorts(sh.s.Output.Connect, v.ports)
	} else {
		absent = sh.s.Output.Connect
	}
	for _, pattern := range absent {
		label := pattern + " (not there now)"
		if v.portsErr != "" {
			label = pattern
		}
		if portChip(label, true) {
			sh.s.Output.Connect = slices.DeleteFunc(slices.Clone(sh.s.Output.Connect), func(p string) bool { return p == pattern })
			sh.changed()
		}
	}
	py := ports.y
	switch {
	case v.portsErr != "" && !ports.empty():
		// The reason goes below what is chosen.
		py += chipH + 6
		w.text(w.sans, fit(w.sans, v.portsErr, sizeText-2, sh.right-vx), vx, py+3, sizeText-2, col(colFaint))
	case !ports.empty():
	case v.portsErr != "":
		w.text(w.sans, fit(w.sans, v.portsErr, sizeText-2, sh.right-vx), vx, sh.y+3, sizeText-2, col(colFaint))
	default:
		w.text(w.mono, "no MIDI ports found", vx, sh.y+3, sizeMono, col(colFaint))
	}
	sh.y = max(py+chipH+26, sh.y+46)
}

// lights draws the settings of the lights.
func (sh *sheet) lights() {
	w := sh.window
	sh.section("Lights")
	sh.y = w.drawLights(sh.s, sh.editable, sh.x, sh.y, sh.right)
}

// windowSettings draws the settings of the window, which follows them
// at once.
func (sh *sheet) windowSettings() {
	sh.section("Window")
	sh.switches(
		toggleItem{"The window opens when HIDI starts", "otherwise HIDI starts as an icon in the tray", &sh.s.Window.ShowAtStart, ""},
		toggleItem{"Look for a newer HIDI", "asks GitHub once a day; installs nothing", &sh.s.Window.CheckUpdates, ""},
		toggleItem{"Name the controls of game controllers", "BTN_SOUTH and ABS_X, as profiles call them, beside their numbers", &sh.s.Window.ControlNames, ""},
	)
	// The window itself follows these at once.
	vx := sh.row("Size of the interface", "of everything this window draws")
	if picked, hit := sh.choices(vx, sh.s.Window.Scale, []int{85, 100, 125, 150, 175, 200}, percent); hit {
		sh.s.Window.Scale = picked
		sh.changed()
	}
	sh.y += 46
	fps := func(n int) string {
		if n == 0 {
			return "none"
		}
		return fmt.Sprintf("%d", n)
	}
	vx = sh.row("Frame limit", "none keeps to the monitor's refresh rate")
	if picked, hit := sh.choices(vx, sh.s.Window.FPS, []int{0, 30, 60, 120, 144, 240}, fps); hit {
		sh.s.Window.FPS = picked
		sh.changed()
	}
	sh.y += 46
	vx = sh.row("Frame limit while unfocused", "for less work while another window is in use")
	if picked, hit := sh.choices(vx, sh.s.Window.BackgroundFPS, []int{0, 15, 30, 60}, fps); hit {
		sh.s.Window.BackgroundFPS = picked
		sh.changed()
	}
	sh.y += 46
}

// profiles draws where HIDI keeps things, and every profile, also those
// of devices that are not connected: the user's own first, which come
// before the factory ones where both fit a device.
func (sh *sheet) profiles() {
	w, v := sh.window, &sh.window.settings
	p := w.opts.Paths
	sh.section("Profiles")
	w.text(w.sans, "HIDI's directory", sh.x, sh.y+5, sizeText, col(colText))
	bx := sh.x + 130
	if _, err := os.Stat(p.Root); err == nil {
		hit, end := w.button("Open folder", bx, sh.y)
		if hit {
			w.open(p.Root)
		}
		bx = end + 12
	}
	w.drawPath(p.Root, bx, sh.y+6, sh.right-bx)
	sh.y += 44

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
		name := fit(w.sans, profile.Name, sizeText, sh.right-from)
		w.text(w.sans, name, from, sh.y+5, sizeText, col(ink))
		ax := from + width(w.sans, name, sizeText) + 12
		most := int((sh.right - ax) / width(w.mono, "0", sizeMono))
		if most > 3 {
			w.text(w.mono, CutEnd(profileLine(profile), most), ax, sh.y+6, sizeMono, col(colFaint))
		}
	}

	w.text(w.sans, "Yours", sh.x, sh.y, sizeText, col(colText))
	w.text(w.sans, fit(w.sans, "used before the factory profiles, and yours to change", sizeText-2, sh.right-sh.x-60), sh.x+width(w.sans, "Yours", sizeText)+12, sh.y+2, sizeText-2, col(colFaint))
	sh.y += 26
	if len(own) == 0 {
		w.text(w.sans, "None yet. \"Customise\" makes one of a factory profile.", sh.x, sh.y, sizeText, col(colFaint))
		sh.y += 26
	}
	for _, profile := range own {
		// The switch says whether the profile is used; it is written
		// into the profile at once.
		if w.pill(profile.Enabled, sh.x, sh.y+2) {
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
		edit, end := w.button("Edit", sh.x+50, sh.y)
		if edit {
			w.gear.set(false, time.Now()) // the page is under the sheet
			w.editProfile(profile.Source)
		} else if w.unsaved(profile.Source) {
			rl.DrawCircleV(rl.NewVector2(end-2, sh.y+2), 4.5, col(colUnsaved))
		}
		hit, end := w.button("Show in folder", end+6, sh.y)
		if hit {
			w.show(profile.Source)
		}
		remove, end := w.button("Delete", end+6, sh.y)
		if remove {
			w.deleteProfile(profile)
		}
		ink := colText
		if !profile.Enabled {
			ink = colOff
		}
		about(profile, end+12, ink)
		sh.y += 32
	}
	sh.y += 10

	w.text(w.sans, "Factory", sh.x, sh.y, sizeText, col(colText))
	w.text(w.sans, fit(w.sans, "come with HIDI, which keeps them as they are", sizeText-2, sh.right-sh.x-70), sh.x+width(w.sans, "Factory", sizeText)+12, sh.y+2, sizeText-2, col(colFaint))
	sh.y += 26
	if len(factory) == 0 {
		w.text(w.sans, "None. HIDI writes them when it starts.", sh.x, sh.y, sizeText, col(colFaint))
		sh.y += 26
	}
	for _, profile := range factory {
		view, end := w.button("View", sh.x, sh.y)
		if view {
			w.gear.set(false, time.Now())
			w.editProfile(profile.Source)
		}
		label := "Customise"
		if mine[filepath.Base(profile.Source)] {
			label = "Edit yours"
		}
		hit, end := w.button(label, end+6, sh.y)
		if hit {
			w.customise(profile.Source)
		}
		about(profile, end+12, colText)
		sh.y += 32
	}
	for _, problem := range v.broken {
		hit, end := w.button("Show in folder", sh.x, sh.y)
		if hit {
			w.show(problem.File)
		}
		w.text(w.sans, filepath.Base(problem.File)+" cannot be used:", end+12, sh.y+5, sizeText, col(AnsiColour(91)))
		sh.y += 28
		for _, line := range strings.Split(problem.Err.Error(), "\n") {
			w.text(w.mono, line, end+12, sh.y, sizeMono, col(AnsiColour(91)))
			sh.y += 18
		}
		sh.y += 8
	}
}

// deleteProfile asks whether a profile of the user's is to be removed,
// and removes its file when the answer is yes.
func (w *window) deleteProfile(profile *config.Profile) {
	file := profile.Source
	question := fmt.Sprintf("Delete your profile %q?\n\nIts file, %s, is removed. This cannot be taken back. Devices that played by it play by the next profile that fits them.",
		profile.Name, filepath.Base(file))
	w.confirm(question, "Delete", func() {
		if err := os.Remove(file); err != nil {
			w.settings.message = fmt.Sprintf("%s could not be removed: %v", file, err)
			return
		}
		// What was being changed in it goes with it, also for the
		// running HIDI.
		if e := w.docs[file]; e != nil {
			if w.edit == e {
				w.edit = nil
			}
			delete(w.docs, file)
			w.profiles.put(file, "")
		}
		w.settings.message = ""
		w.settings.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
	})
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

// drawDeviceList draws the choice of the devices that play: the way the
// list is read, and the list itself, of the devices that are connected,
// those HIDI has met before, and those the settings name.
func (w *window) drawDeviceList(state *ipc.Snapshot, s *settings.Settings, editable bool, x, y, right float32) float32 {
	w.text(w.sans, "Devices that play", x, y+2, sizeText, col(colText))
	about := "blacklist: all but the ticked ones play"
	if s.Devices.Mode == settings.Whitelist {
		about = "whitelist: only the ticked ones play"
	}
	w.text(w.sans, about, x, y+20, sizeText-2, col(colFaint))
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
		w.text(w.mono, hint, vx, y+3, sizeMono, col(colFaint))
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
		w.text(w.sans, e.Name, vx+24, y+1, sizeText, col(ink))
		w.text(w.sans, e.About, vx+24+width(w.sans, e.Name, sizeText)+10, y+3, sizeText-2, col(colOff))
		y += 24
	}
	return y + 16
}

// drawLights draws the settings of the lights: whether the keys are lit,
// and the colours, of which the one that is clicked can be changed.
func (w *window) drawLights(s *settings.Settings, editable bool, x, y, right float32) float32 {
	v := &w.settings
	w.text(w.sans, "Light the keys through OpenRGB", x, y+2, sizeText, col(colText))
	w.text(w.sans, "needs an OpenRGB server, a program of its own", x, y+20, sizeText-2, col(colFaint))
	vx := x + w.valueX
	if w.pill(s.Lights.OpenRGB, vx, y) && editable {
		s.Lights.OpenRGB = !s.Lights.OpenRGB
		w.set(*s)
	}
	y += 46

	// Where the server is. The usual place shows faintly while none is
	// named. It can be changed while the lights are off: a HIDI that is
	// talking to a server is not sent elsewhere letter by letter.
	w.text(w.sans, "OpenRGB server", x, y+2, sizeText, col(colText))
	about := "address and port; turn the lights off to change it"
	if !s.Lights.OpenRGB {
		about = "address and port; empty means the usual place"
	}
	w.text(w.sans, about, x, y+20, sizeText-2, col(colFaint))
	// What is typed is taken as it is; whether it is an address is
	// said when it is to be used.
	address := s.Lights.Address
	look := fieldLook{hint: settings.DefaultOpenRGBAddress, most: 64, locked: !editable || s.Lights.OpenRGB,
		accept: func(c rune) bool { return c > 0x20 && c < 0x7f }}
	if changed, _ := w.field(&v.address, rl.NewRectangle(vx, y-2, min(200, right-vx), 24), &address, look); changed {
		s.Lights.Address = address
		w.set(*s)
	}
	y += 46

	w.text(w.sans, "Colours of the keys", x, y+2, sizeText, col(colText))
	w.text(w.sans, "click one to change it", x, y+20, sizeText-2, col(colFaint))
	// The colours are changed in what the page holds, and written with
	// everything else when the user saves.
	colours := v.edit.shown.Lights.Colours.Named()
	const cellW = 150
	cols := max(int((right-vx)/cellW), 1)
	for i, n := range colours {
		cx, cy := vx+float32(i%cols)*cellW, y+float32(i/cols)*26
		cell := rl.NewRectangle(cx-4, cy-3, cellW-6, 24)
		w.hot(cell, 0.3)
		if w.clicked(cell) && editable && v.slide == 0 {
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
		rl.DrawRectangleRoundedLinesEx(swatch, 0.3, 4, w.hair(), col(colOff))
		w.text(w.mono, n.Name, cx+26, cy+2, sizeMono, col(colText))
	}
	y += float32((len(colours)+cols-1)/cols)*26 + 8

	for _, n := range colours {
		if n.Name != v.colour {
			continue
		}
		w.text(w.sans, n.About, vx, y+2, sizeText-2, col(colFaint))
		w.text(w.mono, settings.FormatColour(*n.Colour), vx+width(w.sans, n.About, sizeText-2)+12, y+1, sizeMono, col(colText))
		y += 24
		for i, part := range []struct {
			label string
			value *uint8
			ink   RGB
		}{{"R", &n.Colour.R, RGB{0xe0, 0x5a, 0x5a}}, {"G", &n.Colour.G, RGB{0x4c, 0xc2, 0x7a}}, {"B", &n.Colour.B, RGB{0x5a, 0x9b, 0xe0}}} {
			w.text(w.mono, part.label, vx, y+2, sizeMono, col(colFaint))
			track := rl.NewRectangle(vx+20, y+7, 200, 6)
			grip := rl.NewRectangle(track.X-8, y, track.Width+16, 20)
			w.hot(grip, 0.5)
			if w.clicked(grip) && editable {
				v.slide = i + 1
			}
			if v.slide == i+1 {
				if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
					at := (w.mouse().X - track.X) / track.Width
					if to := uint8(min(max(at, 0), 1)*255 + 0.5); to != *part.value {
						// In use at once: the lights follow the slider.
						*part.value = to
						w.useSettings()
					}
				} else {
					v.slide = 0
				}
			}
			at := track.X + track.Width*float32(*part.value)/255
			rl.DrawRectangleRounded(track, 1, 4, col(colPanelEdge))
			rl.DrawRectangleRounded(rl.NewRectangle(track.X, track.Y, at-track.X, track.Height), 1, 4, col(part.ink))
			rl.DrawCircleV(rl.NewVector2(at, track.Y+3), 7, col(colText))
			w.text(w.mono, fmt.Sprintf("%3d", *part.value), track.X+track.Width+16, y+2, sizeMono, col(colText))
			y += 24
		}
		defaults := light.DefaultColours()
		for _, d := range defaults.Named() {
			if d.Name == n.Name && *d.Colour != *n.Colour {
				if hit, _ := w.button("Back to the default", vx, y+2); hit && editable {
					*n.Colour = *d.Colour
					w.useSettings()
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
	w.text(w.mono, s, x, y, sizeMono, col(colFaint))
}
