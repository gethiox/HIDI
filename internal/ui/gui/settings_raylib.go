//go:build (linux || windows) && (amd64 || arm64) && !cgo

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/gethiox/HIDI/internal/config"
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
	scroll   float32
	content  float32 // the height of the page when it was last drawn
}

const settingsRefresh = 2 * time.Second

func (v *settingsView) refresh(p settings.Paths, now time.Time, force bool) {
	if !force && now.Sub(v.loaded) < settingsRefresh {
		return
	}
	v.loaded = now
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
			w.settings.message = ""
			w.settings.refresh(w.opts.Paths, now, true)
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
	w.settings.refresh(w.opts.Paths, time.Now(), true)
}

// What the page says after a change, by when a running HIDI follows it.
const (
	soonNote    = "Saved. A running HIDI takes this over within a few seconds."
	restartNote = "Saved. A running HIDI takes this over when it is started again."
)

// drawOverlay draws the settings as a sheet over the body of the window:
// smaller than it, so that the instruments show around it, and not quite
// opaque, so that they show through. A click beside the sheet closes it.
func (w *window) drawOverlay(body rl.Rectangle) {
	inset := min(max(body.Width*0.05, 14), 60)
	sheet := rl.NewRectangle(body.X+inset, body.Y+6, body.Width-2*inset, body.Height-18)
	if sheet.Height < 60 {
		sheet = rl.NewRectangle(body.X, body.Y, body.Width, body.Height)
	}
	rl.DrawRectangleRec(body, rl.NewColor(0, 0, 0, 70))
	roundness := 20 / min(sheet.Width, sheet.Height)
	rl.DrawRectangleRounded(sheet, roundness, 8, colA(colBar, 226))
	rl.DrawRectangleRoundedLinesEx(sheet, roundness, 8, 1, col(colPanelEdge))
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) && hovered(body) && !hovered(sheet) {
		w.gear.set(false, time.Now())
		return
	}
	w.drawSettings(rl.NewRectangle(sheet.X+2, sheet.Y+2, sheet.Width-4, sheet.Height-4))
}

// drawSettings draws the settings page.
func (w *window) drawSettings(area rl.Rectangle) {
	v := &w.settings
	p := w.opts.Paths
	v.refresh(p, time.Now(), false)

	// The page is as long as it was when last drawn, which is what the
	// scrolling is held to before anything is drawn.
	v.scroll -= wheel(area) * 40
	v.scroll = min(max(v.scroll, 0), max(v.content-area.Height, 0))
	rl.BeginScissorMode(int32(area.X), int32(area.Y), int32(area.Width), int32(area.Height))
	defer rl.EndScissorMode()

	x, y := area.X+margin, area.Y+12-v.scroll
	right := area.X + area.Width - margin
	defer func() { v.content = y + v.scroll - area.Y + 14 }()
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
	vx := row("Toggle combination", "turns HIDI off and on; change it in settings.toml, HIDI follows")
	combination := strings.Join(toggleNames, " + ")
	if combination == "" {
		combination = "none"
	}
	text(w.mono, combination, vx, y+3, sizeMono, col(colText))
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

	vx = row("Also send to MIDI ports", "change it in settings.toml and start HIDI again")
	ports := strings.Join(s.Output.Connect, ", ")
	if ports == "" {
		ports = "none"
	}
	text(w.mono, ports, vx, y+3, sizeMono, col(colText))
	y += 46

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
			w.editProfile(profile.Source)
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
