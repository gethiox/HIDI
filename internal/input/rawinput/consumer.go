package rawinput

import "slices"

// Many keyboards do not send their media keys as keys. Volume, mute,
// play and their like are "consumer controls" in HID, and sit on an
// interface of their own beside the one for the keys. Windows turns what
// that interface reports into the volume going up and the like by
// itself, and Raw Input says nothing of it under keyboards. HIDI reads
// the interface, for a keyboard that has one, and makes keys of it.

// The usage page of consumer controls, and the usage of the collection
// that holds them.
const (
	pageConsumer         = 0x0C
	usageConsumerControl = 0x01
)

// isConsumerControl reports whether an interface is one of consumer
// controls.
func isConsumerControl(usagePage, usage uint16) bool {
	return usagePage == pageConsumer && usage == usageConsumerControl
}

// consumerCodes gives the Linux key code for the consumer controls
// keyboards have, by their HID usage. The codes are those the Linux
// kernel gives the same usages.
var consumerCodes = map[uint16]uint16{
	0x0B5: 163, // Scan Next Track: KEY_NEXTSONG
	0x0B6: 165, // Scan Previous Track: KEY_PREVIOUSSONG
	0x0B7: 166, // Stop: KEY_STOPCD
	0x0CD: 164, // Play/Pause: KEY_PLAYPAUSE
	0x0E2: 113, // Mute: KEY_MUTE
	0x0E9: 115, // Volume Increment: KEY_VOLUMEUP
	0x0EA: 114, // Volume Decrement: KEY_VOLUMEDOWN
	0x183: 171, // Consumer Control Configuration: KEY_CONFIG
	0x18A: 155, // Email Reader: KEY_MAIL
	0x192: 140, // Calculator: KEY_CALC
	0x194: 144, // Local Machine Browser: KEY_FILE
	0x221: 217, // Search: KEY_SEARCH
	0x223: 172, // Home: KEY_HOMEPAGE
	0x224: 158, // Back: KEY_BACK
	0x225: 159, // Forward: KEY_FORWARD
	0x226: 128, // Stop: KEY_STOP
	0x227: 173, // Refresh: KEY_REFRESH
	0x22A: 156, // Bookmarks: KEY_BOOKMARKS
}

// mediaKeys gives the Linux key code for the virtual key codes Windows
// has for the same keys, for the keyboard hook: Windows names such a key
// by its virtual key code alone where it made the key of a consumer
// control.
var mediaKeys = map[uint32]uint16{
	0xA6: 158, // VK_BROWSER_BACK
	0xA7: 159, // VK_BROWSER_FORWARD
	0xA8: 173, // VK_BROWSER_REFRESH
	0xA9: 128, // VK_BROWSER_STOP
	0xAA: 217, // VK_BROWSER_SEARCH
	0xAB: 156, // VK_BROWSER_FAVORITES
	0xAC: 172, // VK_BROWSER_HOME
	0xAD: 113, // VK_VOLUME_MUTE
	0xAE: 114, // VK_VOLUME_DOWN
	0xAF: 115, // VK_VOLUME_UP
	0xB0: 163, // VK_MEDIA_NEXT_TRACK
	0xB1: 165, // VK_MEDIA_PREV_TRACK
	0xB2: 166, // VK_MEDIA_STOP
	0xB3: 164, // VK_MEDIA_PLAY_PAUSE
	0xB4: 155, // VK_LAUNCH_MAIL
	0xB5: 171, // VK_LAUNCH_MEDIA_SELECT
	0xB6: 144, // VK_LAUNCH_APP1
	0xB7: 140, // VK_LAUNCH_APP2
}

// keyChange is a key that went down or up.
type keyChange struct {
	code    uint16
	pressed bool
}

// consumerKeys follows the consumer controls of one interface. A report
// lists the controls that are on at the moment; what changed since the
// report before is what was pressed and let go of.
type consumerKeys struct {
	down []uint16 // the key codes that are down, in order
}

// update takes the usages a report lists and returns the keys that
// changed: releases first, then presses, each in the order of their
// codes. Usages HIDI has no key for are left out.
func (c *consumerKeys) update(usages []uint16) []keyChange {
	var now []uint16
	for _, usage := range usages {
		if code, ok := consumerCodes[usage]; ok && !slices.Contains(now, code) {
			now = append(now, code)
		}
	}
	slices.Sort(now)
	var changes []keyChange
	for _, code := range c.down {
		if !slices.Contains(now, code) {
			changes = append(changes, keyChange{code, false})
		}
	}
	for _, code := range now {
		if !slices.Contains(c.down, code) {
			changes = append(changes, keyChange{code, true})
		}
	}
	c.down = now
	return changes
}
