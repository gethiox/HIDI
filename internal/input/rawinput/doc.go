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
// Many keyboards have their media keys, volume and play and the like, on
// an interface of their own, as "consumer controls". Windows acts on them
// by itself and reports nothing of them under keyboards. HIDI reads that
// interface as it reads game controllers, and makes keys of it for the
// keyboard it belongs to; consumer.go has the keys. While a keyboard is
// HIDI's alone, the hook keeps the keys Windows makes of them from other
// programs, as it does every other key.
//
// Game controllers are found in the list Raw Input keeps of all input
// devices, and read from their devices directly, as the HID reports the
// controller sends: not through Raw Input. What Windows delivers through
// Raw Input it also counts as somebody using the machine, and a stick
// that never quite rests reports all the time, so a HIDI that had asked
// for controllers there kept the screen from ever turning off. Windows
// cannot keep controllers from other programs. pad.go
// has the translation of their buttons and axes. Xbox controllers are
// better read through XInput, which package xinput does: as HID devices
// they report their two triggers as one axis.
//
// The files without a build constraint hold the parts that do not call
// Windows, so that their tests run on every system.
package rawinput
