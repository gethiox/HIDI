package device

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/device/config"
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
	openrgbPort int

	effectEvents chan<- midi.Event
	outputEvents chan<- midi.Event
	target       *chan<- midi.Event
	midiIn       <-chan midi.Event

	// keeps track of currently active notes on midi input
	externalNoteTracker  map[byte]map[byte]bool // channel: note
	externalTrackerMutex *sync.Mutex

	// instead of generating NoteOff events based on the current Device state (lazy approach), every emitted note
	// is being tracked and released precisely on related hardware button release.
	// This approach gives much nicer user experience as the User may conveniently hold some keys
	// and modify state on the fly (changing octave, channel etc.), NoteOff events will be emitted correctly anyway.
	noteTracker       map[evdev.EvCode][2]byte // 1: note, 2: channel
	analogNoteTracker map[string][2]byte       // 1: note, 2: channel
	// used to track active occurrence number for given channel/note for purpose of handling clashed notes.
	// more info in hidi.toml at "collision_mode" option.
	activeNotesCounter map[byte]map[byte]int // map[channel]map[note]occurrence_number
	lastAnalogValue    map[string]map[evdev.EvCode]float64

	actionTracker map[config.Action]bool
	ccZeroed      map[byte]bool // 1: positive, 2: negative
	keyTracker    map[evdev.EvCode]struct{}
	sigs          chan os.Signal

	eventProcessMutex *sync.Mutex

	octave   int8
	semitone int8
	channel  uint8
	velocity uint8
	// warning: currently lazy implementation
	multiNote  []int // list of additional note intervals (offsets)
	mapping    int
	ccLearning bool

	actionsPress   map[config.Action]func(*Device)
	actionsRelease map[config.Action]func(*Device)
}

func NewDevice(
	inputDevice input.Device, cfg config.DeviceConfig,
	midiEvents chan<- midi.Event, midiIn <-chan midi.Event,
	noLogs bool, openrgbPort int,
	sigs chan os.Signal,
) Device {
	var activeNoteCounter = make(map[byte]map[byte]int)
	for ch := byte(0); ch < 16; ch++ {
		var t = make(map[byte]int)
		for note := byte(0); note < 128; note++ {
			t[note] = 0
		}
		activeNoteCounter[ch] = t
	}

	inmap := make(map[byte]map[byte]bool)
	for i := byte(0); i < 16; i++ {
		inmap[i] = make(map[byte]bool)
	}

	var subhandlers = make(map[string]interface{})
	for _, mapping := range cfg.Config.KeyMappings {
		for subhandler := range mapping.Analog {
			subhandlers[subhandler] = true
		}
	}

	var lastAnalogValue = make(map[string]map[evdev.EvCode]float64)
	for subhandler := range subhandlers {
		lastAnalogValue[subhandler] = make(map[evdev.EvCode]float64)
	}

	actionsPress := map[config.Action]func(*Device){
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
	actionsRelease := map[config.Action]func(*Device){
		config.Learning: (*Device).CCLearningOff,
	}

	device := Device{
		noLogs:               noLogs,
		config:               cfg.Config,
		InputDevice:          inputDevice,
		outputEvents:         midiEvents,
		effectEvents:         make(chan midi.Event, 8),
		target:               &midiEvents,
		midiIn:               midiIn,
		sigs:                 sigs,
		eventProcessMutex:    &sync.Mutex{},
		externalTrackerMutex: &sync.Mutex{},
		externalNoteTracker:  inmap,
		openrgbPort:          openrgbPort,

		noteTracker:        make(map[evdev.EvCode][2]byte, 32),
		keyTracker:         make(map[evdev.EvCode]struct{}, 32),
		analogNoteTracker:  make(map[string][2]byte, 32),
		activeNotesCounter: activeNoteCounter,
		actionTracker:      make(map[config.Action]bool, 16),
		ccZeroed:           make(map[byte]bool, 32),
		lastAnalogValue:    lastAnalogValue,

		actionsPress:   actionsPress,
		actionsRelease: actionsRelease,

		octave:     int8(cfg.Config.Defaults.Octave),
		semitone:   int8(cfg.Config.Defaults.Semitone),
		channel:    uint8(cfg.Config.Defaults.Channel - 1),
		multiNote:  []int{},
		mapping:    cfg.Config.Defaults.Mapping,
		ccLearning: false,
		velocity:   uint8(cfg.Config.Defaults.Velocity),
	}

	return device
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

func (d *Device) NoteOn(ev *input.InputEvent) {
	key, ok := d.config.KeyMappings[d.mapping].Midi[ev.Source.Name][ev.Event.Code]
	if !ok {
		return
	}
	note := key.Note
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)
	channel := (d.channel + key.ChannelOffset) % 16

	var event midi.Event
	switch d.config.CollisionMode {
	case config.CollisionOff, config.CollisionRetrigger:
		event = midi.NoteEvent(midi.NoteOn, channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs { // TODO: maybe move logging outside of device, but it will need InputEvent and Device reference tho
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
		}
	case config.CollisionNoRepeat:
		if d.activeNotesCounter[channel][note] > 0 {
			break
		}
		event = midi.NoteEvent(midi.NoteOn, channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
		}
	case config.CollisionInterrupt:
		if d.activeNotesCounter[channel][note] > 0 {
			event = midi.NoteEvent(midi.NoteOff, channel, note, 0)
			d.outputEvents <- event
			if !d.noLogs {
				log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
			}
		}

		event = midi.NoteEvent(midi.NoteOn, channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
		}
	default:
		panic("unsupported collision mode")
	}

	d.noteTracker[ev.Event.Code] = [2]byte{note, channel}
	d.activeNotesCounter[channel][note]++
}

func (d *Device) NoteOff(ev *input.InputEvent) {
	noteAndChannel, ok := d.noteTracker[ev.Event.Code]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	var event midi.Event
	switch d.config.CollisionMode {
	case config.CollisionOff:
		event = midi.NoteEvent(midi.NoteOff, channel, note, 0)
		d.outputEvents <- event
		delete(d.noteTracker, ev.Event.Code)
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
		}
	case config.CollisionNoRepeat, config.CollisionRetrigger, config.CollisionInterrupt:
		if d.activeNotesCounter[channel][note] != 1 {
			delete(d.noteTracker, ev.Event.Code)
			break
		}
		event = midi.NoteEvent(midi.NoteOff, channel, note, 0)
		d.outputEvents <- event
		delete(d.noteTracker, ev.Event.Code)
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
		}
	}

	d.activeNotesCounter[channel][note]--
}

func (d *Device) AnalogNoteOn(identifier string, note byte, channelOffset byte, ev *input.InputEvent) { // TODO: multinote, collision handler
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	channel := (d.channel + channelOffset) % 16

	d.analogNoteTracker[identifier] = [2]byte{note, channel}
	event := midi.NoteEvent(midi.NoteOn, channel, note, 64)
	d.outputEvents <- event
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
	}
}

func (d *Device) AnalogNoteOff(identifier string, ev *input.InputEvent) {
	noteAndChannel, ok := d.analogNoteTracker[identifier]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	event := midi.NoteEvent(midi.NoteOff, channel, note, 0)
	d.outputEvents <- event
	delete(d.analogNoteTracker, identifier)
	if !d.noLogs {
		log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.DeviceInfo.Event()))...)
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

	d.multiNote = noteOffsets
	if !d.noLogs {
		log.Info(fmt.Sprintf("Multinote mode engaged, intervals: %v", d.multiNote), d.logFields(logger.Action)...)
	}
}

func (d *Device) Panic() {
	d.outputEvents <- midi.ControlChangeEvent(d.channel, midi.AllNotesOff, 0)

	// Some plugins may not respect AllNotesOff control change message, there is a simple workaround
	for note := uint8(0); note < 128; note++ {
		d.outputEvents <- midi.NoteEvent(midi.NoteOff, d.channel, note, 0)
	}
	if !d.noLogs {
		log.Info("Panic!", d.logFields(logger.Action)...)
	}

	// resetting LEDs for external midi input as well
	d.externalTrackerMutex.Lock()
	inmap := make(map[byte]map[byte]bool)
	for i := byte(0); i < 16; i++ {
		inmap[i] = make(map[byte]bool)
	}
	d.externalNoteTracker = inmap
	d.externalTrackerMutex.Unlock()
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
		"octave: %3d, semitone: %3d, channel: %2d, notes: %2d, map: %s",
		d.octave,
		d.semitone,
		d.channel+1,
		len(d.noteTracker)+len(d.analogNoteTracker),
		d.config.KeyMappings[d.mapping].Name,
	)
}

type State struct {
	Octave   int8
	Semitone int8
	Channel  uint8
	Notes    int
	Mapping  string
}

func (d *Device) State() State {
	return State{
		Octave:   d.octave,
		Semitone: d.semitone,
		Channel:  d.channel,
		Notes:    len(d.noteTracker) + len(d.analogNoteTracker),
		Mapping:  d.config.KeyMappings[d.mapping].Name,
	}
}
