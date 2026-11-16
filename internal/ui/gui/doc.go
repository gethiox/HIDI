// Package gui is HIDI's window: the instruments and their state, a
// keyboard that shows what is played, and the log.
//
// The window is a client of the running HIDI over the control socket, like
// the terminal views, so closing it stops nothing. It is drawn with raylib,
// which is loaded as a shared library without cgo; that works on 64-bit
// Linux and Windows only, so on other systems [Run] reports that there is
// no window.
//
// The files without a build constraint hold what does not draw: the state
// the window shows, the log with its filter, the geometry of the keyboard
// and the colours. Their tests run everywhere.
package gui
