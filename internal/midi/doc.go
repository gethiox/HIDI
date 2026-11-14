// Package midi defines the MIDI messages HIDI generates and their wire
// encodings. It performs no I/O.
//
// # Resolution
//
// A [Message] stores every value (velocity, controller value, pressure, pitch
// bend) as an unsigned 32-bit number that spans the full range of the
// parameter. Encoders scale it down to what the wire format carries: 7 or 14
// bits for MIDI 1.0. Keeping the full resolution in memory means the engine
// does not need to know which protocol an output speaks.
//
// Use [Unit] and [Bipolar] to build values from normalized input, and
// [From7] and [From14] to build them from MIDI 1.0 data.
//
// # Encodings
//
// [Message.AppendBytes] produces the MIDI 1.0 byte stream format, and
// [Message.UMP] produces a MIDI 1.0 channel voice message in a Universal MIDI
// Packet, which is what packet-based APIs such as Windows MIDI Services
// expect. [DecodeBytes] and [DecodeUMP] are their inverses.
package midi
