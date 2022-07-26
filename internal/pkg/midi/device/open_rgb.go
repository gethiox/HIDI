package device

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/fs"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/holoplot/go-evdev"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/realbucksavage/openrgb-go"
)

func init() {
	for k, v := range KeyToLedName {
		LedNameToKey[v] = k
	}
}

var LedNameToKey = map[string]evdev.EvCode{} // filled up with init()

var KeyToLedName = map[evdev.EvCode]string{ // hardware button to OpenRGB LED name mapping
	evdev.KEY_ESC:       "Key: Escape",
	evdev.KEY_GRAVE:     "Key: `",
	evdev.KEY_TAB:       "Key: Tab",
	evdev.KEY_CAPSLOCK:  "Key: Caps Lock",
	evdev.KEY_LEFTSHIFT: "Key: Left Shift",
	evdev.KEY_LEFTCTRL:  "Key: Left Control",
	// evdev.KEY_:           "Key: \\ (ISO)", // ?
	evdev.KEY_1:          "Key: 1",
	evdev.KEY_Q:          "Key: Q",
	evdev.KEY_A:          "Key: A",
	evdev.KEY_Z:          "Key: Z",
	evdev.KEY_LEFTMETA:   "Key: Left Windows",
	evdev.KEY_F1:         "Key: F1",
	evdev.KEY_2:          "Key: 2",
	evdev.KEY_W:          "Key: W",
	evdev.KEY_S:          "Key: S",
	evdev.KEY_X:          "Key: X",
	evdev.KEY_LEFTALT:    "Key: Left Alt",
	evdev.KEY_F2:         "Key: F2",
	evdev.KEY_3:          "Key: 3",
	evdev.KEY_E:          "Key: E",
	evdev.KEY_D:          "Key: D",
	evdev.KEY_C:          "Key: C",
	evdev.KEY_F3:         "Key: F3",
	evdev.KEY_4:          "Key: 4",
	evdev.KEY_R:          "Key: R",
	evdev.KEY_F:          "Key: F",
	evdev.KEY_V:          "Key: V",
	evdev.KEY_F4:         "Key: F4",
	evdev.KEY_5:          "Key: 5",
	evdev.KEY_T:          "Key: T",
	evdev.KEY_G:          "Key: G",
	evdev.KEY_B:          "Key: B",
	evdev.KEY_SPACE:      "Key: Space",
	evdev.KEY_F5:         "Key: F5",
	evdev.KEY_6:          "Key: 6",
	evdev.KEY_Y:          "Key: Y",
	evdev.KEY_H:          "Key: H",
	evdev.KEY_N:          "Key: N",
	evdev.KEY_F6:         "Key: F6",
	evdev.KEY_7:          "Key: 7",
	evdev.KEY_U:          "Key: U",
	evdev.KEY_J:          "Key: J",
	evdev.KEY_M:          "Key: M",
	evdev.KEY_F7:         "Key: F7",
	evdev.KEY_8:          "Key: 8",
	evdev.KEY_I:          "Key: I",
	evdev.KEY_K:          "Key: K",
	evdev.KEY_COMMA:      "Key: ,",
	evdev.KEY_RIGHTALT:   "Key: Right Alt",
	evdev.KEY_F8:         "Key: F8",
	evdev.KEY_9:          "Key: 9",
	evdev.KEY_O:          "Key: O",
	evdev.KEY_L:          "Key: L",
	evdev.KEY_DOT:        "Key: .",
	evdev.KEY_COMPOSE:    "Key: Menu",
	evdev.KEY_F9:         "Key: F9",
	evdev.KEY_0:          "Key: 0",
	evdev.KEY_P:          "Key: P",
	evdev.KEY_SEMICOLON:  "Key: ;",
	evdev.KEY_SLASH:      "Key: /",
	evdev.KEY_RIGHTMETA:  "Key: Right Windows",
	evdev.KEY_F10:        "Key: F10",
	evdev.KEY_MINUS:      "Key: -",
	evdev.KEY_LEFTBRACE:  "Key: [",
	evdev.KEY_APOSTROPHE: "Key: '",
	evdev.KEY_F11:        "Key: F11",
	evdev.KEY_EQUAL:      "Key: =",
	evdev.KEY_RIGHTBRACE: "Key: ]",
	// evdev.KEY_:             "Key: #", // ?
	evdev.KEY_F12:          "Key: F12",
	evdev.KEY_BACKSPACE:    "Key: Backspace",
	evdev.KEY_BACKSLASH:    "Key: \\ (ANSI)",
	evdev.KEY_ENTER:        "Key: Enter",
	evdev.KEY_RIGHTSHIFT:   "Key: Right Shift",
	evdev.KEY_RIGHTCTRL:    "Key: Right Control",
	evdev.KEY_SYSRQ:        "Key: Print Screen",
	evdev.KEY_INSERT:       "Key: Insert",
	evdev.KEY_DELETE:       "Key: Delete",
	evdev.KEY_LEFT:         "Key: Left Arrow",
	evdev.KEY_SCROLLLOCK:   "Key: Scroll Lock",
	evdev.KEY_HOME:         "Key: Home",
	evdev.KEY_END:          "Key: End",
	evdev.KEY_UP:           "Key: Up Arrow",
	evdev.KEY_DOWN:         "Key: Down Arrow",
	evdev.KEY_PAUSE:        "Key: Pause/Break",
	evdev.KEY_PAGEUP:       "Key: Page Up",
	evdev.KEY_PAGEDOWN:     "Key: Page Down",
	evdev.KEY_RIGHT:        "Key: Right Arrow",
	evdev.KEY_NUMLOCK:      "Key: Num Lock",
	evdev.KEY_KP7:          "Key: Number Pad 7",
	evdev.KEY_KP4:          "Key: Number Pad 4",
	evdev.KEY_KP1:          "Key: Number Pad 1",
	evdev.KEY_KP0:          "Key: Number Pad 0",
	evdev.KEY_KPSLASH:      "Key: Number Pad /",
	evdev.KEY_KP8:          "Key: Number Pad 8",
	evdev.KEY_KP5:          "Key: Number Pad 5",
	evdev.KEY_KP2:          "Key: Number Pad 2",
	evdev.KEY_MUTE:         "Key: Media Mute",
	evdev.KEY_KPASTERISK:   "Key: Number Pad *",
	evdev.KEY_KP9:          "Key: Number Pad 9",
	evdev.KEY_KP6:          "Key: Number Pad 6",
	evdev.KEY_KP3:          "Key: Number Pad 3",
	evdev.KEY_KPDOT:        "Key: Number Pad .",
	evdev.KEY_PREVIOUSSONG: "Key: Media Previous",
	evdev.KEY_KPMINUS:      "Key: Number Pad -",
	evdev.KEY_KPPLUS:       "Key: Number Pad +",
	evdev.KEY_PLAYPAUSE:    "Key: Media Play/Pause",
	evdev.KEY_NEXTSONG:     "Key: Media Next",
	evdev.KEY_KPENTER:      "Key: Number Pad Enter",
}

// resolveHidraw returns event name that relates to given hidraw device
// "/dev/hidraw0" > "event0"
func resolveHidraw(dev string) (string, error) {
	regex1 := regexp.MustCompile(`/dev/(hidraw\d+)`)
	out := regex1.FindStringSubmatch(dev)
	if len(out) != 2 {
		return "", fmt.Errorf("unexpected dev format: %s", dev)
	}

	path := fmt.Sprintf("/sys/class/hidraw/%s/device/input", out[1])

	rootEntry := fs.NewEntry(path)
	dirs, err := rootEntry.Dirs()
	if err != nil {
		return "", fmt.Errorf("failed to list root entry: %s", err)
	}

	if len(dirs) != 1 {
		return "", fmt.Errorf("unexpected dir length")
	}

	var entry fs.Entry
	for _, entry = range dirs {
		break
	}

	dirs, err = entry.Dirs()
	if err != nil {
		return "", fmt.Errorf("failed to list \"%s\": %s", entry.Path(), err)
	}

	regex2 := regexp.MustCompile(`event\d+`)

	for name := range dirs {
		if regex2.MatchString(name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("event not found")
}

func findController(c *openrgb.Client, events map[string]bool) (openrgb.Device, int, error) {
	count, err := c.GetControllerCount()
	if err != nil {
		return openrgb.Device{}, 0, fmt.Errorf("failed to get controller count: %s", err)
	}

	if count == 0 {
		return openrgb.Device{}, 0, fmt.Errorf("no supported controllers available")
	}

	regex := regexp.MustCompile(`.*(/dev/hidraw\d+)`)

	for i := 0; i < count; i++ {
		dev, err := c.GetDeviceController(i)
		if err != nil {
			return openrgb.Device{}, 0, fmt.Errorf("getting controller information failed (%d/%d): %s", i, count, err)
		}

		if dev.Type != 5 { // keyboard
			continue
		}

		out := regex.FindStringSubmatch(dev.Location)
		if len(out) != 2 {
			continue
		}

		event, err := resolveHidraw(out[1])
		if err != nil {
			return openrgb.Device{}, 0, fmt.Errorf("resolve hidraw failed (%d/%d): %s", i, count, err)
		}

		if events[event] == true {
			return dev, i, nil
		}
	}

	return openrgb.Device{}, 0, fmt.Errorf("controller not found")
}

func (d *Device) handleOpenrgb(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()

	host, port := "localhost", d.openrgbPort

	log.Info(fmt.Sprintf("[OpenRGB] Connecting: %s:%d...", host, port), d.logFields(logger.Debug)...)

	var c *openrgb.Client
	var err error

	timeout := time.Now().Add(time.Second * 5)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond * 250):
			break
		}

		if time.Now().After(timeout) {
			log.Info("[OpenRGB] Connecting to server: Giving up", d.logFields(logger.Debug)...)
			break
		}

		c, err = openrgb.Connect(host, port)
		if err != nil {
			continue
		}
		break
	}

	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Cannot connect to server: %s", err), d.logFields(logger.Debug)...)
		return
	}

	log.Info(fmt.Sprintf("[OpenRGB] Connected, finding controller..."), d.logFields(logger.Debug)...)

	var dev openrgb.Device
	var index int

	var events = make(map[string]bool)
	for _, di := range d.InputDevice.Handlers {
		events[di.Event()] = true
	}

	timeout = time.Now().Add(time.Second * 2)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond * 250):
			break
		}

		if time.Now().After(timeout) {
			log.Info("[OpenRGB] find controller: Giving up", d.logFields(logger.Debug)...)
			break
		}

		dev, index, err = findController(c, events)
		if err != nil {
			continue
		}
		break
	}

	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Cannot find controller: %s", err), d.logFields(logger.Debug)...)
		return
	}

	log.Info(fmt.Sprintf("[OpenRGB] Controller found: %s, index: %d", dev.Name, index), d.logFields(logger.Debug)...)

	var ledArray = make([]openrgb.Color, 0)

	for range dev.Colors {
		ledArray = append(ledArray, openrgb.Color{})
	}

	ledSequence := dev.LEDs

	var indexMap = make(map[evdev.EvCode]int)

	for i, led := range ledSequence {
		key, ok := LedNameToKey[led.Name]
		if !ok {
			continue
		}
		indexMap[key] = i
	}

	var nameToIndex = make(map[string]int)

	for i, led := range ledSequence {
		nameToIndex[led.Name] = i
	}

	var MidiKeyMappings = make([]map[byte][]evdev.EvCode, 0)

	for _, m := range d.config.KeyMappings {
		var midiKeyMapping = make(map[byte][]evdev.EvCode)
		for code, note := range m.Midi {
			_, ok := midiKeyMapping[note]
			if !ok {
				midiKeyMapping[note] = []evdev.EvCode{code}
			} else {
				midiKeyMapping[note] = append(midiKeyMapping[note], code)
			}
		}
		MidiKeyMappings = append(MidiKeyMappings, midiKeyMapping)
	}

	var actionToEvcode = make(map[config.Action]evdev.EvCode)

	for code, action := range d.config.ActionMapping {
		actionToEvcode[action] = code
	}

	white1 := openrgb.Color{Red: 27, Green: 27, Blue: 27}
	white2 := openrgb.Color{Red: 100, Green: 100, Blue: 100}
	white3 := openrgb.Color{Red: 255, Green: 255, Blue: 255}

	var channelColors = make(map[byte]openrgb.Color)

	for ch := 0; ch < 16; ch++ {
		var h = 720/16*float64(ch) + 30
		if h >= 360 {
			h -= 360
		}
		c := colorful.Hsv(h, 1, 1)
		channelColors[byte(ch)] = openrgb.Color{
			Red:   byte(c.R * 255),
			Green: byte(c.G * 255),
			Blue:  byte(c.B * 255),
		}
	}

	log.Info(fmt.Sprintf("[OpenRGB] LED update loop started"), d.logFields(logger.Debug)...)

	nextFailedLedUpdateReport := time.Now()
	updateFails := 0
root:
	for {
		select {
		case <-ctx.Done():
			break root
		default:
			break
		}
		time.Sleep(time.Millisecond * 10)

		d.eventProcessMutex.Lock()
		offset := int(d.semitone) + int(d.octave)*12

		for code := range indexMap {
			ledArray[indexMap[code]] = d.config.OpenRGB.Colors.Unavailable
		}

		ledArray[indexMap[actionToEvcode[config.Panic]]] = openrgb.Color{Red: 0xff}

		ledArray[indexMap[actionToEvcode[config.OctaveUp]]] = white1
		ledArray[indexMap[actionToEvcode[config.OctaveDown]]] = white1

		if d.octave > 0 {
			if d.octave == 1 {
				ledArray[indexMap[actionToEvcode[config.OctaveUp]]] = white2
			} else {
				ledArray[indexMap[actionToEvcode[config.OctaveUp]]] = white3
			}
		}
		if d.octave < 0 {
			if d.octave == -1 {
				ledArray[indexMap[actionToEvcode[config.OctaveDown]]] = white2
			} else {
				ledArray[indexMap[actionToEvcode[config.OctaveDown]]] = white3
			}
		}

		ledArray[indexMap[actionToEvcode[config.SemitoneUp]]] = white1
		ledArray[indexMap[actionToEvcode[config.SemitoneDown]]] = white1
		if d.semitone > 0 {
			if d.semitone == 1 {
				ledArray[indexMap[actionToEvcode[config.SemitoneUp]]] = white2
			} else {
				ledArray[indexMap[actionToEvcode[config.SemitoneUp]]] = white3
			}
		}
		if d.semitone < 0 {
			if d.semitone == -1 {
				ledArray[indexMap[actionToEvcode[config.SemitoneDown]]] = white2
			} else {
				ledArray[indexMap[actionToEvcode[config.SemitoneDown]]] = white3
			}
		}

		ledArray[indexMap[actionToEvcode[config.MappingUp]]] = white3
		ledArray[indexMap[actionToEvcode[config.MappingDown]]] = white3
		if d.mapping == 0 {
			ledArray[indexMap[actionToEvcode[config.MappingDown]]] = white1
		}
		if d.mapping == len(d.config.KeyMappings)-1 {
			ledArray[indexMap[actionToEvcode[config.MappingUp]]] = white1
		}

		chanColor := channelColors[d.channel]
		ledArray[indexMap[actionToEvcode[config.ChannelUp]]] = chanColor
		ledArray[indexMap[actionToEvcode[config.ChannelDown]]] = chanColor
		if d.channel == 0 {
			ledArray[indexMap[actionToEvcode[config.ChannelDown]]] = openrgb.Color{
				Red:   chanColor.Red / 3,
				Green: chanColor.Green / 3,
				Blue:  chanColor.Blue / 3,
			}
		}
		if d.channel == 15 {
			ledArray[indexMap[actionToEvcode[config.ChannelUp]]] = openrgb.Color{
				Red:   chanColor.Red / 3,
				Green: chanColor.Green / 3,
				Blue:  chanColor.Blue / 3,
			}
		}

		ledArray[indexMap[actionToEvcode[config.Multinote]]] = white1

		for code, note := range d.config.KeyMappings[d.mapping].Midi {
			x := int(note) + offset
			if x < 0 || x > 127 {
				continue
			}

			var color openrgb.Color

			if d.config.KeyMappings[d.mapping].Name == "Control" {
				color = d.config.OpenRGB.Colors.White
			} else {
				switch x % 12 {
				case 0: // c
					color = d.config.OpenRGB.Colors.C
				case 1, 3, 6, 8, 10: // black keys
					color = d.config.OpenRGB.Colors.Black
				default: // white keys
					color = d.config.OpenRGB.Colors.White
				}
			}

			id, ok := indexMap[code]
			if !ok {
				continue
			}
			ledArray[id] = color
		}

		d.externalTrackerMutex.Lock()
		for ch := 15; ch >= 0; ch-- {
			for note := range d.externalNoteTracker[byte(ch)] {
				note = note - byte(offset)
				for _, code := range MidiKeyMappings[d.mapping][note] {
					id, ok := indexMap[code]
					if !ok {
						continue
					}
					ledArray[id] = channelColors[byte(ch)]
				}
			}
		}

		for note := range d.externalNoteTracker[d.channel] {
			note = note - byte(offset)
			for _, code := range MidiKeyMappings[d.mapping][note] {
				// duplicated code
				id, ok := indexMap[code]
				if !ok {
					continue
				}
				ledArray[id] = d.config.OpenRGB.Colors.ActiveExternal
			}
		}
		d.externalTrackerMutex.Unlock()

		for _, noteAndChannel := range d.noteTracker {
			note := noteAndChannel[0] - byte(offset)

			for _, code := range MidiKeyMappings[d.mapping][note] {
				id, ok := indexMap[code]
				if !ok {
					continue
				}
				ledArray[id] = d.config.OpenRGB.Colors.Active
			}
		}

		err = c.UpdateLEDs(index, ledArray)
		if err != nil {
			updateFails++
			now := time.Now()
			if now.After(nextFailedLedUpdateReport) {
				log.Info(fmt.Sprintf("[OpenRGB] Led update fails %d times, last err: %s", updateFails, err), d.logFields(logger.Debug)...)
				updateFails = 0
				nextFailedLedUpdateReport = now.Add(time.Second * 2)
			}
		}
		d.eventProcessMutex.Unlock()
	}

	for i, _ := range ledArray {
		ledArray[i] = openrgb.Color{Red: 0xff}
	}
	c.UpdateLEDs(index, ledArray)
}
