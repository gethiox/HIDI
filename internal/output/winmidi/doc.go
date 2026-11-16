// Package winmidi creates virtual MIDI devices on Windows through Windows
// MIDI Services, the MIDI stack of Windows 11 25H2 and later.
//
// The API is a WinRT one, Windows.Devices.Midi2. This package calls it
// without cgo: it activates the classes itself and calls their methods
// through the interface tables, the way a C program would. The identifiers
// and the order of the methods come from the interface definitions in
// Microsoft's source; docs/research/windows-midi-abi.md records them and
// where each was found.
//
// A virtual device belongs to the program that creates it and disappears
// when the program closes it or ends. Other programs see it as a MIDI
// device, also through the older Windows MIDI APIs.
package winmidi
