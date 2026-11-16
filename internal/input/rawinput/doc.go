// Package rawinput reads keyboards and game controllers on Windows.
//
// Windows has no single interface that both tells keyboards apart and can
// keep their input from other programs, so the package uses two:
//
//   - Raw Input reports every key with the keyboard it came from. HIDI
//     reads the keys from it.
//   - A low-level keyboard hook can swallow keys before other programs
//     see them, but does not know which keyboard a key came from. While a
//     keyboard is grabbed, the hook swallows the keys of every keyboard.
//
// Keys are reported by their position, as scan codes, and translated to
// the Linux key codes HIDI uses everywhere; scancode.go has the table.
//
// Game controllers come through Raw Input as well, as the HID reports the
// controller sends. Windows cannot keep them from other programs. pad.go
// has the translation of their buttons and axes.
//
// The files without a build constraint hold the parts that do not call
// Windows, so that their tests run on every system.
package rawinput
