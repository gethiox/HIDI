package rawinput

import (
	"slices"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
)

func TestConsumerKeys(t *testing.T) {
	var c consumerKeys
	step := func(usages []uint16, want ...keyChange) {
		t.Helper()
		if got := c.update(usages); !slices.Equal(got, want) {
			t.Errorf("update(%#x) = %v, want %v", usages, got, want)
		}
	}
	// Volume up goes down, play is added, volume up is let go of while a
	// control HIDI has no key for comes and goes, and all is let go of.
	step([]uint16{0xE9}, keyChange{115, true})
	step([]uint16{0xE9}) // the report repeated
	step([]uint16{0xE9, 0xCD}, keyChange{164, true})
	step([]uint16{0xCD, 0x999}, keyChange{115, false})
	step(nil, keyChange{164, false})
	step(nil)
	// The same control listed twice is one key.
	step([]uint16{0xE2, 0xE2}, keyChange{113, true})
}

func TestMediaKeyNames(t *testing.T) {
	// Every code is one HIDI has a name for, and the two tables agree on
	// the keys they share.
	for usage, code := range consumerCodes {
		if name := control.Key(code).String(); name == "" || name[:4] != "KEY_" {
			t.Errorf("usage %#x gives code %d, which has no name", usage, code)
		}
	}
	for vk, code := range mediaKeys {
		if name := control.Key(code).String(); name == "" || name[:4] != "KEY_" {
			t.Errorf("virtual key %#x gives code %d, which has no name", vk, code)
		}
	}
	if mediaKeys[0xAF] != consumerCodes[0xE9] || mediaKeys[0xAD] != consumerCodes[0xE2] || mediaKeys[0xB3] != consumerCodes[0xCD] {
		t.Error("the tables give different keys for volume up, mute or play")
	}
	if !isConsumerControl(0x0C, 0x01) || isConsumerControl(0x01, 0x06) {
		t.Error("the interface of consumer controls is not told apart")
	}
}
