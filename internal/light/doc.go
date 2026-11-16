// Package light shows an instrument on the lights of its keyboard: which
// keys play which kind of note, what the command keys have done, and the
// notes that sound, played here or sent to HIDI by another program.
//
// The lights are reached through an OpenRGB server the user runs; HIDI
// does not come with one. [Paint] decides the colours and is independent
// of how they get to the hardware.
package light
