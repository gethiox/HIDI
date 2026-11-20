//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
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
	loaded   time.Time
	exists   bool // HIDI's directory is in use
	values   settings.Settings
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
		v.values = settings.Default()
		return
	}
	s, err := settings.Load(p.Settings)
	if err != nil {
		v.problem = err.Error()
	}
	v.values = s
	v.profiles, v.broken = config.LoadDir(p.Profiles)
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
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
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
	cx, cy := area.X+size/2, area.Y+size/2
	// Six teeth: three bars through the middle, each turned a sixth of a
	// circle further. raylib turns clockwise for growing angles.
	for i := range 3 {
		bar := rl.NewRectangle(cx, cy, 26, 7)
		rl.DrawRectanglePro(bar, rl.NewVector2(13, 3.5), angle+float32(i)*60, ink)
	}
	rl.DrawCircleV(rl.NewVector2(cx, cy), 9, ink)
	rl.DrawCircleV(rl.NewVector2(cx, cy), 4, col(colBackground))
}

// button draws a button and reports a click on it.
func (w *window) button(label string, x, y float32) (clickedNow bool, right float32) {
	bw := width(w.sans, label, sizeText) + 20
	r := rl.NewRectangle(x, y, bw, 24)
	if hovered(r) {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanelEdge))
	} else {
		rl.DrawRectangleRounded(r, 0.35, 6, col(colPanel))
		rl.DrawRectangleRoundedLinesEx(r, 0.35, 6, 1, col(colPanelEdge))
	}
	text(w.sans, label, x+10, y+5, sizeText, col(colText))
	return clicked(r), x + bw
}

// pill draws an on/off switch and reports a click on it.
func (w *window) pill(on bool, x, y float32) bool {
	r := rl.NewRectangle(x, y, 40, 20)
	back, knob := colOff, r.X+10
	if on {
		back, knob = colOn, r.X+r.Width-10
	}
	if hovered(r) {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
	}
	rl.DrawRectangleRounded(r, 1, 12, col(back))
	rl.DrawCircle(int32(knob), int32(r.Y+10), 7, col(colText))
	return clicked(r)
}

func (w *window) open(path string) {
	if err := openPath(path); err != nil {
		w.settings.message = fmt.Sprintf("%s could not be opened: %v", path, err)
	} else {
		w.settings.message = ""
	}
}

// save writes changed settings and says what came of it.
func (w *window) save(s settings.Settings, note string) {
	if err := settings.Save(w.opts.Paths.Settings, s); err != nil {
		w.settings.message = strings.ReplaceAll(err.Error(), "\n", " ")
		return
	}
	w.settings.message = note
	w.settings.refresh(w.opts.Paths, w.opts.Ports, time.Now(), true)
}

// What the page says after a change, by when a running HIDI follows it.
const (
	soonNote    = "Saved. A running HIDI takes this over within a few seconds."
	restartNote = "Saved. A running HIDI takes this over when it is started again."
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
	rl.DrawRectangleRoundedLinesEx(sheet, roundness, 8, 1, col(colPanelEdge))
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && hovered(body) && !hovered(sheet) {
		w.gear.set(false, time.Now())
		return
	}
	w.drawSettings(state, rl.NewRectangle(sheet.X+2, sheet.Y+2, sheet.Width-4, sheet.Height-4))
}

// drawSettings draws the settings page.
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
	heading := func(s string) {
		text(w.bold, s, x, y, sizeName, col(colText))
		y += 30
	}
	// row draws the name of a setting and returns where its value goes.
	const valueX = 300
	row := func(name, about string) float32 {
		text(w.sans, name, x, y+2, sizeText, col(colText))
		if about != "" {
			text(w.sans, about, x, y+20, sizeText-2, col(colFaint))
		}
		return x + valueX
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
	text(w.sans, v.message, x, y, sizeText, col(AnsiColour(93)))
	y += 26

	// The settings that are a yes or a no can be changed here.
	editable := v.exists && v.problem == ""
	s := v.values
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
				s.Input.ToggleTap = false // a tap is of one key
			}
			v.capture = nil
			w.save(s, soonNote)
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
	y += 46

	for _, item := range []struct {
		name, about string
		value       *bool
	}{
		{"Keyboards do not type while HIDI is on", "needs a toggle combination", &s.Input.GrabKeyboards},
		{"Game controllers are kept from other programs", "not possible on Windows", &s.Input.GrabGamepads},
		{"HIDI is on when it starts", "", &s.Input.StartOn},
		{"The toggle is one key, tapped", "held longer, the key does what it normally does", &s.Input.ToggleTap},
		{"Keyboards stay grabbed while HIDI is off", "Linux; hides the tapped key from the system", &s.Input.AlwaysGrab},
		{"The window opens when HIDI starts", "otherwise HIDI starts as an icon in the tray", &s.Window.ShowAtStart},
	} {
		vx := row(item.name, item.about)
		if w.pill(*item.value, vx, y) && editable {
			*item.value = !*item.value
			note := soonNote
			if item.value == &s.Input.StartOn || item.value == &s.Window.ShowAtStart {
				note = restartNote
			}
			w.save(s, note)
		}
		y += 46
		if item.about == "" {
			y -= 12
		}
	}

	// How far an axis has to move in learning mode.
	vx = row("Learning mode lets an axis through when it moved", "by this much of its range; the switch is at the top")
	for _, percent := range []int{5, 10, 20, 30, 50} {
		label := fmt.Sprintf("%d%%", percent)
		if w.chip(label, s.Input.LearnThreshold == percent, vx, y) && editable && s.Input.LearnThreshold != percent {
			s.Input.LearnThreshold = percent
			w.save(s, soonNote)
		}
		vx += w.chipWidth(label) + 6
	}
	if !slices.Contains([]int{5, 10, 20, 30, 50}, s.Input.LearnThreshold) {
		text(w.mono, fmt.Sprintf("now %d%%", s.Input.LearnThreshold), vx+6, y+3, sizeMono, col(colFaint))
	}
	y += 46

	y = w.drawDeviceList(state, &s, editable, x, y, right)

	// The MIDI ports that exist, and those the settings name that do not:
	// a click adds or removes one.
	vx = row("Also send to MIDI ports", "every instrument; HIDI follows when it starts again")
	px, py := vx, y
	portChip := func(label string, on bool) bool {
		cw := w.chipWidth(label)
		if px > vx && px+cw > right {
			px, py = vx, py+chipH+6
		}
		chip := rl.NewRectangle(px, py, cw, chipH)
		if hovered(chip) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
		if on {
			rl.DrawRectangleRounded(chip, 0.4, 6, col(colAccent))
			text(w.mono, label, px+7, py+3, sizeMono, col(colBar))
		} else {
			rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 1, col(colPanelEdge))
			text(w.mono, label, px+7, py+3, sizeMono, col(colFaint))
		}
		px += cw + 6
		return clicked(chip) && editable
	}
	for _, port := range v.ports {
		if portChip(port.Name, portChosen(s.Output.Connect, port)) {
			var still string
			s.Output.Connect, still = togglePort(s.Output.Connect, port)
			if still != "" {
				// Nothing to save: the entry may select other ports too.
				v.message = fmt.Sprintf("%q in settings.toml selects this port, and maybe others; change it there.", still)
			} else {
				w.save(s, restartNote)
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
			w.save(s, restartNote)
		}
	}
	switch {
	case v.portsErr != "" && px > vx:
		// The reason goes below what is chosen.
		py += chipH + 6
		text(w.sans, v.portsErr, vx, py+3, sizeText-2, col(colFaint))
	case px > vx:
	case v.portsErr != "":
		// The reason may be long; its start says most.
		reason := []rune(v.portsErr)
		for len(reason) > 1 && width(w.sans, string(reason), sizeText-2) > right-vx {
			reason = append(reason[:len(reason)-2], '…')
		}
		text(w.sans, string(reason), vx, y+3, sizeText-2, col(colFaint))
	default:
		text(w.mono, "no MIDI ports found", vx, y+3, sizeMono, col(colFaint))
	}
	y = max(py+chipH+26, y+46)

	y = w.drawLights(&s, editable, x, y, right)

	// The two frame limits: one for always, one for while another window
	// is the one in use.
	for _, limit := range []struct {
		name, about string
		value       *int
		live        *int
		choices     []int
	}{
		{"Frame limit", "none keeps to the monitor's refresh rate", &s.Window.FPS, &w.pace.limit, []int{0, 30, 60, 120, 144, 240}},
		{"Frame limit while the window is unfocused", "for less work while another window is in use", &s.Window.BackgroundFPS, &w.pace.background, []int{0, 15, 30, 60}},
	} {
		vx = row(limit.name, limit.about)
		for _, fps := range limit.choices {
			label := fmt.Sprintf("%d", fps)
			if fps == 0 {
				label = "none"
			}
			chip := rl.NewRectangle(vx, y, w.chipWidth(label), chipH)
			if hovered(chip) {
				rl.SetMouseCursor(rl.MouseCursorPointingHand)
			}
			if *limit.value == fps {
				rl.DrawRectangleRounded(chip, 0.4, 6, col(colAccent))
				text(w.mono, label, vx+7, y+3, sizeMono, col(colBar))
			} else {
				rl.DrawRectangleRoundedLinesEx(chip, 0.4, 6, 1, col(colPanelEdge))
				text(w.mono, label, vx+7, y+3, sizeMono, col(colFaint))
			}
			if clicked(chip) && editable && *limit.value != fps {
				*limit.value = fps
				*limit.live = fps // this window follows at once
				w.save(s, "Saved.")
			}
			vx += chip.Width + 6
		}
		y += 46
	}

	vx = row("Height of the log when the window opens", "")
	text(w.mono, fmt.Sprintf("%d", s.Window.LogHeight), vx, y+3, sizeMono, col(colText))
	if now := int(w.logHeight); now != s.Window.LogHeight {
		if hit, _ := w.button(fmt.Sprintf("Keep the present height (%d)", now), vx+50, y-2); hit && editable {
			s.Window.LogHeight = now
			w.save(s, "Saved.")
		}
	}
	y += 44

	// Where things are.
	heading("Where things are")
	for _, place := range []struct{ name, path, label, target string }{
		{"HIDI's directory", p.Root, "Open folder", p.Root},
		{"Settings", p.Settings, "Open file", p.Settings},
		{"Profiles", p.Profiles, "Open folder", p.Profiles},
	} {
		text(w.sans, place.name, x, y+5, sizeText, col(colText))
		bx := x + 130
		if _, err := os.Stat(place.target); err == nil {
			hit, end := w.button(place.label, bx, y)
			if hit {
				w.open(place.target)
			}
			bx = end + 12
		}
		w.drawPath(place.path, bx, y+6, right-bx)
		y += 32
	}
	y += 12

	// Every profile, also those of devices that are not connected.
	heading("Profiles")
	if len(v.profiles) == 0 && len(v.broken) == 0 {
		text(w.sans, "No profiles.", x, y, sizeText, col(colFaint))
		y += 26
	}
	for _, profile := range v.profiles {
		edit, end := w.button("Edit", x, y)
		if edit {
			w.gear.set(false, time.Now()) // the page is under the sheet
			w.editProfile(profile.Source)
		} else if w.unsaved(profile.Source) {
			rl.DrawCircleV(rl.NewVector2(end-2, y+2), 4.5, col(colUnsaved))
		}
		hit, end := w.button("Open file", end+6, y)
		if hit {
			w.open(profile.Source)
		}
		text(w.sans, profile.Name, end+12, y+5, sizeText, col(colText))
		about := filepath.Base(profile.Source) + " · for " + profile.Match.String()
		w.drawPath(about, end+12+width(w.sans, profile.Name, sizeText)+12, y+6, right-end-24-width(w.sans, profile.Name, sizeText))
		y += 32
	}
	for _, problem := range v.broken {
		hit, end := w.button("Open file", x, y)
		if hit {
			w.open(problem.File)
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

// checkbox draws a box that is ticked or not and reports a click on it or
// on the text beside it, which is room wide.
func (w *window) checkbox(ticked bool, x, y, room float32) bool {
	box := rl.NewRectangle(x, y+1, 16, 16)
	if ticked {
		rl.DrawRectangleRounded(box, 0.3, 4, col(colAccent))
		rl.DrawLineEx(rl.NewVector2(x+3.5, y+9), rl.NewVector2(x+7, y+12.5), 2, col(colBar))
		rl.DrawLineEx(rl.NewVector2(x+7, y+12.5), rl.NewVector2(x+12.5, y+5), 2, col(colBar))
	} else {
		rl.DrawRectangleRoundedLinesEx(box, 0.3, 4, 1.5, col(colOff))
	}
	area := rl.NewRectangle(x, y, room, 18)
	if hovered(area) {
		rl.SetMouseCursor(rl.MouseCursorPointingHand)
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
	vx := x + 300
	for _, mode := range []struct{ value, label string }{
		{settings.Blacklist, "blacklist"},
		{settings.Whitelist, "whitelist"},
	} {
		if w.chip(mode.label, s.Devices.Mode == mode.value, vx, y) && editable && s.Devices.Mode != mode.value {
			s.Devices.Mode = mode.value
			w.save(*s, soonNote)
		}
		vx += w.chipWidth(mode.label) + 6
	}
	y += 30

	type entry struct {
		name, about string
		there       bool
	}
	var entries []entry
	listed := map[string]bool{}
	if state != nil {
		for _, d := range state.Devices {
			if listed[d.Name] {
				continue
			}
			listed[d.Name] = true
			about := d.Type
			if !d.Connected {
				about += ", not connected"
			}
			entries = append(entries, entry{d.Name, about, d.Connected})
		}
	}
	// What the settings name and HIDI does not know stays in view, so
	// that it can be taken off the list.
	for _, name := range s.Devices.List {
		if !listed[name] {
			listed[name] = true
			entries = append(entries, entry{name, "not connected", false})
		}
	}
	vx = x + 300
	if len(entries) == 0 {
		hint := "no devices yet"
		if state == nil {
			hint = "start HIDI to see the devices"
		}
		text(w.mono, hint, vx, y+3, sizeMono, col(colFaint))
		return y + 34
	}
	for _, e := range entries {
		ticked := slices.Contains(s.Devices.List, e.name)
		ink := colText
		if !e.there {
			ink = colOff
		}
		room := 24 + width(w.sans, e.name, sizeText) + 10 + width(w.sans, e.about, sizeText-2)
		if w.checkbox(ticked, vx, y, min(room, right-vx)) && editable {
			s.Devices.List = flip(s.Devices.List, e.name)
			w.save(*s, soonNote)
		}
		text(w.sans, e.name, vx+24, y+1, sizeText, col(ink))
		text(w.sans, e.about, vx+24+width(w.sans, e.name, sizeText)+10, y+3, sizeText-2, col(colOff))
		y += 24
	}
	return y + 16
}

// drawLights draws the settings of the lights: whether the keys are lit,
// and the colours, of which the one that is clicked can be changed.
func (w *window) drawLights(s *settings.Settings, editable bool, x, y, right float32) float32 {
	v := &w.settings
	text(w.sans, "Light the keys through OpenRGB", x, y+2, sizeText, col(colText))
	text(w.sans, "needs an OpenRGB server; HIDI follows when it starts again", x, y+20, sizeText-2, col(colFaint))
	vx := x + 300
	if w.pill(s.Lights.OpenRGB, vx, y) && editable {
		s.Lights.OpenRGB = !s.Lights.OpenRGB
		w.save(*s, restartNote)
	}
	text(w.mono, s.Lights.Address, vx+54, y+3, sizeMono, col(colFaint))
	y += 46

	text(w.sans, "Colours of the keys", x, y+2, sizeText, col(colText))
	text(w.sans, "click one to change it; HIDI follows within a few seconds", x, y+20, sizeText-2, col(colFaint))
	// The colours are changed in what the page holds, and written when a
	// slider is let go of.
	colours := v.values.Lights.Colours.Named()
	const cellW = 150
	cols := max(int((right-vx)/cellW), 1)
	for i, n := range colours {
		cx, cy := vx+float32(i%cols)*cellW, y+float32(i/cols)*26
		cell := rl.NewRectangle(cx-4, cy-3, cellW-6, 24)
		if hovered(cell) {
			rl.SetMouseCursor(rl.MouseCursorPointingHand)
		}
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
		rl.DrawRectangleRoundedLinesEx(swatch, 0.3, 4, 1, col(colOff))
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
		released := false
		for i, part := range []struct {
			label string
			value *uint8
			ink   RGB
		}{{"R", &n.Colour.R, RGB{0xe0, 0x5a, 0x5a}}, {"G", &n.Colour.G, RGB{0x4c, 0xc2, 0x7a}}, {"B", &n.Colour.B, RGB{0x5a, 0x9b, 0xe0}}} {
			text(w.mono, part.label, vx, y+2, sizeMono, col(colFaint))
			track := rl.NewRectangle(vx+20, y+7, 200, 6)
			grip := rl.NewRectangle(track.X-8, y, track.Width+16, 20)
			if hovered(grip) {
				rl.SetMouseCursor(rl.MouseCursorPointingHand)
			}
			if clicked(grip) && editable {
				v.slide = i + 1
			}
			if v.slide == i+1 {
				if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
					at := (rl.GetMousePosition().X - track.X) / track.Width
					*part.value = uint8(min(max(at, 0), 1)*255 + 0.5)
				} else {
					v.slide, released = 0, true
				}
			}
			at := track.X + track.Width*float32(*part.value)/255
			rl.DrawRectangleRounded(track, 1, 4, col(colPanelEdge))
			rl.DrawRectangleRounded(rl.NewRectangle(track.X, track.Y, at-track.X, track.Height), 1, 4, col(part.ink))
			rl.DrawCircleV(rl.NewVector2(at, track.Y+3), 7, col(colText))
			text(w.mono, fmt.Sprintf("%3d", *part.value), track.X+track.Width+16, y+2, sizeMono, col(colText))
			y += 24
		}
		if released {
			w.save(v.values, soonNote)
		}
		defaults := light.DefaultColours()
		for _, d := range defaults.Named() {
			if d.Name == n.Name && *d.Colour != *n.Colour {
				if hit, _ := w.button("Back to the default", vx, y+2); hit && editable {
					*n.Colour = *d.Colour
					w.save(v.values, soonNote)
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
	charW := width(w.mono, "0", sizeMono)
	r := []rune(s)
	if most := int(room / charW); most > 1 && len(r) > most {
		r = append([]rune{'…'}, r[len(r)-most+1:]...)
	}
	text(w.mono, string(r), x, y, sizeMono, col(colFaint))
}
