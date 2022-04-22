package midi

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"

	"github.com/holoplot/go-evdev"
)

const (
	EV_KEY_RELEASE = 0
	EV_KEY_PRESS   = 1
	EV_KEY_REPEAT  = 2
)

type Device struct {
	config      config.Config
	InputDevice input.Device
	inputEvents <-chan input.InputEvent
	midiEvents  chan<- Event

	// TODO: move this to input.Device
	// absinfos holds information about boundaries of analog information
	absinfos map[string]map[evdev.EvCode]evdev.AbsInfo // map key: DeviceInfo.Event()

	// instead of generating NoteOff events based on the current Device state (lazy approach), every emitted note
	// is being tracked and released precisely on related hardware button release.
	// This approach gives much nicer user experience as the User may conveniently hold some keys
	// and modify state on the fly (changing octave, channel etc.), NoteOff events will be emitted correctly anyway.
	noteTracker       map[evdev.EvCode][2]byte // 1: note, 2: channel
	analogNoteTracker map[string][2]byte       // 1: note, 2: channel
	actionTracker     map[config.Action]bool
	octave            int8
	semitone          int8
	channel           uint8
	// warning: currently lazy implementation
	multiNote []int // list of additional note intervals (offsets)
	mapping   int
}

func NewDevice(inputDevice input.Device, cfg config.DeviceConfig, inputEvents <-chan input.InputEvent, midiEvents chan<- Event) Device {
	var absinfos = make(map[string]map[evdev.EvCode]evdev.AbsInfo)

	if inputDevice.DeviceType == input.JoystickDevice {
		for ht, edev := range inputDevice.Evdevs {
			if ht != input.DI_TYPE_JOYSTICK {
				continue
			}
			eventRaw := strings.Split(edev.Path(), "/")
			event := eventRaw[len(eventRaw)-1]

			absi, err := edev.AbsInfos()
			if err != nil {
				log.Printf("Failed to fetch absinfos [%s]", inputDevice.Name)
				absinfos[event] = make(map[evdev.EvCode]evdev.AbsInfo)
				continue
			}

			absinfos[event] = absi

			edev.NonBlock() // hotfix, TODO: remove it
		}
	}

	return Device{
		config:      cfg.Config,
		InputDevice: inputDevice,
		inputEvents: inputEvents,
		midiEvents:  midiEvents,
		absinfos:    absinfos,

		noteTracker:       make(map[evdev.EvCode][2]byte, 32),
		analogNoteTracker: make(map[string][2]byte, 32),
		actionTracker:     make(map[config.Action]bool, 16),
	}
}

func (d *Device) ProcessEvents() {
	var actionMap = map[config.Action]func(){
		config.Panic:        d.Panic,
		config.MappingUp:    d.MappingUp,
		config.MappingDown:  d.MappingDown,
		config.OctaveUp:     d.OctaveUp,
		config.OctaveDown:   d.OctaveDown,
		config.SemitoneUp:   d.SemitoneUp,
		config.SemitoneDown: d.SemitoneDown,
		config.ChannelUp:    d.ChannelUp,
		config.ChannelDown:  d.ChannelDown,
		config.Multinote:    func() {}, // on key release only
	}

	invokeAction := func(action config.Action) {
		if f, ok := actionMap[action]; ok {
			f()
		}
	}

	checkDoubleActions := func() (executed bool) {
		if len(d.actionTracker) > 1 {
			switch {
			case d.actionTracker[config.MappingUp] && d.actionTracker[config.MappingDown]:
				d.MappingReset()
			case d.actionTracker[config.OctaveUp] && d.actionTracker[config.OctaveDown]:
				d.OctaveReset()
			case d.actionTracker[config.SemitoneUp] && d.actionTracker[config.SemitoneDown]:
				d.SemitoneReset()
			case d.actionTracker[config.ChannelUp] && d.actionTracker[config.ChannelDown]:
				d.ChannelReset()
			default:
				return false
			}
			return true
		}
		return false
	}

	for ie := range d.inputEvents {
		if ie.Event.Type == evdev.EV_SYN {
			continue
		}

		switch ie.Event.Type {
		case evdev.EV_KEY:
			if ie.Event.Value == EV_KEY_REPEAT {
				break
			}

			_, noteOk := d.config.KeyMappings[d.mapping].Midi[ie.Event.Code]
			action, actionOk := d.config.ActionMapping[ie.Event.Code]

			switch {
			case noteOk:
				switch ie.Event.Value {
				case EV_KEY_PRESS:
					d.NoteOn(ie.Event.Code)
				case EV_KEY_RELEASE:
					d.NoteOff(ie.Event.Code)
				}
			case actionOk:
				switch ie.Event.Value {
				case EV_KEY_PRESS:
					d.actionTracker[action] = true
					if !checkDoubleActions() {
						invokeAction(action)
					}
				case EV_KEY_RELEASE:
					switch action {
					case config.Multinote:
						d.Multinote()
					}
					delete(d.actionTracker, action)
				}
			default:
				switch {
				case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_RELEASE:
					continue
				case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_REPEAT:
					continue
				}

				log.Printf(
					"Unbinded Button: code: %3d (0x%02x) [%s], type: %2x [%s] value: %d (0x%x) [%s] [%s]",
					ie.Event.Code, ie.Event.Code, evdev.KEYToString[ie.Event.Code],
					ie.Event.Type, evdev.EVToString[ie.Event.Type],
					ie.Event.Value, ie.Event.Value,
					d.InputDevice.Name, ie.Source.Event(),
				)
			}
		case evdev.EV_ABS:
			analog, analogOk := d.config.KeyMappings[d.mapping].Analog[ie.Event.Code]

			switch {
			case analogOk:
				// converting integer value to float
				// -1.0 - 1.0 range if negative values are included, 0.0 - 1.0 otherwise
				var value float64
				var canBeNegative bool
				min := d.absinfos[ie.Source.Event()][ie.Event.Code].Minimum
				max := d.absinfos[ie.Source.Event()][ie.Event.Code].Maximum
				if min < 0 {
					canBeNegative = true
				}

				if ie.Event.Value < 0 {
					value = float64(ie.Event.Value) / math.Abs(float64(min))
				} else {
					value = float64(ie.Event.Value) / math.Abs(float64(max))
				}

				if analog.FlipAxis {
					if canBeNegative {
						value = -value
					} else {
						value = 1.0 - value
					}
				}

				var event Event
				switch analog.ID {
				case config.AnalogCC:
					log.Printf("cc: value: %.1f, can negative: %v, bidirect: %v", value, canBeNegative, analog.Bidirectional)

					var adjustedValue float64
					var target byte

					switch {
					case canBeNegative && analog.Bidirectional:
						adjustedValue = math.Abs(value)
						if value < 0 {
							target = analog.CCNeg
						} else {
							target = analog.CC
						}
					case canBeNegative && !analog.Bidirectional:
						adjustedValue = (value + 1) / 2
						target = analog.CC
					case !canBeNegative && analog.Bidirectional:
						adjustedValue = math.Abs(value*2 - 1)
						if value < 0.5 {
							target = analog.CCNeg
						} else {
							target = analog.CC
						}
					case !canBeNegative && !analog.Bidirectional:
						adjustedValue = value
						target = analog.CC
					default:
						panic("ouu")
					}

					event = ControlChangeEvent(d.channel, target, byte(int(float64(127)*adjustedValue)))
				case config.AnalogPitchBend:
					if canBeNegative {
						event = PitchBendEvent(d.channel, value)
					} else {
						event = PitchBendEvent(d.channel, value*2-1.0)
					}
				case config.AnalogKeySim:
					identifier := fmt.Sprintf("%d", ie.Event.Code) // for tracking purpose
					note := analog.Note

					if analog.Bidirectional && value < 0 {
						note = analog.NoteNeg
						identifier = fmt.Sprintf("%d_neg", ie.Event.Code)
					}

					v := math.Abs(value)
					switch {
					case v > 0.5:
						_, ok := d.analogNoteTracker[identifier]
						if !ok {
							d.AnalogNoteOn(identifier, note)
						}
					case v < 0.49:
						d.AnalogNoteOff(fmt.Sprintf("%d", ie.Event.Code))
						d.AnalogNoteOff(fmt.Sprintf("%d_neg", ie.Event.Code))
					}
					continue
				case config.AnalogActionSim:
					action := analog.Action
					if analog.Bidirectional && value < 0 {
						action = analog.ActionNeg
					}

					if checkDoubleActions() {
						continue
					}

					v := math.Abs(value)
					if v > 0.5 {
						invokeAction(action)
						d.actionTracker[action] = true
					} else {
						delete(d.actionTracker, analog.Action)
						delete(d.actionTracker, analog.ActionNeg)
					}

					continue
				default:
					panic(fmt.Sprintf("unexpected AnalogID type: %+v", analog.ID))
				}

				d.midiEvents <- event
				// log.Printf("%s [%s]", event.String(), d.InputDevice.Name)
			}
		}
	}

	if len(d.noteTracker) > 0 || len(d.analogNoteTracker) > 0 {
		log.Printf("active midi notes cleanup [%s]", d.InputDevice.Name)
	}

	for evcode := range d.noteTracker {
		d.NoteOff(evcode)
	}
	for identifier := range d.analogNoteTracker {
		d.AnalogNoteOff(identifier)
	}
	log.Printf("virtual midi device exited [%s]", d.InputDevice.Name)
}

func (d *Device) NoteOn(evCode evdev.EvCode) {
	note, ok := d.config.KeyMappings[d.mapping].Midi[evCode]
	if !ok {
		return
	}
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	d.noteTracker[evCode] = [2]byte{note, d.channel}
	event := NoteEvent(NoteOn, d.channel, note, 64)
	d.midiEvents <- event
	log.Printf("%s [%s]", event.String(), d.InputDevice.Name)

	for _, offset := range d.multiNote {
		multiNote := noteCalculatored + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		note = uint8(multiNote)
		// untracked notes
		event = NoteEvent(NoteOn, d.channel, note, 64)
		d.midiEvents <- event
		log.Printf("%s [%s]", event.String(), d.InputDevice.Name)
	}
}

func (d *Device) NoteOff(evCode evdev.EvCode) {
	noteAndChannel, ok := d.noteTracker[evCode]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	event := NoteEvent(NoteOff, channel, note, 0)
	d.midiEvents <- event
	delete(d.noteTracker, evCode)
	log.Printf("%s [%s]", event.String(), d.InputDevice.Name)

	for _, offset := range d.multiNote {
		multiNote := int(note) + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		newNote := uint8(multiNote)
		event = NoteEvent(NoteOff, channel, newNote, 0)
		d.midiEvents <- event
		log.Printf("%s [%s]", event.String(), d.InputDevice.Name)
	}
}

func (d *Device) AnalogNoteOn(identifier string, note byte) {
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	d.analogNoteTracker[identifier] = [2]byte{note, d.channel}
	event := NoteEvent(NoteOn, d.channel, note, 64)
	d.midiEvents <- event
	log.Printf("%s [%s]", event.String(), d.InputDevice.Name)
}

func (d *Device) AnalogNoteOff(identifier string) {
	noteAndChannel, ok := d.analogNoteTracker[identifier]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	event := NoteEvent(NoteOff, channel, note, 0)
	d.midiEvents <- event
	delete(d.analogNoteTracker, identifier)
	log.Printf("%s [%s]", event.String(), d.InputDevice.Name)
}

func (d *Device) OctaveDown() {
	d.octave--
	log.Printf("octave down (%d) [%s]", d.octave, d.InputDevice.Name)
}

func (d *Device) OctaveUp() {
	d.octave++
	log.Printf("octave up (%d) [%s]", d.octave, d.InputDevice.Name)
}

func (d *Device) OctaveReset() {
	d.octave = 0
	log.Printf("octave reset (%d) [%s]", d.octave, d.InputDevice.Name)
}

func (d *Device) SemitoneDown() {
	d.semitone--
	log.Printf("semitone down (%d) [%s]", d.semitone, d.InputDevice.Name)
}

func (d *Device) SemitoneUp() {
	d.semitone++
	log.Printf("semitone up (%d) [%s]", d.semitone, d.InputDevice.Name)
}

func (d *Device) SemitoneReset() {
	d.semitone = 0
	log.Printf("semitone reset (%d) [%s]", d.semitone, d.InputDevice.Name)
}

func (d *Device) MappingDown() {
	if d.mapping != 0 {
		d.mapping--
	}
	log.Printf("mapping down (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name)
}

func (d *Device) MappingUp() {
	if d.mapping != len(d.config.KeyMappings)-1 {
		d.mapping++
	}
	log.Printf("mapping up (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name)
}

func (d *Device) MappingReset() {
	d.mapping = 0
	log.Printf("mapping reset (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name)
}

func (d *Device) ChannelDown() {
	if d.channel != 0 {
		d.channel--
	}

	log.Printf("channel down (%2d) [%s]", d.channel+1, d.InputDevice.Name)
}

func (d *Device) ChannelUp() {
	if d.channel != 15 {
		d.channel++
	}
	log.Printf("channel up (%2d) [%s]", d.channel+1, d.InputDevice.Name)
}

func (d *Device) ChannelReset() {
	d.channel = 0
	log.Printf("channel reset (%2d) [%s]", d.channel+1, d.InputDevice.Name)
}

func (d *Device) Multinote() {
	var pressedNotes []int
	for _, noteAndChannel := range d.noteTracker {
		pressedNotes = append(pressedNotes, int(noteAndChannel[0]))
	}

	if len(pressedNotes) == 0 {
		log.Printf("Bruh, no pressed notes, multinote mode disengaged [%s]", d.InputDevice.Name)
		d.multiNote = []int{}
		return
	}

	if len(pressedNotes) == 1 {
		log.Printf("Bruh, press more than one note, multinote mode disengaged [%s]", d.InputDevice.Name)
		d.multiNote = []int{}
		return
	}

	sort.Ints(pressedNotes)
	minVal := pressedNotes[0]
	var noteOffsets []int

	for i, note := range pressedNotes {
		if i == 0 {
			continue
		}
		noteOffsets = append(noteOffsets, note-minVal)
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

	d.multiNote = noteOffsets
	log.Printf("Multinote mode engaged, intervals: %v/[%s] [%s]", d.multiNote, intervals, d.InputDevice.Name)
}

func (d *Device) Panic() {
	d.midiEvents <- ControlChangeEvent(d.channel, AllNotesOff, 0)

	// Some plugins may not respect AllNotesOff control change message, there is a simple workaround
	for note := uint8(0); note < 128; note++ {
		d.midiEvents <- NoteEvent(NoteOff, d.channel, note, 0)
	}

	log.Printf("Panic! [%s]", d.InputDevice.Name)
}
