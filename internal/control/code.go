package control

import (
	"fmt"
	"strconv"
	"strings"
)

// Kind is the class of a control.
type Kind uint8

const (
	// KindKey is a key or button. Names start with KEY_ or BTN_.
	KindKey Kind = iota + 1
	// KindAxis is an absolute axis such as a stick, trigger or hat. Names
	// start with ABS_.
	KindAxis
	// KindRelative is a relative axis such as mouse movement or a scroll
	// wheel. Names start with REL_.
	KindRelative
)

func (k Kind) String() string {
	switch k {
	case KindKey:
		return "key"
	case KindAxis:
		return "axis"
	case KindRelative:
		return "relative"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// prefix returns the name prefix used for codes of this kind that have no
// name of their own.
func (k Kind) prefix() string {
	switch k {
	case KindKey:
		return "KEY_"
	case KindAxis:
		return "ABS_"
	case KindRelative:
		return "REL_"
	}
	return ""
}

func (k Kind) names() map[uint16]string {
	switch k {
	case KindKey:
		return keyNames
	case KindAxis:
		return axisNames
	case KindRelative:
		return relativeNames
	}
	return nil
}

func (k Kind) codes() map[string]uint16 {
	switch k {
	case KindKey:
		return keyCodes
	case KindAxis:
		return axisCodes
	case KindRelative:
		return relativeCodes
	}
	return nil
}

// Code identifies a control on a device. It is comparable and can be used as
// a map key. The zero value is not a valid code.
type Code struct {
	// Kind is the class of the control.
	Kind Kind
	// Num is the Linux input event code within that class.
	Num uint16
}

// Key returns the code of a key or button.
func Key(num uint16) Code { return Code{KindKey, num} }

// Axis returns the code of an absolute axis.
func Axis(num uint16) Code { return Code{KindAxis, num} }

// Relative returns the code of a relative axis.
func Relative(num uint16) Code { return Code{KindRelative, num} }

// IsZero reports whether c is the zero value.
func (c Code) IsZero() bool { return c == Code{} }

// String returns the canonical name of the code, such as "KEY_A". A code the
// Linux headers do not name is written with its number, such as "KEY_0x2f5".
// [ParseCode] accepts both forms.
func (c Code) String() string {
	if name, ok := c.Kind.names()[c.Num]; ok {
		return name
	}
	if prefix := c.Kind.prefix(); prefix != "" {
		return fmt.Sprintf("%s0x%x", prefix, c.Num)
	}
	return fmt.Sprintf("Code(%d, 0x%x)", uint8(c.Kind), c.Num)
}

// ParseCode converts a control name to a [Code].
//
// It accepts every name from the Linux header linux/input-event-codes.h in
// either case, including aliases such as BTN_A for BTN_SOUTH, and the numeric
// form "KEY_0x2f5" for controls that have no name. BTN_ may be used in place
// of KEY_ in the numeric form.
func ParseCode(name string) (Code, error) {
	upper := strings.ToUpper(name)
	var kind Kind
	switch {
	case strings.HasPrefix(upper, "KEY_"), strings.HasPrefix(upper, "BTN_"):
		kind = KindKey
	case strings.HasPrefix(upper, "ABS_"):
		kind = KindAxis
	case strings.HasPrefix(upper, "REL_"):
		kind = KindRelative
	default:
		return Code{}, fmt.Errorf("unknown control %q: names start with KEY_, BTN_, ABS_ or REL_", name)
	}

	if num, ok := kind.codes()[upper]; ok {
		return Code{kind, num}, nil
	}
	if rest := upper[4:]; strings.HasPrefix(rest, "0X") {
		num, err := strconv.ParseUint(rest[2:], 16, 16)
		if err != nil {
			return Code{}, fmt.Errorf("invalid control number in %q", name)
		}
		return Code{kind, uint16(num)}, nil
	}
	return Code{}, fmt.Errorf("unknown control %q", name)
}

// MarshalText implements [encoding.TextMarshaler] using the canonical name.
func (c Code) MarshalText() ([]byte, error) {
	if c.Kind.prefix() == "" {
		return nil, fmt.Errorf("cannot encode invalid control code %v", c)
	}
	return []byte(c.String()), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler] using [ParseCode].
func (c *Code) UnmarshalText(text []byte) error {
	parsed, err := ParseCode(string(text))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
