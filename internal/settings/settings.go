package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gethiox/HIDI/internal/control"
)

// Schema is the version of the settings file this HIDI writes.
const Schema = 1

// Settings are HIDI's own settings.
type Settings struct {
	Input  Input
	Output Output
	Window Window
}

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
}

// Output says where the instruments send, besides their own MIDI devices.
type Output struct {
	// Connect names existing MIDI ports every instrument also sends to.
	Connect []string
}

// Window holds what the window remembers.
type Window struct {
	// BackgroundFPS limits how often the window is drawn while it is not
	// in front; 0 keeps to the monitor's refresh rate.
	BackgroundFPS int
	// LogHeight is the height of the log when the window opens.
	LogHeight int
}

// Default returns the settings of a HIDI nobody has changed.
func Default() Settings {
	return Settings{Input: Input{
		Toggle:        []control.Code{control.Key(29), control.Key(42), control.Key(56)}, // left Ctrl, Shift, Alt
		GrabKeyboards: true,
		StartOn:       true,
	}}
}

// file is the settings as they are written. Pointers tell a setting that
// is absent, which keeps its default, from one that is set to nothing.
type file struct {
	Schema int `toml:"schema"`
	Input  struct {
		Toggle        *[]string `toml:"toggle"`
		GrabKeyboards *bool     `toml:"grab_keyboards"`
		GrabGamepads  *bool     `toml:"grab_gamepads"`
		StartOn       *bool     `toml:"start_on"`
	} `toml:"input"`
	Output struct {
		Connect *[]string `toml:"connect"`
	} `toml:"output"`
	Window struct {
		BackgroundFPS *int `toml:"background_fps"`
		LogHeight     *int `toml:"log_height"`
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
	set(&s.Output.Connect, f.Output.Connect)
	set(&s.Window.BackgroundFPS, f.Window.BackgroundFPS)
	set(&s.Window.LogHeight, f.Window.LogHeight)
	if s.Window.BackgroundFPS < 0 || s.Window.LogHeight < 0 {
		problems = append(problems, "window: background_fps and log_height cannot be negative")
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
			quoted[i] = fmt.Sprintf("%q", item)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	toggle := make([]string, len(s.Input.Toggle))
	for i, code := range s.Input.Toggle {
		toggle[i] = code.String()
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

[output]
# Existing MIDI ports that every instrument also sends to, by a part of
# their name or by their ID as "hidi ports" lists them.
connect = %s

[window]
# Draw the window at most this many times a second while another window is
# in front. 0 keeps to the monitor's refresh rate.
background_fps = %d

# The height of the log when the window opens, in pixels. 0 shows only its
# last line.
log_height = %d
`, Schema, list(toggle), s.Input.GrabKeyboards, s.Input.GrabGamepads, s.Input.StartOn,
		list(s.Output.Connect), s.Window.BackgroundFPS, s.Window.LogHeight)
	return []byte(b.String())
}
