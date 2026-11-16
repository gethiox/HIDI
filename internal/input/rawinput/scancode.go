package rawinput

// A keyboard reports a key as a scan code of "set 1", the codes of the
// original PC keyboard, sometimes with a prefix. Linux numbered its keys
// after the same set, so a plain scan code up to 0x58 is the Linux key
// code. Keys added later carry the prefix E0 and have Linux codes of
// their own, listed here. The values are from the kernel's
// drivers/input/keyboard/atkbd.c and linux/input-event-codes.h.

// maxPlain is the highest scan code without a prefix that maps to itself.
const maxPlain = 0x58 // F12

// extended maps scan codes with the E0 prefix to Linux key codes.
var extended = map[uint16]uint16{
	0x10: 165, // KEY_PREVIOUSSONG
	0x19: 163, // KEY_NEXTSONG
	0x1C: 96,  // KEY_KPENTER
	0x1D: 97,  // KEY_RIGHTCTRL
	0x20: 113, // KEY_MUTE
	0x21: 140, // KEY_CALC
	0x22: 164, // KEY_PLAYPAUSE
	0x24: 166, // KEY_STOPCD
	0x2E: 114, // KEY_VOLUMEDOWN
	0x30: 115, // KEY_VOLUMEUP
	0x32: 172, // KEY_HOMEPAGE
	0x35: 98,  // KEY_KPSLASH
	0x37: 99,  // KEY_SYSRQ (Print Screen)
	0x38: 100, // KEY_RIGHTALT
	0x46: 119, // KEY_PAUSE (Ctrl+Pause, "Break")
	0x47: 102, // KEY_HOME
	0x48: 103, // KEY_UP
	0x49: 104, // KEY_PAGEUP
	0x4B: 105, // KEY_LEFT
	0x4D: 106, // KEY_RIGHT
	0x4F: 107, // KEY_END
	0x50: 108, // KEY_DOWN
	0x51: 109, // KEY_PAGEDOWN
	0x52: 110, // KEY_INSERT
	0x53: 111, // KEY_DELETE
	0x5B: 125, // KEY_LEFTMETA
	0x5C: 126, // KEY_RIGHTMETA
	0x5D: 127, // KEY_COMPOSE (the menu key)
	0x5E: 116, // KEY_POWER
	0x5F: 142, // KEY_SLEEP
	0x63: 143, // KEY_WAKEUP
	0x65: 217, // KEY_SEARCH
	0x66: 156, // KEY_BOOKMARKS
	0x67: 173, // KEY_REFRESH
	0x68: 128, // KEY_STOP
	0x69: 159, // KEY_FORWARD
	0x6A: 158, // KEY_BACK
	0x6B: 157, // KEY_COMPUTER
	0x6C: 155, // KEY_MAIL
	0x6D: 226, // KEY_MEDIA
}

// plainExtra maps the scan codes without a prefix above F12 that keyboards
// use: the keys of international layouts and F13 to F24.
var plainExtra = map[uint16]uint16{
	0x59: 117, // KEY_KPEQUAL
	0x64: 183, // KEY_F13
	0x65: 184, // KEY_F14
	0x66: 185, // KEY_F15
	0x67: 186, // KEY_F16
	0x68: 187, // KEY_F17
	0x69: 188, // KEY_F18
	0x6A: 189, // KEY_F19
	0x6B: 190, // KEY_F20
	0x6C: 191, // KEY_F21
	0x6D: 192, // KEY_F22
	0x6E: 193, // KEY_F23
	0x76: 194, // KEY_F24
	0x70: 93,  // KEY_KATAKANAHIRAGANA
	0x73: 89,  // KEY_RO
	0x79: 92,  // KEY_HENKAN
	0x7B: 94,  // KEY_MUHENKAN
	0x7D: 124, // KEY_YEN
	0x7E: 121, // KEY_KPCOMMA
}

// Flags of a raw keyboard event, from RAWKEYBOARD.
const (
	flagBreak = 0x01 // the key was released
	flagE0    = 0x02 // the scan code has the E0 prefix
	flagE1    = 0x04 // the scan code has the E1 prefix
)

// overrun is the code a keyboard sends when it lost track of its keys.
const overrun = 0xFF

// keyCode translates a raw keyboard event to a Linux key code. It reports
// false for events that are not keys: the shift presses keyboards make up
// around some keys, and codes the table does not know.
func keyCode(makeCode, flags uint16) (code uint16, ok bool) {
	switch {
	case makeCode == 0 || makeCode == overrun:
		return 0, false
	case flags&flagE1 != 0:
		// Pause is the only key with this prefix: E1 1D, then 45.
		if makeCode == 0x1D {
			return 119, true // KEY_PAUSE
		}
		return 0, false
	case flags&flagE0 != 0:
		// E0 2A and E0 36 are shift presses the keyboard inserts around
		// the navigation keys and Print Screen; they are no keys.
		if makeCode == 0x2A || makeCode == 0x36 {
			return 0, false
		}
		code, ok = extended[makeCode]
		return code, ok
	case makeCode <= maxPlain:
		// Num Lock arrives as 45, and so does the second half of Pause,
		// which was reported by its first half above.
		return makeCode, true
	}
	code, ok = plainExtra[makeCode]
	return code, ok
}
