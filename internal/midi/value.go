package midi

import "math"

// Full-range constants for the 32-bit values a [Message] carries.
const (
	// ValueMax is the largest value.
	ValueMax uint32 = math.MaxUint32
	// ValueCenter is the midpoint, used as "no bend" by pitch bend.
	ValueCenter uint32 = 1 << 31
)

// Unit converts a normalized value to the full 32-bit range. Input outside
// 0-1 is clamped, and NaN becomes 0.
func Unit(f float64) uint32 {
	switch {
	case !(f > 0): // also catches NaN
		return 0
	case f >= 1:
		return ValueMax
	}
	return uint32(math.Round(f * float64(ValueMax)))
}

// Bipolar converts a value in the range -1 to 1 to the full 32-bit range, so
// that 0 maps exactly to [ValueCenter]. Input outside the range is clamped,
// and NaN becomes the center.
func Bipolar(f float64) uint32 {
	switch {
	case f != f:
		return ValueCenter
	case f <= -1:
		return 0
	case f >= 1:
		return ValueMax
	case f < 0:
		return ValueCenter - uint32(math.Round(-f*float64(ValueCenter)))
	}
	return ValueCenter + uint32(math.Round(f*float64(ValueMax-ValueCenter)))
}

// From7 expands a 7-bit MIDI 1.0 value to the full range. Bits above the
// seventh are ignored.
func From7(v uint8) uint32 { return scaleUp(uint32(v&0x7F), 7) }

// From14 expands a 14-bit MIDI 1.0 value to the full range. Bits above the
// fourteenth are ignored.
func From14(v uint16) uint32 { return scaleUp(uint32(v&0x3FFF), 14) }

// To7 reduces a full-range value to 7 bits.
func To7(v uint32) uint8 { return uint8(v >> 25) }

// To14 reduces a full-range value to 14 bits.
func To14(v uint32) uint16 { return uint16(v >> 18) }

// scaleUp expands a value of srcBits bits to 32 bits using the min-center-max
// algorithm from the MIDI 2.0 specification: zero, the center and the maximum
// of the source range map to zero, the center and the maximum of the result,
// and values above the center are filled by repeating their low bits rather
// than with zeros.
func scaleUp(v uint32, srcBits uint) uint32 {
	const dstBits = 32
	scaleBits := dstBits - srcBits
	shifted := v << scaleBits
	if v <= 1<<(srcBits-1) {
		return shifted
	}

	repeatBits := srcBits - 1
	repeat := v & (1<<repeatBits - 1)
	if scaleBits > repeatBits {
		repeat <<= scaleBits - repeatBits
	} else {
		repeat >>= repeatBits - scaleBits
	}
	for repeat != 0 {
		shifted |= repeat
		repeat >>= repeatBits
	}
	return shifted
}
