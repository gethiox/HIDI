package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/light"
)

// Schema is the version of the settings file this HIDI writes.
const Schema = 1

// Settings are HIDI's own settings.
type Settings struct {
	Input   Input
	Devices Devices
	Output  Output
	Lights  Lights
	Window  Window
	Shown   Shown
}

// The two ways the list of devices is read.
const (
	// Blacklist: every device plays except those listed.
	Blacklist = "blacklist"
	// Whitelist: only the listed devices play.
	Whitelist = "whitelist"
)

// Devices says which devices become instruments, by their names.
type Devices struct {
	// Mode is [Blacklist] or [Whitelist].
	Mode string
	// List holds the names of devices, as HIDI shows them.
	List []string
}

// Allowed reports whether the device with this name may be an instrument.
func (d Devices) Allowed(name string) bool {
	listed := false
	for _, entry := range d.List {
		if entry == name {
			listed = true
		}
	}
	return listed == (d.Mode == Whitelist)
}

// Shown says for which instruments the window shows more than the two
// lines every one of them has, by the names of their devices.
type Shown struct {
	// Input lists the devices whose keys, buttons and axes are drawn,
	// and Output those whose keyboard of notes is.
	Input, Output []string
}

// Lights says whether and how HIDI lights the keys of keyboards.
type Lights struct {
	// OpenRGB turns on lighting through an OpenRGB server.
	OpenRGB bool
	// Address is where that server listens.
	Address string
	// Colours are the colours of the keys.
	Colours light.Colours
}

// DefaultOpenRGBAddress is where an OpenRGB server listens unless it is
// told otherwise.
const DefaultOpenRGBAddress = "127.0.0.1:6742"

// Input says how HIDI treats input devices.
type Input struct {
	// Toggle is the keys that, pressed together on one device, turn HIDI
	// off and on. Empty means there is no such combination.
	Toggle []control.Code
	// GrabKeyboards keeps keyboards from typing while HIDI is on.
	GrabKeyboards bool
	// GrabGamepads also keeps game controllers from other programs.
	GrabGamepads bool
	// StartOn says whether HIDI is on when it starts.
	StartOn bool
	// ToggleTap makes a toggle of one key work by a tap: pressed and let
	// go within half a second it turns HIDI off or on, and held for
	// longer it does what the key normally does. On Windows the tap is
	// hidden from the system.
	ToggleTap bool
	// AlwaysGrab, on Linux, keeps keyboards grabbed while HIDI is off as
	// well and types their keys again on a keyboard of HIDI's own, so
	// that the tapped key can be hidden from the system there too.
	AlwaysGrab bool
	// LearnThreshold is how far an axis has to be moved to get through
	// in learning mode, in hundredths of its range.
	LearnThreshold int
}

// Output says where the instruments send, besides their own MIDI devices.
type Output struct {
	// Connect names existing MIDI ports every instrument also sends to.
	Connect []string
}

// Window holds what the window remembers.
type Window struct {
	// ShowAtStart opens the window when HIDI starts as an application.
	// Without it HIDI starts as an icon in the tray only.
	ShowAtStart bool
	// FPS limits how often the window is drawn; 0 keeps to the monitor's
	// refresh rate, which is also the most it is ever drawn.
	FPS int
	// BackgroundFPS limits how often the window is drawn while it is not
	// in front; 0 keeps to the monitor's refresh rate.
	BackgroundFPS int
	// LogHeight is the height of the log when the window opens.
	LogHeight int
}

// Default returns the settings of a HIDI nobody has changed.
func Default() Settings {
	return Settings{
		Input: Input{
			Toggle:         []control.Code{control.Key(29), control.Key(42), control.Key(56)}, // left Ctrl, Shift, Alt
			GrabKeyboards:  true,
			StartOn:        true,
			LearnThreshold: 20,
		},
		Devices: Devices{Mode: Blacklist},
		Lights:  Lights{Address: DefaultOpenRGBAddress, Colours: light.DefaultColours()},
	}
}

// file is the settings as they are written. Pointers tell a setting that
// is absent, which keeps its default, from one that is set to nothing.
type file struct {
	Schema int `toml:"schema"`
	Input  struct {
		Toggle         *[]string `toml:"toggle"`
		GrabKeyboards  *bool     `toml:"grab_keyboards"`
		GrabGamepads   *bool     `toml:"grab_gamepads"`
		StartOn        *bool     `toml:"start_on"`
		ToggleTap      *bool     `toml:"toggle_tap"`
		AlwaysGrab     *bool     `toml:"always_grab"`
		LearnThreshold *int      `toml:"learn_threshold"`
	} `toml:"input"`
	Devices struct {
		Mode *string   `toml:"mode"`
		List *[]string `toml:"list"`
	} `toml:"devices"`
	Output struct {
		Connect *[]string `toml:"connect"`
	} `toml:"output"`
	Lights struct {
		OpenRGB *bool             `toml:"openrgb"`
		Address *string           `toml:"address"`
		Colours map[string]string `toml:"colours"`
	} `toml:"lights"`
	Window struct {
		ShowAtStart   *bool     `toml:"show_at_start"`
		FPS           *int      `toml:"fps"`
		BackgroundFPS *int      `toml:"background_fps"`
		LogHeight     *int      `toml:"log_height"`
		ShowInput     *[]string `toml:"show_input"`
		ShowOutput    *[]string `toml:"show_output"`
	} `toml:"window"`
}

// Parse reads settings. What the text does not mention keeps its default.
// Every mistake is reported, one to a line.
func Parse(data []byte) (Settings, error) {
	s := Default()
	var f file
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			var lines []string
			for _, e := range strict.Errors {
				lines = append(lines, fmt.Sprintf("unknown setting %q", strings.Join(e.Key(), ".")))
			}
			return s, errors.New(strings.Join(lines, "\n"))
		}
		return s, err
	}
	var problems []string
	if f.Schema == 0 {
		problems = append(problems, "the file does not say which schema it is written for; add \"schema = 1\"")
	} else if f.Schema > Schema {
		problems = append(problems, fmt.Sprintf("the file is written for schema %d, this HIDI knows up to %d", f.Schema, Schema))
	}
	if f.Input.Toggle != nil {
		s.Input.Toggle = nil
		for _, name := range *f.Input.Toggle {
			code, err := control.ParseCode(name)
			if err != nil || code.Kind != control.KindKey {
				problems = append(problems, fmt.Sprintf("input.toggle: %q is not the name of a key", name))
				continue
			}
			s.Input.Toggle = append(s.Input.Toggle, code)
		}
	}
	set(&s.Input.GrabKeyboards, f.Input.GrabKeyboards)
	set(&s.Input.GrabGamepads, f.Input.GrabGamepads)
	set(&s.Input.StartOn, f.Input.StartOn)
	set(&s.Input.ToggleTap, f.Input.ToggleTap)
	set(&s.Input.AlwaysGrab, f.Input.AlwaysGrab)
	set(&s.Input.LearnThreshold, f.Input.LearnThreshold)
	if s.Input.LearnThreshold < 1 || s.Input.LearnThreshold > 100 {
		problems = append(problems, fmt.Sprintf("input.learn_threshold: %d is not a number of hundredths from 1 to 100", s.Input.LearnThreshold))
	}
	set(&s.Devices.Mode, f.Devices.Mode)
	set(&s.Devices.List, f.Devices.List)
	if s.Devices.Mode != Blacklist && s.Devices.Mode != Whitelist {
		problems = append(problems, fmt.Sprintf("devices.mode: %q is neither %q nor %q", s.Devices.Mode, Blacklist, Whitelist))
	}
	set(&s.Output.Connect, f.Output.Connect)
	set(&s.Lights.OpenRGB, f.Lights.OpenRGB)
	set(&s.Lights.Address, f.Lights.Address)
	named := map[string]*light.RGB{}
	for _, n := range s.Lights.Colours.Named() {
		named[n.Name] = n.Colour
	}
	for _, name := range slices.Sorted(maps.Keys(f.Lights.Colours)) {
		target, known := named[name]
		if !known {
			problems = append(problems, fmt.Sprintf("unknown setting %q", "lights.colours."+name))
			continue
		}
		colour, err := ParseColour(f.Lights.Colours[name])
		if err != nil {
			problems = append(problems, fmt.Sprintf("lights.colours.%s: %v", name, err))
			continue
		}
		*target = colour
	}
	if _, _, err := net.SplitHostPort(s.Lights.Address); err != nil {
		problems = append(problems, fmt.Sprintf("lights.address: %q is not a host and a port, such as %q", s.Lights.Address, DefaultOpenRGBAddress))
	}
	set(&s.Window.ShowAtStart, f.Window.ShowAtStart)
	set(&s.Window.FPS, f.Window.FPS)
	set(&s.Window.BackgroundFPS, f.Window.BackgroundFPS)
	set(&s.Window.LogHeight, f.Window.LogHeight)
	set(&s.Shown.Input, f.Window.ShowInput)
	set(&s.Shown.Output, f.Window.ShowOutput)
	if s.Window.FPS < 0 || s.Window.BackgroundFPS < 0 || s.Window.LogHeight < 0 {
		problems = append(problems, "window: fps, background_fps and log_height cannot be negative")
	}
	if len(problems) == 0 {
		problems = append(problems, s.Check()...)
	}
	if len(problems) > 0 {
		return Default(), errors.New(strings.Join(problems, "\n"))
	}
	return s, nil
}

func set[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// ParseColour reads a colour written as "#rrggbb".
func ParseColour(text string) (light.RGB, error) {
	var c light.RGB
	if len(text) != 7 || text[0] != '#' {
		return c, fmt.Errorf("%q is not a colour written as \"#rrggbb\"", text)
	}
	v, err := strconv.ParseUint(text[1:], 16, 32)
	if err != nil {
		return c, fmt.Errorf("%q is not a colour written as \"#rrggbb\"", text)
	}
	return light.RGB{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
}

// FormatColour writes a colour as "#rrggbb".
func FormatColour(c light.RGB) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Check returns what is wrong with settings that each make sense alone.
func (s Settings) Check() []string {
	var problems []string
	if len(s.Input.Toggle) == 0 && s.Input.GrabKeyboards {
		// A grabbed keyboard cannot type, so without the combination a
		// user with one keyboard could not get it back.
		problems = append(problems, "input: grabbing keyboards needs a toggle combination to release them; set toggle, or set grab_keyboards to false")
	}
	if len(s.Input.Toggle) == 0 && !s.Input.StartOn {
		problems = append(problems, "input: starting turned off needs a toggle combination to turn HIDI on")
	}
	if s.Input.ToggleTap && len(s.Input.Toggle) != 1 {
		problems = append(problems, "input: toggle_tap needs a toggle of exactly one key, such as [\"KEY_CAPSLOCK\"]")
	}
	return problems
}

// Load reads the settings file. A file that does not exist gives the
// defaults and no error. A file with mistakes gives the defaults and says
// what is wrong.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	return Parse(data)
}

// Text writes settings as a file, with a note on every setting.
func (s Settings) Text() []byte {
	list := func(items []string) string {
		quoted := make([]string, len(items))
		for i, item := range items {
			quoted[i] = tomlString(item)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	toggle := make([]string, len(s.Input.Toggle))
	for i, code := range s.Input.Toggle {
		toggle[i] = code.String()
	}
	var colours strings.Builder
	for _, n := range s.Lights.Colours.Named() {
		fmt.Fprintf(&colours, "# %s\n%s = %s\n", n.About, n.Name, tomlString(FormatColour(*n.Colour)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `# HIDI's own settings. The profiles, which say how a device plays, are the
# files in the directory "profiles" beside this one.
#
# HIDI reads this file when it starts. A setting that is left out keeps its
# default; options given on the command line come before what is here.

schema = %d

[input]
# The keys that, pressed together on one device, turn HIDI off and on.
# An empty list means there is no such combination.
toggle = %s

# Keep keyboards from typing while HIDI is on. This needs a toggle
# combination, or a single keyboard could never be got back.
grab_keyboards = %t

# Also keep game controllers from other programs while HIDI is on.
# Windows cannot do that.
grab_gamepads = %t

# Whether HIDI is on when it starts.
start_on = %t

# Turn HIDI off and on by tapping one key, such as Caps Lock: set toggle to
# that one key and this to true. The key then plays nothing. Held for
# longer than half a second it does not toggle, and does what it normally
# does instead. On Windows the tap is hidden from the system. On Linux it
# is hidden only with always_grab; without it, the tap that turns HIDI on
# also reaches the system.
toggle_tap = %t

# Linux only, and only of use with toggle_tap. Keep keyboards grabbed
# while HIDI is off too, and type their keys again on a keyboard of HIDI's
# own, so that the tapped key never reaches the system. This puts HIDI
# between your keyboard and everything you type for as long as it runs:
# should HIDI ever hang, the keyboard does nothing until HIDI is ended.
# It needs access to /dev/uinput.
always_grab = %t

# Learning mode makes every axis stand still until it is moved by more
# than this many hundredths of its range. It is for teaching a program
# which controller to listen to, when the program takes the first one that
# changes at all.
learn_threshold = %d

[devices]
# Which devices become instruments, by their names as HIDI shows them.
# With "blacklist" every keyboard and game controller plays except the
# listed ones; with "whitelist" only the listed ones do.
mode = %s
list = %s

[output]
# Existing MIDI ports that every instrument also sends to, by a part of
# their name or by their ID as "hidi ports" lists them.
connect = %s

[lights]
# Light the keys of keyboards to show what they play, through an OpenRGB
# server, which is a program of its own that has to be running.
openrgb = %t

# Where the OpenRGB server listens.
address = %s

# The colours of the keys, each as "#rrggbb". Keys that change the MIDI
# channel take the colour of the channel instead, and a note another
# program sends on another channel that of its channel.
[lights.colours]
%s
[window]
# Open the window when HIDI starts. Without this HIDI starts as an icon in
# the tray, and the window is opened from there.
show_at_start = %t

# Draw the window at most this many times a second. 0 sets no limit: the
# window then keeps to the refresh rate of the monitor it is on, which is
# also the most it is ever drawn.
fps = %d

# The same limit for while the window is not the one in use: another
# window has the focus. 0 sets no limit of its own.
background_fps = %d

# The height of the log when the window opens, in pixels. 0 shows only its
# last line.
log_height = %d

# The instruments whose input is drawn in the window, and those whose
# notes are: by the names of their devices. The window keeps these lists.
show_input = %s
show_output = %s
`, Schema, list(toggle), s.Input.GrabKeyboards, s.Input.GrabGamepads, s.Input.StartOn, s.Input.ToggleTap, s.Input.AlwaysGrab, s.Input.LearnThreshold,
		tomlString(s.Devices.Mode), list(s.Devices.List),
		list(s.Output.Connect), s.Lights.OpenRGB, tomlString(s.Lights.Address), colours.String(),
		s.Window.ShowAtStart, s.Window.FPS, s.Window.BackgroundFPS, s.Window.LogHeight,
		list(s.Shown.Input), list(s.Shown.Output))
	return []byte(b.String())
}

// Save writes the settings to their file, whole or not at all.
func Save(path string, s Settings) error {
	if problems := s.Check(); len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return writeFile(path, s.Text())
}

// tomlString writes text as a TOML string. Go's own quoting is close and
// not the same: it writes a bell as \a, which TOML does not know, and a
// file with that in it would not be read again. The name of a MIDI port
// can hold anything.
func tomlString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range strings.ToValidUTF8(text, "\uFFFD") {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
