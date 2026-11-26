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

// Endpoint returns where the OpenRGB server is looked for: the address of
// the settings, or the usual one where they name none.
func (l Lights) Endpoint() string {
	if l.Address == "" {
		return DefaultOpenRGBAddress
	}
	return l.Address
}

// MinScale and MaxScale bound the size of the window's contents, in
// hundredths.
const (
	MinScale = 50
	MaxScale = 300
)

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
	// ToggleExclusive keeps a toggle of one key from the rest of the
	// system altogether: the key is HIDI's alone, also while HIDI is off.
	ToggleExclusive bool
	// LearnThreshold is how far an axis has to be moved to get through
	// in learning mode, in hundredths of its range.
	LearnThreshold int
	// Hysteresis is the hysteresis of an axis that acts as a button, in
	// hundredths of its travel: the axis has to pass its trigger point
	// by half of it to count as pressed, and to come back behind the
	// point by half of it to count as let go.
	Hysteresis int
	// VelocityStep is how far velocity_up and velocity_down move the
	// velocity, from 1 to 127. The velocity moves along the values that
	// many apart from the profile's own, and stops at 1 and 127.
	VelocityStep int
}

// Output says where the instruments send, besides their own MIDI devices.
type Output struct {
	// Connect names existing MIDI ports every instrument also sends to.
	Connect []string
	// ResetPitchBend returns the pitch bend to rest when HIDI is turned
	// off, and ResetControllers every controller. What is not returned
	// stays as it was last sent.
	ResetPitchBend, ResetControllers bool
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
	// Scale is the size of everything the window draws, in hundredths
	// of its ordinary size.
	Scale int
	// ControlNames shows the buttons and axes of game controllers by
	// the names of their codes, such as BTN_SOUTH, where the window
	// would otherwise number them.
	ControlNames bool
	// CheckUpdates lets the window ask GitHub whether a newer HIDI has
	// been released. It is off unless the user turns it on.
	CheckUpdates bool
}

// Default returns the settings of a HIDI nobody has changed.
func Default() Settings {
	return Settings{
		Input: Input{
			// Caps Lock, tapped, and HIDI's alone: one key that does
			// nothing else any more. HIDI starts turned off, with its
			// window open, so that a keyboard types until it is asked
			// to play.
			ToggleTap:       true,
			ToggleExclusive: true,
			GrabKeyboards:   true,
			StartOn:         false,
			LearnThreshold:  50,
			Hysteresis:      2,
			VelocityStep:    16,
		},
		Output:  Output{ResetPitchBend: true},
		Devices: Devices{Mode: Blacklist},
		Lights:  Lights{Colours: light.DefaultColours()},
		Window:  Window{Scale: 100, ShowAtStart: true},
	}
}

// file is the settings as they are written. Pointers tell a setting that
// is absent, which keeps its default, from one that is set to nothing.
type file struct {
	Schema int `toml:"schema"`
	Input  struct {
		Toggle          *[]string `toml:"toggle"`
		GrabKeyboards   *bool     `toml:"grab_keyboards"`
		GrabGamepads    *bool     `toml:"grab_gamepads"`
		StartOn         *bool     `toml:"start_on"`
		ToggleTap       *bool     `toml:"toggle_tap"`
		AlwaysGrab      *bool     `toml:"always_grab"`
		LearnThreshold  *int      `toml:"learn_threshold"`
		Hysteresis      *int      `toml:"hysteresis"`
		VelocityStep    *int      `toml:"velocity_step"`
		ToggleExclusive *bool     `toml:"toggle_exclusive"`
	} `toml:"input"`
	Devices struct {
		Mode *string   `toml:"mode"`
		List *[]string `toml:"list"`
	} `toml:"devices"`
	Output struct {
		Connect          *[]string `toml:"connect"`
		ResetPitchBend   *bool     `toml:"reset_pitch_bend"`
		ResetControllers *bool     `toml:"reset_controllers"`
	} `toml:"output"`
	Lights struct {
		OpenRGB *bool             `toml:"openrgb"`
		Address *string           `toml:"address"`
		Colours map[string]string `toml:"colours"`
	} `toml:"lights"`
	Window struct {
		ShowAtStart   *bool `toml:"show_at_start"`
		FPS           *int  `toml:"fps"`
		BackgroundFPS *int  `toml:"background_fps"`
		Scale         *int  `toml:"scale"`
		// LogHeight, ShowInput and ShowOutput were settings once: how
		// the window was left, which is no setting and is kept
		// elsewhere now. A file that still has them is read, and the
		// next writing of the file leaves them out.
		LogHeight    *int      `toml:"log_height"`
		ShowInput    *[]string `toml:"show_input"`
		ShowOutput   *[]string `toml:"show_output"`
		CheckUpdates *bool     `toml:"check_updates"`
		ControlNames *bool     `toml:"control_names"`
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
	set(&s.Input.ToggleExclusive, f.Input.ToggleExclusive)
	if s.Input.LearnThreshold < 1 || s.Input.LearnThreshold > 100 {
		problems = append(problems, fmt.Sprintf("input.learn_threshold: %d is not a number of hundredths from 1 to 100", s.Input.LearnThreshold))
	}
	set(&s.Devices.Mode, f.Devices.Mode)
	set(&s.Devices.List, f.Devices.List)
	if s.Devices.Mode != Blacklist && s.Devices.Mode != Whitelist {
		problems = append(problems, fmt.Sprintf("devices.mode: %q is neither %q nor %q", s.Devices.Mode, Blacklist, Whitelist))
	}
	set(&s.Output.Connect, f.Output.Connect)
	set(&s.Output.ResetPitchBend, f.Output.ResetPitchBend)
	set(&s.Output.ResetControllers, f.Output.ResetControllers)
	set(&s.Input.Hysteresis, f.Input.Hysteresis)
	if s.Input.Hysteresis < 0 || s.Input.Hysteresis > 100 {
		problems = append(problems, fmt.Sprintf("input.hysteresis: %d is not a number of hundredths from 0 to 100", s.Input.Hysteresis))
	}
	set(&s.Input.VelocityStep, f.Input.VelocityStep)
	if s.Input.VelocityStep < 1 || s.Input.VelocityStep > 127 {
		problems = append(problems, fmt.Sprintf("input.velocity_step: %d is not a step from 1 to 127", s.Input.VelocityStep))
	}
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
	set(&s.Window.ShowAtStart, f.Window.ShowAtStart)
	set(&s.Window.FPS, f.Window.FPS)
	set(&s.Window.BackgroundFPS, f.Window.BackgroundFPS)
	set(&s.Window.Scale, f.Window.Scale)
	if s.Window.Scale < MinScale || s.Window.Scale > MaxScale {
		problems = append(problems, fmt.Sprintf("window.scale: %d is not a size in hundredths from %d to %d", s.Window.Scale, MinScale, MaxScale))
	}
	set(&s.Window.CheckUpdates, f.Window.CheckUpdates)
	set(&s.Window.ControlNames, f.Window.ControlNames)
	if s.Window.FPS < 0 || s.Window.BackgroundFPS < 0 {
		problems = append(problems, "window: fps and background_fps cannot be negative")
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
	host, port, err := net.SplitHostPort(s.Lights.Endpoint())
	if n, perr := strconv.Atoi(port); err != nil || host == "" || perr != nil || n < 1 || n > 65535 {
		problems = append(problems, fmt.Sprintf("lights.address: %q is not a host and a port, such as %q", s.Lights.Address, DefaultOpenRGBAddress))
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
# What turns HIDI off and on. Left empty, every profile says it for the
# devices that play with it: a key, or several pressed together, bound to
# "hidi_toggle". The keyboard profile HIDI comes with uses Caps Lock.
# Keys named here are the toggle for every device instead, whatever the
# profiles say: ["KEY_SCROLLLOCK"], or several to be pressed together.
toggle = %s

# Keep keyboards from typing while HIDI is on. A keyboard that has no
# toggle, by its profile or by the line above, is left typing all the
# same: nothing on it could turn HIDI off again.
grab_keyboards = %t

# Also keep game controllers from other programs while HIDI is on.
# Windows cannot do that.
grab_gamepads = %t

# Whether HIDI is on when it starts.
start_on = %t

# A toggle of one key, such as Caps Lock, works by a tap, and the key
# plays nothing. Held for longer than half a second it does not toggle,
# and does what it normally does instead. On Windows the tap is hidden
# from the system. On Linux it is hidden only with always_grab; without
# it, the tap that turns HIDI on also reaches the system. A toggle of
# several keys is pressed, whatever this says.
toggle_tap = %t

# Linux only, and only of use with toggle_tap. Keep keyboards grabbed
# while HIDI is off too, and type their keys again on a keyboard of HIDI's
# own, so that the tapped key never reaches the system. This puts HIDI
# between your keyboard and everything you type for as long as it runs:
# should HIDI ever hang, the keyboard does nothing until HIDI is ended.
# It needs access to /dev/uinput.
always_grab = %t

# Keep a toggle of one key from the rest of the system altogether: the
# key is HIDI's alone and never does what it normally does, also while
# HIDI is off. On Linux HIDI then keeps keyboards grabbed all the time,
# as with always_grab.
toggle_exclusive = %t

# Learning mode makes every axis stand still until it is moved by more
# than this many hundredths of its range. It is for teaching a program
# which controller to listen to, when the program takes the first one that
# changes at all.
learn_threshold = %d

# The hysteresis of an axis that acts as a button, in hundredths of its
# travel. An axis that plays a note or runs a command does so when it
# passes a point, half of its way unless the profile says otherwise. With
# a hysteresis it has to pass the point by half of this to count as
# pressed, and to come back behind the point by half of this to count as
# let go, so that an axis that rests near the point does not trigger over
# and over. Neither goes past the ends of the travel: 100 means pressed at
# the far end and let go at rest, wherever the point is. 0 turns it off.
hysteresis = %d

# How far velocity_up and velocity_down move the velocity of the notes.
# The velocity goes along the values this far apart from the velocity the
# profile starts with, and stops at 127 and 1; the step back from either
# end goes to the value before, so the profile's velocity is always
# reached again.
velocity_step = %d

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

# What turning HIDI off does to what the instruments sent, beside ending
# every note. The pitch bend goes back to no bend, so that nothing stays
# out of tune. Controllers stay as they were last sent, a volume for one,
# unless reset_controllers is true: then every controller goes back to
# its rest as well.
reset_pitch_bend = %t
reset_controllers = %t

[lights]
# Light the keys of keyboards to show what they play, through an OpenRGB
# server, which is a program of its own that has to be running.
openrgb = %t

# Where the OpenRGB server listens. Empty means the usual place,
# 127.0.0.1:6742.
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

# The size of everything the window draws, in hundredths of its ordinary
# size: 125 makes it a quarter larger.
scale = %d

# Ask GitHub now and then whether a newer HIDI has been released, and say
# so in the corner of the window. Nothing is downloaded or installed, and
# nothing about you or your devices is sent: it is one request for the
# newest release of the project. Off unless you turn it on.
check_updates = %t

# The buttons and axes of a game controller are numbered in the window,
# from 0, in an order that is the same whenever a controller of that kind
# is plugged in. With this the names profiles use for them, such as
# BTN_SOUTH and ABS_X, stand beside the numbers.
control_names = %t
`, Schema, list(toggle), s.Input.GrabKeyboards, s.Input.GrabGamepads, s.Input.StartOn, s.Input.ToggleTap, s.Input.AlwaysGrab, s.Input.ToggleExclusive, s.Input.LearnThreshold, s.Input.Hysteresis, s.Input.VelocityStep,
		tomlString(s.Devices.Mode), list(s.Devices.List),
		list(s.Output.Connect), s.Output.ResetPitchBend, s.Output.ResetControllers, s.Lights.OpenRGB, tomlString(s.Lights.Address), colours.String(),
		s.Window.ShowAtStart, s.Window.FPS, s.Window.BackgroundFPS, s.Window.Scale,
		s.Window.CheckUpdates, s.Window.ControlNames)
	return []byte(b.String())
}

// Save writes the settings to their file, whole or not at all.
func Save(path string, s Settings) error {
	if problems := s.Check(); len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return writeFile(path, s.Text(), 0o644)
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
