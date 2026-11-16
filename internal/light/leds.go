package light

import (
	"strings"

	"github.com/gethiox/HIDI/internal/control"
)

// OpenRGB names the lights of a keyboard after its keys, "Key: A" and so
// on. ledKeys says which key each of those names means, by the name of the
// key in HIDI.
var ledKeys = map[string]string{
	"Escape": "KEY_ESC", "`": "KEY_GRAVE", "Tab": "KEY_TAB", "Caps Lock": "KEY_CAPSLOCK",
	"Left Shift": "KEY_LEFTSHIFT", "Left Control": "KEY_LEFTCTRL", "Left Windows": "KEY_LEFTMETA", "Left Alt": "KEY_LEFTALT",
	"Space": "KEY_SPACE", "Right Alt": "KEY_RIGHTALT", "Right Windows": "KEY_RIGHTMETA", "Menu": "KEY_COMPOSE",
	"Right Control": "KEY_RIGHTCTRL", "Right Shift": "KEY_RIGHTSHIFT", "Enter": "KEY_ENTER", "Backspace": "KEY_BACKSPACE",
	"-": "KEY_MINUS", "=": "KEY_EQUAL", "[": "KEY_LEFTBRACE", "]": "KEY_RIGHTBRACE", "\\ (ANSI)": "KEY_BACKSLASH",
	"\\ (ISO)": "KEY_102ND", "#": "KEY_BACKSLASH", ";": "KEY_SEMICOLON", "'": "KEY_APOSTROPHE",
	",": "KEY_COMMA", ".": "KEY_DOT", "/": "KEY_SLASH",
	"Print Screen": "KEY_SYSRQ", "Scroll Lock": "KEY_SCROLLLOCK", "Pause/Break": "KEY_PAUSE",
	"Insert": "KEY_INSERT", "Home": "KEY_HOME", "Page Up": "KEY_PAGEUP",
	"Delete": "KEY_DELETE", "End": "KEY_END", "Page Down": "KEY_PAGEDOWN",
	"Up Arrow": "KEY_UP", "Down Arrow": "KEY_DOWN", "Left Arrow": "KEY_LEFT", "Right Arrow": "KEY_RIGHT",
	"Num Lock": "KEY_NUMLOCK", "Number Pad /": "KEY_KPSLASH", "Number Pad *": "KEY_KPASTERISK",
	"Number Pad -": "KEY_KPMINUS", "Number Pad +": "KEY_KPPLUS", "Number Pad Enter": "KEY_KPENTER", "Number Pad .": "KEY_KPDOT",
	"Media Mute": "KEY_MUTE", "Media Previous": "KEY_PREVIOUSSONG", "Media Next": "KEY_NEXTSONG",
	"Media Play/Pause": "KEY_PLAYPAUSE", "Media Stop": "KEY_STOPCD",
	"Media Volume +": "KEY_VOLUMEUP", "Media Volume -": "KEY_VOLUMEDOWN",
}

// KeyOfLED returns the key a light of a keyboard belongs to, from the name
// OpenRGB gives the light. Lights that are not under a key, a logo or a
// strip, belong to none.
func KeyOfLED(name string) (control.Code, bool) {
	rest, ok := strings.CutPrefix(name, "Key: ")
	if !ok {
		return control.Code{}, false
	}
	keyName, known := ledKeys[rest]
	switch {
	case known:
	case len(rest) == 1 && (rest[0] >= 'A' && rest[0] <= 'Z' || rest[0] >= '0' && rest[0] <= '9'):
		keyName = "KEY_" + rest
	case len(rest) >= 2 && rest[0] == 'F' && allDigits(rest[1:]):
		keyName = "KEY_" + rest
	case strings.HasPrefix(rest, "Number Pad ") && len(rest) == len("Number Pad ")+1 && allDigits(rest[len(rest)-1:]):
		keyName = "KEY_KP" + rest[len(rest)-1:]
	default:
		return control.Code{}, false
	}
	code, err := control.ParseCode(keyName)
	return code, err == nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
