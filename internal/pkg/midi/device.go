package midi

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"hidi/internal/pkg/input"
	"hidi/internal/pkg/logg"

	"github.com/gethiox/go-evdev"
)

const (
	EV_KEY_RELEASE = 0
	EV_KEY_PRESS   = 1
	EV_KEY_REPEAT  = 2
)

type Device struct {
	config      Config
	InputDevice input.Device
	inputEvents <-chan *input.InputEvent
	midiEvents  chan<- Event
	logs        chan<- logg.LogEntry

	// TODO: move this to input.Device
	// absinfos holds information about boundaries of analog information
	absinfos map[string]map[evdev.EvCode]evdev.AbsInfo // map key: DeviceInfo.Event()

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

func NewDevice(inputDevice input.Device, config DeviceConfig, inputEvents <-chan *input.InputEvent, midiEvents chan<- Event,
	logs chan<- logg.LogEntry) Device {

	// var config Config
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
				logs <- logg.Warning(fmt.Sprintf("Failed to fetch absinfos [%s]", inputDevice.Name))
				absinfos[event] = make(map[evdev.EvCode]evdev.AbsInfo)
				continue
			}

			absinfos[event] = absi
		}
	}

	return Device{
		config:      config.Config,
		InputDevice: inputDevice,
		inputEvents: inputEvents,
		midiEvents:  midiEvents,
		logs:        logs,
		absinfos:    absinfos,

		noteTracker:   make(map[evdev.EvCode][2]byte, 32),
		actionTracker: make(map[Action]bool, 16),
	}
}

func (d *Device) ProcessEvents(ctx context.Context) {
root:
	for {
		var ie *input.InputEvent
		select {
		case <-ctx.Done():
			break root
		case ie = <-d.inputEvents:
			break
		}

		switch ie.Event.Type {
		case evdev.EV_SYN:
			continue
		case evdev.EV_KEY:
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
				switch {
				case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_RELEASE:
					continue
				case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_REPEAT:
					continue
				}

				d.logs <- logg.Debug(fmt.Sprintf(
					"Unbinded Button: code: %3d (0x%02x) [%s], type: %2x [%s] value: %d (0x%x) [%s]",
					ie.Event.Code, ie.Event.Code, evdev.KEYToString[ie.Event.Code],
					ie.Event.Type, evdev.EVToString[ie.Event.Type],
					ie.Event.Value, ie.Event.Value,
					d.InputDevice.Name,
				))
			}
		case evdev.EV_ABS:
			analog, analogOk := d.config.KeyMappings[d.mapping].Analog[ie.Event.Code]

			switch {
			case analogOk:
				min := float64(d.absinfos[ie.Source.Event()][ie.Event.Code].Minimum)
				max := float64(d.absinfos[ie.Source.Event()][ie.Event.Code].Maximum)
				value := (float64(ie.Event.Value) + math.Abs(min)) / (math.Abs(min) + max)
				if analog.flipAxis {
					value = 1 - value
				}

				var event Event
				switch analog.id {
				case AnalogCC:
					event = ControlChangeEvent(d.channel, analog.cc, byte(int(float64(127)*value)))
					fmt.Printf("PROCESSING ANALOG, %+v, %+v, %+v\n", d.channel, analog.cc, byte(int(float64(127)*value)))
				case AnalogPitchBend:
					event = PitchBendEvent(d.channel, value)
					fmt.Printf("PROCESSING ANALOG 2, %+v, %+v\n", d.channel, value)
				case AnalogKeySim:
					// TODO
					continue
				default:
					panic(fmt.Sprintf("unexpected AnalogID type: %+v", analog.id))
				}

				fmt.Printf("PROCESSING ANALOG, %#v\n", []byte(event))

				d.midiEvents <- event
				d.logs <- logg.Debug(fmt.Sprintf("%s [%s]", event.String(), d.InputDevice.Name))
			}
		}
	}

	if len(d.noteTracker) > 0 {
		d.logs <- logg.Debug("active midi notes cleanup")
	}

	for evcode := range d.noteTracker {
		d.NoteOff(evcode)
	}
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
	d.logs <- logg.Debug(fmt.Sprintf("%s [%s]", event.String(), d.InputDevice.Name))

	for _, offset := range d.multiNote {
		multiNote := noteCalculatored + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		note = uint8(multiNote)
		// untracked notes
		event = NoteEvent(NoteOn, d.channel, note, 64)
		d.midiEvents <- event
		d.logs <- logg.Debug(fmt.Sprintf("%s [%s]", event.String(), d.InputDevice.Name))
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
	d.logs <- logg.Debug(fmt.Sprintf("%s [%s]", event.String(), d.InputDevice.Name))

	for _, offset := range d.multiNote {
		multiNote := int(note) + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		newNote := uint8(multiNote)
		event = NoteEvent(NoteOff, channel, newNote, 0)
		d.midiEvents <- event
		d.logs <- logg.Debug(fmt.Sprintf("%s [%s]", event.String(), d.InputDevice.Name))
	}
}

func (d *Device) OctaveDown() {
	d.octave--
	d.logs <- logg.Info(fmt.Sprintf("octave down (%d) [%s]", d.octave, d.InputDevice.Name))
}

func (d *Device) OctaveUp() {
	d.octave++
	d.logs <- logg.Info(fmt.Sprintf("octave up (%d) [%s]", d.octave, d.InputDevice.Name))
}

func (d *Device) OctaveReset() {
	d.octave = 0
	d.logs <- logg.Info(fmt.Sprintf("octave reset (%d) [%s]", d.octave, d.InputDevice.Name))
}

func (d *Device) SemitoneDown() {
	d.semitone--
	d.logs <- logg.Info(fmt.Sprintf("semitone down (%d) [%s]", d.semitone, d.InputDevice.Name))
}

func (d *Device) SemitoneUp() {
	d.semitone++
	d.logs <- logg.Info(fmt.Sprintf("semitone up (%d) [%s]", d.semitone, d.InputDevice.Name))
}

func (d *Device) SemitoneReset() {
	d.semitone = 0
	d.logs <- logg.Info(fmt.Sprintf("semitone reset (%d) [%s]", d.semitone, d.InputDevice.Name))
}

func (d *Device) MappingDown() {
	if d.mapping != 0 {
		d.mapping--
	}
	d.logs <- logg.Info(fmt.Sprintf("mapping down (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name))
}

func (d *Device) MappingUp() {
	if d.mapping != len(d.config.KeyMappings)-1 {
		d.mapping++
	}
	d.logs <- logg.Info(fmt.Sprintf("mapping up (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name))
}

func (d *Device) MappingReset() {
	d.mapping = 0
	d.logs <- logg.Info(fmt.Sprintf("mapping reset (%s) [%s]", d.config.KeyMappings[d.mapping].Name, d.InputDevice.Name))
}

func (d *Device) ChannelDown() {
	if d.channel != 0 {
		d.channel--
	}

	d.logs <- logg.Info(fmt.Sprintf("channel down (%2d) [%s]", d.channel+1, d.InputDevice.Name))
}

func (d *Device) ChannelUp() {
	if d.channel != 15 {
		d.channel++
	}
	d.logs <- logg.Info(fmt.Sprintf("channel up (%2d) [%s]", d.channel+1, d.InputDevice.Name))
}

func (d *Device) ChannelReset() {
	d.channel = 0
	d.logs <- logg.Info(fmt.Sprintf("channel reset (%2d) [%s]", d.channel+1, d.InputDevice.Name))
}

func (d *Device) Multinote() {
	var pressedNotes []int
	for _, noteAndChannel := range d.noteTracker {
		pressedNotes = append(pressedNotes, int(noteAndChannel[0]))
	}

	if len(pressedNotes) == 0 {
		d.logs <- logg.Info(fmt.Sprintf("Bruh, no pressed notes, multinote mode disengaged [%s]", d.InputDevice.Name))
		d.multiNote = []int{}
		return
	}

	if len(pressedNotes) == 1 {
		d.logs <- logg.Info(fmt.Sprintf("Bruh, press more than one note, multinote mode disengaged [%s]", d.InputDevice.Name))
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
	d.logs <- logg.Info(fmt.Sprintf("Multinote mode engaged, intervals: %v/[%s] [%s]", d.multiNote, intervals, d.InputDevice.Name))
}

func (d *Device) Panic() {
	d.midiEvents <- ControlChangeEvent(d.channel, AllNotesOff, 0)

	// Some plugins may not respect AllNotesOff control change message, there is a simple workaround
	for note := uint8(0); note < 128; note++ {
		d.midiEvents <- NoteEvent(NoteOff, d.channel, note, 0)
	}

	d.logs <- logg.Info(fmt.Sprintf("Panic! [%s]", d.InputDevice.Name))
}
