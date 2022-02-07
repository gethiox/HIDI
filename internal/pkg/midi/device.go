package midi

import (
	"fmt"
	"math"
	"sort"

	"pi-midi-keyboard/internal/pkg/input"

	"github.com/holoplot/go-evdev"
)

const (
	EV_KEY_RELEASE = 0
	EV_KEY_PRESS   = 1
	EV_KEY_REPEAT  = 2
)

type Device struct {
	config      Config
	inputDevice input.Device
	inputEvents <-chan *evdev.InputEvent
	midiEvents  chan<- Event
	logs        chan<- string

	// TODO: move this to input.Device
	// absinfos holds information about boundaries of analog information
	absinfos map[evdev.EvCode]evdev.AbsInfo

	// instead of generating NoteOff events based on the current Device state (lazy approach), every emitted note
	// is being tracked and released precisely on related hardware button release.
	// This approach gives much nicer user experience as the User may conveniently hold some keys
	// and modify state on the fly (changing octave, channel etc.), NoteOff events will be emitted correctly anyway.
	noteTracker   map[evdev.EvCode][2]byte // 1: note, 2: channel
	actionTracker map[Action]bool
	octave        int8
	semitone      int8
	channel       uint8
	// warning: currently lazy implementation
	multiNote []int // list of additional note intervals (offsets)
	mapping   int
}

func NewDevice(inputDevice input.Device, inputEvents <-chan *evdev.InputEvent, midiEvents chan<- Event,
	logs chan<- string) Device {

	var config Config
	var absinfos = make(map[evdev.EvCode]evdev.AbsInfo)

	switch inputDevice.DeviceType {
	case input.KeyboardDevice:
		config = KeyboardConfig
	case input.JoystickDevice:
		config = JoystickConfig

		for ht, edev := range inputDevice.Evdevs {
			// TODO: handle situation where joystick provides more than one input handler
			if ht == input.DI_TYPE_JOYSTICK {
				absi, err := edev.AbsInfos()
				if err != nil {
					logs <- fmt.Sprintf("warning: failed to fetch absinfos [%s]", inputDevice.Name)
				}
				absinfos = absi
			}
		}
	default:
		panic(fmt.Sprintf("cannot pick config for \"%s\" device", inputDevice.DeviceType.String()))
	}

	return Device{
		config:      config,
		inputDevice: inputDevice,
		inputEvents: inputEvents,
		midiEvents:  midiEvents,
		logs:        logs,
		absinfos:    absinfos,

		noteTracker:   make(map[evdev.EvCode][2]byte, 32),
		actionTracker: make(map[Action]bool, 16),
	}
}

func (d *Device) ProcessEvents() {
	for ie := range d.inputEvents {
		if ie.Type == evdev.EV_SYN {
			continue
		}

		switch ie.Type {
		case evdev.EV_KEY:
			_, noteOk := d.config.midiMappings[d.mapping].mapping[ie.Code]
			action, actionOk := d.config.actionMapping[ie.Code]

			switch {
			case noteOk:
				switch ie.Value {
				case EV_KEY_PRESS:
					d.NoteOn(ie.Code)
				case EV_KEY_RELEASE:
					d.NoteOff(ie.Code)
				}
			case actionOk:
				switch ie.Value {
				case EV_KEY_PRESS:
					d.actionTracker[action] = true
					if len(d.actionTracker) > 1 {
						switch {
						case d.actionTracker[MappingUp] && d.actionTracker[MappingDown]:
							d.MappingReset()
						case d.actionTracker[OctaveUp] && d.actionTracker[OctaveDown]:
							d.OctaveReset()
						case d.actionTracker[SemitoneUp] && d.actionTracker[SemitoneDown]:
							d.SemitoneReset()
						case d.actionTracker[ChannelUp] && d.actionTracker[ChannelDown]:
							d.ChannelReset()
						}
						break
					}

					switch action {
					case Panic:
						d.Panic()
					case MappingUp:
						d.MappingUp()
					case MappingDown:
						d.MappingDown()
					case OctaveUp:
						d.OctaveUp()
					case OctaveDown:
						d.OctaveDown()
					case SemitoneUp:
						d.SemitoneUp()
					case SemitoneDown:
						d.SemitoneDown()
					case ChannelUp:
						d.ChannelUp()
					case ChannelDown:
						d.ChannelDown()
					}
					// TODO:
					// case Mapping:
					// case Channel:
				case EV_KEY_RELEASE:
					switch action {
					case Multinote:
						d.Multinote()
					}
					delete(d.actionTracker, action)
				}
			default:
				d.logs <- fmt.Sprintf("Unbinded Button: code: %d (hex: 0x%x), type: %d (hex: 0x%x) [%s]",
					ie.Code, ie.Code, ie.Type, ie.Type, d.inputDevice.Name)
			}
		case evdev.EV_ABS:
			analog, analogOk := d.config.analogMapping[ie.Code]

			switch {
			case analogOk:
				min, max := float64(d.absinfos[ie.Code].Minimum), float64(d.absinfos[ie.Code].Maximum)
				value := (float64(ie.Value) + math.Abs(min)) / (math.Abs(min) + max)
				if analog.flipAxis {
					value = 1 - value
				}

				var event Event
				switch analog.id {
				case AnalogCC:
					event = ControlChangeEvent(d.channel, analog.cc, byte(int(float64(127)*value)))
				case AnalogPitchBend:
					event = PitchBendEvent(d.channel, value)
				case AnalogKeySim:
					// TODO
					continue
				}

				d.logs <- fmt.Sprintf("%s [%s]", event.String(), d.inputDevice.Name)
				d.midiEvents <- event
			}
		}

	}
}

func (d *Device) NoteOn(code evdev.EvCode) {
	note, ok := d.config.midiMappings[d.mapping].mapping[code]
	if !ok {
		return
	}
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	d.noteTracker[code] = [2]byte{note, d.channel}
	event := NoteEvent(NoteOn, d.channel, note, 64)
	d.midiEvents <- event
	d.logs <- fmt.Sprintf("%s [%s]", event.String(), d.inputDevice.Name)

	for _, offset := range d.multiNote {
		multiNote := noteCalculatored + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		note = uint8(multiNote)
		// untracked notes
		event = NoteEvent(NoteOn, d.channel, note, 64)
		d.midiEvents <- event
		d.logs <- fmt.Sprintf("%s [%s]", event.String(), d.inputDevice.Name)
	}
}

func (d *Device) NoteOff(code evdev.EvCode) {
	noteAndChannel, ok := d.noteTracker[code]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	delete(d.noteTracker, code)

	event := NoteEvent(NoteOff, channel, note, 0)
	d.midiEvents <- event
	d.logs <- fmt.Sprintf("%s [%s]", event.String(), d.inputDevice.Name)

	for _, offset := range d.multiNote {
		multiNote := int(note) + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		newNote := uint8(multiNote)
		event = NoteEvent(NoteOff, channel, newNote, 0)
		d.midiEvents <- event
		d.logs <- fmt.Sprintf("%s [%s]", event.String(), d.inputDevice.Name)
	}
}

func (d *Device) OctaveDown() {
	d.octave--
	d.logs <- fmt.Sprintf("octave down (%d) [%s]", d.octave, d.inputDevice.Name)
}

func (d *Device) OctaveUp() {
	d.octave++
	d.logs <- fmt.Sprintf("octave up (%d) [%s]", d.octave, d.inputDevice.Name)
}

func (d *Device) OctaveReset() {
	d.octave = 0
	d.logs <- fmt.Sprintf("octave reset (%d) [%s]", d.octave, d.inputDevice.Name)
}

func (d *Device) SemitoneDown() {
	d.semitone--
	d.logs <- fmt.Sprintf("semitone down (%d) [%s]", d.semitone, d.inputDevice.Name)
}

func (d *Device) SemitoneUp() {
	d.semitone++
	d.logs <- fmt.Sprintf("semitone up (%d) [%s]", d.semitone, d.inputDevice.Name)
}

func (d *Device) SemitoneReset() {
	d.semitone = 0
	d.logs <- fmt.Sprintf("semitone reset (%d) [%s]", d.semitone, d.inputDevice.Name)
}

func (d *Device) MappingDown() {
	if d.mapping == 0 {
		return
	}
	d.mapping--
	d.logs <- fmt.Sprintf("mapping down (%s) [%s]", d.config.midiMappings[d.mapping].name, d.inputDevice.Name)
}

func (d *Device) MappingUp() {
	if d.mapping == len(d.config.midiMappings)-1 {
		return
	}
	d.mapping++
	d.logs <- fmt.Sprintf("mapping up (%s) [%s]", d.config.midiMappings[d.mapping].name, d.inputDevice.Name)
}

func (d *Device) MappingReset() {
	d.mapping = 0
	d.logs <- fmt.Sprintf("mapping reset (%s) [%s]", d.config.midiMappings[d.mapping].name, d.inputDevice.Name)
}

func (d *Device) ChannelDown() {
	if d.channel == 0 {
		return
	}
	d.channel--
	d.logs <- fmt.Sprintf("channel down (%2d) [%s]", d.channel+1, d.inputDevice.Name)
}

func (d *Device) ChannelUp() {
	if d.channel == 15 {
		return
	}
	d.channel++
	d.logs <- fmt.Sprintf("channel up (%2d) [%s]", d.channel+1, d.inputDevice.Name)
}

func (d *Device) ChannelReset() {
	d.channel = 0
	d.logs <- fmt.Sprintf("channel reset (%2d) [%s]", d.channel+1, d.inputDevice.Name)
}

func (d *Device) Multinote() {
	var pressedNotes []int
	for _, noteAndChannel := range d.noteTracker {
		pressedNotes = append(pressedNotes, int(noteAndChannel[0]))
	}

	if len(pressedNotes) == 0 {
		d.logs <- fmt.Sprintf("Bruh, no pressed notes, multinote mode disengaged [%s]", d.inputDevice.Name)
		d.multiNote = []int{}
		return
	}

	if len(pressedNotes) == 1 {
		d.logs <- fmt.Sprintf("Bruh, press more than one note, multinote mode disengaged [%s]", d.inputDevice.Name)
		d.multiNote = []int{}
		return
	}

	sort.Ints(pressedNotes)
	minVal := pressedNotes[0]

	for i, note := range pressedNotes {
		if i == 0 {
			continue
		}
		d.multiNote = append(d.multiNote, note-minVal)
	}

	var intervals = ""
	for i, interval := range d.multiNote {
		name, ok := intervalToString[interval]
		if !ok {
			intervals += "..."
			break
		}
		if i == 0 {
			intervals += fmt.Sprintf("%s", name)
		} else {
			intervals += fmt.Sprintf(", %s", name)
		}
	}

	d.logs <- fmt.Sprintf("Multinote mode engaged, intervals: %v/[%s] [%s]", d.multiNote, intervals, d.inputDevice.Name)
}

func (d *Device) Panic() {
	d.logs <- fmt.Sprintf("Panic! [%s]", d.inputDevice.Name)

	for note := uint8(0); note < 128; note++ {
		d.midiEvents <- NoteEvent(NoteOff, d.channel, note, 0)
	}
}
