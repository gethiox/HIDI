package input

const (

	// Protocol version.

	EV_VERSION = 0x010001

	// IDs.

	ID_BUS     = 0
	ID_VENDOR  = 1
	ID_PRODUCT = 2
	ID_VERSION = 3

	BUS_PCI       = 0x01
	BUS_ISAPNP    = 0x02
	BUS_USB       = 0x03
	BUS_HIL       = 0x04
	BUS_BLUETOOTH = 0x05
	BUS_VIRTUAL   = 0x06

	BUS_ISA         = 0x10
	BUS_I8042       = 0x11
	BUS_XTKBD       = 0x12
	BUS_RS232       = 0x13
	BUS_GAMEPORT    = 0x14
	BUS_PARPORT     = 0x15
	BUS_AMIGA       = 0x16
	BUS_ADB         = 0x17
	BUS_I2C         = 0x18
	BUS_HOST        = 0x19
	BUS_GSC         = 0x1A
	BUS_ATARI       = 0x1B
	BUS_SPI         = 0x1C
	BUS_RMI         = 0x1D
	BUS_CEC         = 0x1E
	BUS_INTEL_ISHTP = 0x1F

	// MT_TOOL types

	MT_TOOL_FINGER = 0x00
	MT_TOOL_PEN    = 0x01
	MT_TOOL_PALM   = 0x02
	MT_TOOL_DIAL   = 0x0a
	MT_TOOL_MAX    = 0x0f

	// Values describing the status of a force-feedback effect

	FF_STATUS_STOPPED = 0x00
	FF_STATUS_PLAYING = 0x01
	FF_STATUS_MAX     = 0x01

	// Force feedback effect types

	FF_RUMBLE   = 0x50
	FF_PERIODIC = 0x51
	FF_CONSTANT = 0x52
	FF_SPRING   = 0x53
	FF_FRICTION = 0x54
	FF_DAMPER   = 0x55
	FF_INERTIA  = 0x56
	FF_RAMP     = 0x57

	FF_EFFECT_MIN = FF_RUMBLE
	FF_EFFECT_MAX = FF_RAMP

	// Force feedback periodic effect types

	FF_SQUARE   = 0x58
	FF_TRIANGLE = 0x59
	FF_SINE     = 0x5a
	FF_SAW_UP   = 0x5b
	FF_SAW_DOWN = 0x5c
	FF_CUSTOM   = 0x5d

	FF_WAVEFORM_MIN = FF_SQUARE
	FF_WAVEFORM_MAX = FF_CUSTOM

	// Set ff device properties

	FF_GAIN       = 0x60
	FF_AUTOCENTER = 0x61

	// ff->playback(effect_id = FF_GAIN) is the first effect_id to
	// cause a collision with another ff method, in this case ff->set_gain().
	// Therefore the greatest safe value for effect_id is FF_GAIN - 1,
	// and thus the total number of effects should never exceed FF_GAIN.

	FF_MAX_EFFECTS = FF_GAIN

	FF_MAX = 0x7f
	FF_CNT = (FF_MAX + 1)
)

// AbsInfo - used by EVIOCGABS/EVIOCSABS ioctls
// @value: latest reported value for the axis.
// @minimum: specifies minimum value for the axis.
// @maximum: specifies maximum value for the axis.
// @fuzz: specifies fuzz value that is used to filter noise from
//	the event stream.
// @flat: values that are within this value will be discarded by
//	joydev interface and reported as 0 instead.
// @resolution: specifies resolution for the values reported for
//	the axis.
//
// Note that input core does not clamp reported values to the
// [minimum, maximum] limits, such task is left to userspace.
//
// The default resolution for main axes (ABS_X, ABS_Y, ABS_Z)
// is reported in units per millimeter (units/mm), resolution
// for rotational axes (ABS_RX, ABS_RY, ABS_RZ) is reported
// in units per radian.
// When INPUT_PROP_ACCELEROMETER is set the resolution changes.
// The main axes (ABS_X, ABS_Y, ABS_Z) are then reported in
// units per g (units/g) and in units per degree per second
// (units/deg/s) for rotational axes (ABS_RX, ABS_RY, ABS_RZ).
type AbsInfo struct {
	value      int32
	minimum    int32
	maximum    int32
	fuzz       int32
	flat       int32
	resolution int32
}
