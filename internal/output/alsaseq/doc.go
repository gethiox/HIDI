//go:build linux

// Package alsaseq implements MIDI output through the ALSA sequencer, the
// MIDI routing layer of the Linux kernel. PipeWire and JACK expose sequencer
// ports to their own clients, so an endpoint created here is visible to
// programs using any of the three.
//
// The package talks to the kernel directly through /dev/snd/seq instead of
// linking the ALSA C library, which keeps HIDI free of cgo. The structures
// and constants mirror the kernel header sound/asequencer.h; their layout is
// part of the kernel's stable interface and is checked by the tests.
package alsaseq
