package midi

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/holoplot/go-evdev"
	"go.uber.org/zap"
)

var log = logger.GetLogger()

const (
	EV_KEY_RELEASE = 0
	EV_KEY_PRESS   = 1
	EV_KEY_REPEAT  = 2
)

type Device struct {
	noLogs      bool // skips producing most of the log entries for maximum performance
	config      config.Config
	InputDevice input.Device
	inputEvents <-chan *input.InputEvent
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
	ccZeroed          map[byte]bool // 1: positive, 2: negative

	octave   int8
	semitone int8
	channel  uint8
	// warning: currently lazy implementation
	multiNote  []int // list of additional note intervals (offsets)
	mapping    int
	ccLearning bool

	actionsPress   map[config.Action]func(*Device)
	actionsRelease map[config.Action]func(*Device)
}

func NewDevice(inputDevice input.Device, cfg config.DeviceConfig, inputEvents <-chan *input.InputEvent, midiEvents chan<- Event, noLogs bool) Device {
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
				log.Info(fmt.Sprintf("Failed to fetch absinfos [%s]", inputDevice.Name), logger.Warning)
				absinfos[event] = make(map[evdev.EvCode]evdev.AbsInfo)
				continue
			}

			absinfos[event] = absi

			edev.NonBlock() // hotfix, TODO: remove it
		}
	}

	d := Device{
		noLogs:      noLogs,
		config:      cfg.Config,
		InputDevice: inputDevice,
		inputEvents: inputEvents,
		midiEvents:  midiEvents,
		absinfos:    absinfos,

		noteTracker:       make(map[evdev.EvCode][2]byte, 32),
		analogNoteTracker: make(map[string][2]byte, 32),
		actionTracker:     make(map[config.Action]bool, 16),
		ccZeroed:          make(map[byte]bool, 32),
	}

	d.actionsPress = map[config.Action]func(*Device){
		config.Panic:        (*Device).Panic,
		config.MappingUp:    (*Device).MappingUp,
		config.MappingDown:  (*Device).MappingDown,
		config.OctaveUp:     (*Device).OctaveUp,
		config.OctaveDown:   (*Device).OctaveDown,
		config.SemitoneUp:   (*Device).SemitoneUp,
		config.SemitoneDown: (*Device).SemitoneDown,
		config.ChannelUp:    (*Device).ChannelUp,
		config.ChannelDown:  (*Device).ChannelDown,
		config.Multinote:    func(*Device) {}, // on key release only
		config.Learning:     (*Device).CCLearningOn,
	}
	d.actionsRelease = map[config.Action]func(*Device){
		config.Learning: (*Device).CCLearningOff,
	}
	return d
}

func (d *Device) logFields(fields ...zap.Field) []zap.Field {
	fields = append(fields, zap.String("device_name", d.InputDevice.Name))
	return fields
}

func (d *Device) invokeActionPress(action config.Action) {
	if f, ok := d.actionsPress[action]; ok {
		f(d)
	}
}

func (d *Device) invokeActionRelease(action config.Action) {
	if f, ok := d.actionsRelease[action]; ok {
		f(d)
	}
}

func (d *Device) checkDoubleActions() bool {
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

func (d *Device) processEvent(event *input.InputEvent) {
	if event.Event.Type == evdev.EV_SYN {
		return
	}

	if event.Event.Type == evdev.EV_KEY && event.Event.Value == EV_KEY_REPEAT {
		return
	}

	switch event.Event.Type {
	case evdev.EV_KEY:
		d.handleKEYEvent(event)
	case evdev.EV_ABS:
		d.handleABSEvent(event)
	}
}

func (d *Device) ProcessEvents(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info("start ProcessEvents", d.logFields(logger.Debug)...)

	for ie := range d.inputEvents {
		d.processEvent(ie)
	}

	if len(d.noteTracker) > 0 || len(d.analogNoteTracker) > 0 {
		log.Info("active midi notes cleanup", d.logFields(logger.Debug)...)
	}

	for evcode := range d.noteTracker {
		d.NoteOff(&input.InputEvent{
			Source: input.DeviceInfo{},
			Event: evdev.InputEvent{
				Time:  syscall.Timeval{},
				Type:  evdev.EV_KEY,
				Code:  evcode,
				Value: 0,
			},
		})
	}
	for identifier := range d.analogNoteTracker {
		d.AnalogNoteOff(identifier)
	}
	log.Info("virtual midi device exited", d.logFields(logger.Debug)...)
}

func (d *Device) handleKEYEvent(ie *input.InputEvent) {
	_, noteOk := d.config.KeyMappings[d.mapping].Midi[ie.Event.Code]
	action, actionOk := d.config.ActionMapping[ie.Event.Code]

	switch {
	case actionOk:
		switch ie.Event.Value {
		case EV_KEY_PRESS:
			d.actionTracker[action] = true
			if !d.checkDoubleActions() {
				d.invokeActionPress(action)
			}
		case EV_KEY_RELEASE:
			switch action {
			case config.Multinote:
				d.Multinote()
			}
			d.invokeActionRelease(action)
			delete(d.actionTracker, action)
		}
	case noteOk:
		switch ie.Event.Value {
		case EV_KEY_PRESS:
			d.NoteOn(ie)
		case EV_KEY_RELEASE:
			d.NoteOff(ie)
		}
	default:
		switch {
		case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_RELEASE:
			return
		case ie.Event.Type == evdev.EV_KEY && ie.Event.Value == EV_KEY_REPEAT:
			return
		}

		if !d.noLogs {
			log.Info(fmt.Sprintf("Undefined KEY event: %s", ie.Event.String()), d.logFields(
				logger.KeysNotAssigned,
				zap.String("handler_event", ie.Source.Event()),
			)...)
		}
	}
}

func (d *Device) handleABSEvent(ie *input.InputEvent) {
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

		if d.ccLearning && !(value < -0.5 || value > 0.5) {
			return
		}

		if !d.noLogs {
			log.Info(fmt.Sprintf("Analog event: %s", ie.Event.String()),
				d.logFields(logger.Analog, zap.String("handler_event", ie.Source.Event()))...)
		}

		switch analog.MappingType {
		case config.AnalogCC:
			var adjustedValue float64

			switch {
			case canBeNegative && analog.Bidirectional:
				adjustedValue = math.Abs(value)
				if value < 0 {
					d.midiEvents <- ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
					if !d.ccZeroed[analog.CC] {
						d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, 0)
						d.ccZeroed[analog.CC] = true
					}
					d.ccZeroed[analog.CCNeg] = false
				} else {
					d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
					if !d.ccZeroed[analog.CCNeg] {
						d.midiEvents <- ControlChangeEvent(d.channel, analog.CCNeg, 0)
						d.ccZeroed[analog.CCNeg] = true
					}
					d.ccZeroed[analog.CC] = false
				}
			case canBeNegative && !analog.Bidirectional:
				adjustedValue = (value + 1) / 2
				d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
			case !canBeNegative && analog.Bidirectional:
				adjustedValue = math.Abs(value*2 - 1)
				if value < 0.5 {
					d.midiEvents <- ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
					if !d.ccZeroed[analog.CC] {
						d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, 0)
						d.ccZeroed[analog.CC] = true
					}
					d.ccZeroed[analog.CCNeg] = false
				} else {
					d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
					if !d.ccZeroed[analog.CCNeg] {
						d.midiEvents <- ControlChangeEvent(d.channel, analog.CCNeg, 0)
						d.ccZeroed[analog.CCNeg] = true
					}
					d.ccZeroed[analog.CC] = false
				}
			case !canBeNegative && !analog.Bidirectional:
				adjustedValue = value
				d.midiEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
			}
		case config.AnalogPitchBend:
			if canBeNegative {
				d.midiEvents <- PitchBendEvent(d.channel, value)
			} else {
				d.midiEvents <- PitchBendEvent(d.channel, value*2-1.0)
			}
		case config.AnalogKeySim:
			if !canBeNegative {
				value = value*2 - 1.0
			}

			identifier := fmt.Sprintf("%d", ie.Event.Code)
			identifierNeg := fmt.Sprintf("%d_neg", ie.Event.Code)

			switch {
			case value <= -0.5:
				_, ok := d.analogNoteTracker[identifierNeg]
				if !ok {
					d.AnalogNoteOn(identifierNeg, analog.NoteNeg)
				}
				d.AnalogNoteOff(identifier)
			case value > -0.49 && value < 0.49:
				d.AnalogNoteOff(identifier)
				d.AnalogNoteOff(identifierNeg)
			case value >= 0.5:
				_, ok := d.analogNoteTracker[identifier]
				if !ok {
					d.AnalogNoteOn(identifier, analog.Note)
				}
				d.AnalogNoteOff(identifierNeg)
			}

		case config.AnalogActionSim:
			if d.checkDoubleActions() {
				return
			}

			if !canBeNegative {
				value = value*2 - 1.0
			}

			switch {
			case value <= -0.5:
				d.invokeActionPress(analog.ActionNeg)
				d.actionTracker[analog.ActionNeg] = true

				d.invokeActionRelease(analog.Action)
				delete(d.actionTracker, analog.Action)
			case value > -0.49 && value < 0.49:
				d.invokeActionRelease(analog.ActionNeg)
				d.invokeActionRelease(analog.Action)
				delete(d.actionTracker, analog.ActionNeg)
				delete(d.actionTracker, analog.Action)
			case value >= 0.5:
				d.invokeActionPress(analog.Action)
				d.actionTracker[analog.Action] = true

				delete(d.actionTracker, analog.ActionNeg)
				d.invokeActionRelease(analog.ActionNeg)
			}
		default:
			log.Info(fmt.Sprintf("unexpected AnalogID type: %+v", analog.MappingType),
				d.logFields(logger.Warning, zap.String("handler_event", ie.Source.Event()))...,
			)
		}
	default:
		if !d.noLogs {
			log.Info(
				fmt.Sprintf("Undefined ABS event: %s", ie.Event.String()),
				d.logFields(logger.Analog, zap.String("handler_event", ie.Source.Event()))...,
			)
		}
	}
}

func (d *Device) NoteOn(ev *input.InputEvent) {
	note, ok := d.config.KeyMappings[d.mapping].Midi[ev.Event.Code]
	if !ok {
		return
	}
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	d.noteTracker[ev.Event.Code] = [2]byte{note, d.channel}
	event := NoteEvent(NoteOn, d.channel, note, 64)
	d.midiEvents <- event
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
	}

	for _, offset := range d.multiNote {
		multiNote := noteCalculatored + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		note = uint8(multiNote)
		// untracked notes
		event = NoteEvent(NoteOn, d.channel, note, 64)
		d.midiEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	}
}

func (d *Device) NoteOff(ev *input.InputEvent) {
	noteAndChannel, ok := d.noteTracker[ev.Event.Code]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	event := NoteEvent(NoteOff, channel, note, 0)
	d.midiEvents <- event
	delete(d.noteTracker, ev.Event.Code)
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
	}

	for _, offset := range d.multiNote {
		multiNote := int(note) + offset
		if multiNote < 0 || multiNote > 127 {
			continue
		}
		newNote := uint8(multiNote)
		event = NoteEvent(NoteOff, channel, newNote, 0)
		d.midiEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
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
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys)...)
	}
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
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys)...)
	}
}

func (d *Device) OctaveDown() {
	d.octave--
	if !d.noLogs {
		log.Info(fmt.Sprintf("octave down (%d)", d.octave), d.logFields(logger.Action)...)
	}
}

func (d *Device) OctaveUp() {
	d.octave++
	if !d.noLogs {
		log.Info(fmt.Sprintf("octave up (%d)", d.octave), d.logFields(logger.Action)...)
	}
}

func (d *Device) OctaveReset() {
	d.octave = 0
	if !d.noLogs {
		log.Info(fmt.Sprintf("octave reset (%d)", d.octave), d.logFields(logger.Action)...)
	}
}

func (d *Device) SemitoneDown() {
	d.semitone--
	if !d.noLogs {
		log.Info(fmt.Sprintf("semitone down (%d)", d.semitone), d.logFields(logger.Action)...)
	}
}

func (d *Device) SemitoneUp() {
	d.semitone++
	if !d.noLogs {
		log.Info(fmt.Sprintf("semitone up (%d)", d.semitone), d.logFields(logger.Action)...)
	}
}

func (d *Device) SemitoneReset() {
	d.semitone = 0
	if !d.noLogs {
		log.Info(fmt.Sprintf("semitone reset (%d)", d.semitone), d.logFields(logger.Action)...)
	}
}

func (d *Device) MappingDown() {
	if d.mapping != 0 {
		d.mapping--
	}
	if !d.noLogs {
		log.Info(fmt.Sprintf("mapping down (%s)", d.config.KeyMappings[d.mapping].Name), d.logFields(logger.Action)...)
	}
}

func (d *Device) MappingUp() {
	if d.mapping != len(d.config.KeyMappings)-1 {
		d.mapping++
	}
	if !d.noLogs {
		log.Info(fmt.Sprintf("mapping up (%s)", d.config.KeyMappings[d.mapping].Name), d.logFields(logger.Action)...)
	}
}

func (d *Device) MappingReset() {
	d.mapping = 0
	if !d.noLogs {
		log.Info(fmt.Sprintf("mapping reset (%s)", d.config.KeyMappings[d.mapping].Name), d.logFields(logger.Action)...)
	}
}

func (d *Device) ChannelDown() {
	if d.channel != 0 {
		d.channel--
	}
	if !d.noLogs {
		log.Info(fmt.Sprintf("channel down (%2d)", d.channel+1), d.logFields(logger.Action)...)
	}
}

func (d *Device) ChannelUp() {
	if d.channel != 15 {
		d.channel++
	}
	if !d.noLogs {
		log.Info(fmt.Sprintf("channel up (%2d)", d.channel+1), d.logFields(logger.Action)...)
	}
}

func (d *Device) ChannelReset() {
	d.channel = 0
	if !d.noLogs {
		log.Info(fmt.Sprintf("channel reset (%2d)", d.channel+1), d.logFields(logger.Action)...)
	}
}

func humanizeNoteOffsets(offsets []int) string {
	var intervals = ""
	for i, interval := range offsets {
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
	return intervals
}

func (d *Device) Multinote() {
	var pressedNotes []int
	for _, noteAndChannel := range d.noteTracker {
		pressedNotes = append(pressedNotes, int(noteAndChannel[0]))
	}

	if len(pressedNotes) == 0 {
		if !d.noLogs {
			log.Info("Bruh, no pressed notes, multinote mode disengaged", d.logFields(logger.Action)...)
		}
		d.multiNote = []int{}
		return
	}

	if len(pressedNotes) == 1 {
		if !d.noLogs {
			log.Info("Bruh, press more than one note, multinote mode disengaged", d.logFields(logger.Action)...)
		}
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

	intervals := humanizeNoteOffsets(noteOffsets)

	d.multiNote = noteOffsets
	if !d.noLogs {
		log.Info(fmt.Sprintf("Multinote mode engaged, intervals: %v/[%s]", d.multiNote, intervals), d.logFields(logger.Action)...)
	}
}

func (d *Device) Panic() {
	d.midiEvents <- ControlChangeEvent(d.channel, AllNotesOff, 0)

	// Some plugins may not respect AllNotesOff control change message, there is a simple workaround
	for note := uint8(0); note < 128; note++ {
		d.midiEvents <- NoteEvent(NoteOff, d.channel, note, 0)
	}
	if !d.noLogs {
		log.Info("Panic!", d.logFields(logger.Action)...)
	}
}

func (d *Device) CCLearningOn() {
	d.ccLearning = true
	if !d.noLogs {
		log.Info("CC learning mode enabled", d.logFields(logger.Action)...)
	}
}

func (d *Device) CCLearningOff() {
	d.ccLearning = false
	if !d.noLogs {
		log.Info("CC learning mode disabled", d.logFields(logger.Action)...)
	}
}

func (d *Device) Status() string {
	return fmt.Sprintf(
		"octave: %3d, semitone: %3d, channel: %2d, notes: %2d, map: %s, multinote: %s",
		d.octave,
		d.semitone,
		d.channel+1,
		len(d.noteTracker)+len(d.analogNoteTracker),
		d.config.KeyMappings[d.mapping].Name,
		humanizeNoteOffsets(d.multiNote),
	)
}

func DetectDevices() []IODevice {
	fd, err := os.Open("/dev/snd")
	if err != nil {
		panic(err)
	}
	entries, err := fd.ReadDir(0)
	if err != nil {
		panic(err)
	}

	var devices = make([]IODevice, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasPrefix(entry.Name(), "midi") {
			devices = append(devices, IODevice{path: fmt.Sprintf("/dev/snd/%s", entry.Name())})
		}
	}

	return devices
}

type IODevice struct {
	path string
}

func (d *IODevice) Open() (*os.File, error) {
	return os.OpenFile(d.path, os.O_RDWR|os.O_SYNC, 0)
}
