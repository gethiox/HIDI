package gui

// KeyCap is one key of the drawn computer keyboard. Positions and sizes
// are in units of one ordinary key.
type KeyCap struct {
	// Name is the control's name, such as "KEY_A".
	Name string
	// Label is what the key shows.
	Label      string
	X, Y, W, H float32
}

// KeyboardUnits is the size of the drawn keyboard, in key units.
const (
	KeyboardUnitsW = 22.5
	KeyboardUnitsH = 6.5
)

// item is a key in a row of the layout, or a gap when it has no name.
type item struct {
	name, label string
	w           float32
	h           float32 // 0 means one row
}

func k(name, label string) item               { return item{name: name, label: label, w: 1} }
func wide(name, label string, w float32) item { return item{name: name, label: label, w: w} }
func gap(w float32) item                      { return item{w: w} }

// The full-size layout of American keyboards, the most common one: 104
// keys. The names are positions, so a keyboard for another language lights
// the key in the same place whatever is printed on it; the one or two keys
// such a keyboard has more are listed beside the drawing when pressed.
var keyboardRows = []struct {
	y     float32
	items []item
}{
	{0, []item{
		k("KEY_ESC", "Esc"), gap(1),
		k("KEY_F1", "F1"), k("KEY_F2", "F2"), k("KEY_F3", "F3"), k("KEY_F4", "F4"), gap(0.5),
		k("KEY_F5", "F5"), k("KEY_F6", "F6"), k("KEY_F7", "F7"), k("KEY_F8", "F8"), gap(0.5),
		k("KEY_F9", "F9"), k("KEY_F10", "F10"), k("KEY_F11", "F11"), k("KEY_F12", "F12"), gap(0.25),
		k("KEY_SYSRQ", "Prt"), k("KEY_SCROLLLOCK", "Scr"), k("KEY_PAUSE", "Pau"),
	}},
	{1.5, []item{
		k("KEY_GRAVE", "`"), k("KEY_1", "1"), k("KEY_2", "2"), k("KEY_3", "3"), k("KEY_4", "4"), k("KEY_5", "5"),
		k("KEY_6", "6"), k("KEY_7", "7"), k("KEY_8", "8"), k("KEY_9", "9"), k("KEY_0", "0"),
		k("KEY_MINUS", "-"), k("KEY_EQUAL", "="), wide("KEY_BACKSPACE", "Bksp", 2), gap(0.25),
		k("KEY_INSERT", "Ins"), k("KEY_HOME", "Hom"), k("KEY_PAGEUP", "PgU"), gap(0.25),
		k("KEY_NUMLOCK", "Num"), k("KEY_KPSLASH", "/"), k("KEY_KPASTERISK", "*"), k("KEY_KPMINUS", "-"),
	}},
	{2.5, []item{
		wide("KEY_TAB", "Tab", 1.5), k("KEY_Q", "Q"), k("KEY_W", "W"), k("KEY_E", "E"), k("KEY_R", "R"), k("KEY_T", "T"),
		k("KEY_Y", "Y"), k("KEY_U", "U"), k("KEY_I", "I"), k("KEY_O", "O"), k("KEY_P", "P"),
		k("KEY_LEFTBRACE", "["), k("KEY_RIGHTBRACE", "]"), wide("KEY_BACKSLASH", "\\", 1.5), gap(0.25),
		k("KEY_DELETE", "Del"), k("KEY_END", "End"), k("KEY_PAGEDOWN", "PgD"), gap(0.25),
		k("KEY_KP7", "7"), k("KEY_KP8", "8"), k("KEY_KP9", "9"), {name: "KEY_KPPLUS", label: "+", w: 1, h: 2},
	}},
	{3.5, []item{
		wide("KEY_CAPSLOCK", "Caps", 1.75), k("KEY_A", "A"), k("KEY_S", "S"), k("KEY_D", "D"), k("KEY_F", "F"), k("KEY_G", "G"),
		k("KEY_H", "H"), k("KEY_J", "J"), k("KEY_K", "K"), k("KEY_L", "L"),
		k("KEY_SEMICOLON", ";"), k("KEY_APOSTROPHE", "'"), wide("KEY_ENTER", "Enter", 2.25), gap(3.5),
		k("KEY_KP4", "4"), k("KEY_KP5", "5"), k("KEY_KP6", "6"),
	}},
	{4.5, []item{
		wide("KEY_LEFTSHIFT", "Shift", 2.25), k("KEY_Z", "Z"), k("KEY_X", "X"), k("KEY_C", "C"), k("KEY_V", "V"), k("KEY_B", "B"),
		k("KEY_N", "N"), k("KEY_M", "M"), k("KEY_COMMA", ","), k("KEY_DOT", "."), k("KEY_SLASH", "/"),
		wide("KEY_RIGHTSHIFT", "Shift", 2.75), gap(1.25),
		k("KEY_UP", "↑"), gap(1.25),
		k("KEY_KP1", "1"), k("KEY_KP2", "2"), k("KEY_KP3", "3"), {name: "KEY_KPENTER", label: "Ent", w: 1, h: 2},
	}},
	{5.5, []item{
		wide("KEY_LEFTCTRL", "Ctrl", 1.25), wide("KEY_LEFTMETA", "Sup", 1.25), wide("KEY_LEFTALT", "Alt", 1.25),
		wide("KEY_SPACE", "", 6.25),
		wide("KEY_RIGHTALT", "Alt", 1.25), wide("KEY_RIGHTMETA", "Sup", 1.25), wide("KEY_COMPOSE", "Menu", 1.25), wide("KEY_RIGHTCTRL", "Ctrl", 1.25), gap(0.25),
		k("KEY_LEFT", "←"), k("KEY_DOWN", "↓"), k("KEY_RIGHT", "→"), gap(0.25),
		wide("KEY_KP0", "0", 2), k("KEY_KPDOT", "."),
	}},
}

// Keyboard returns the keys of the drawn keyboard.
func Keyboard() []KeyCap {
	var keys []KeyCap
	for _, row := range keyboardRows {
		x := float32(0)
		for _, it := range row.items {
			if it.name != "" {
				h := it.h
				if h == 0 {
					h = 1
				}
				keys = append(keys, KeyCap{Name: it.name, Label: it.label, X: x, Y: row.y, W: it.w, H: h})
			}
			x += it.w
		}
	}
	return keys
}
