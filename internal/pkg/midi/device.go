package midi

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/holoplot/go-evdev"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/realbucksavage/openrgb-go"
	"go.uber.org/zap"
)

var log = logger.GetLogger()

const (
	EV_KEY_RELEASE = 0
	EV_KEY_PRESS   = 1
	EV_KEY_REPEAT  = 2
)

type multiNote []int

func (m multiNote) String() string {
	if len(m) == 0 {
		return "None"
	}

	var intervals = ""
	for i, interval := range m {
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

type Effect interface {
	InputChan() *chan Event
	SetOutput(target *chan Event)
	Enable(currentNotes []Event)
	Disable()
}

type MultiNote struct {
	inputMap     map[byte]byte // channel: note
	generatedMap map[byte]byte // channel: note
	input        chan Event
	output       *chan Event
	offsets      multiNote
}

func NewMultiNote() MultiNote {
	return MultiNote{
		inputMap:     make(map[byte]byte, 32),
		generatedMap: make(map[byte]byte, 32),
		input:        make(chan Event, 8),
		output:       nil,
		offsets:      multiNote{},
	}
}

func (m *MultiNote) process(currentNotes []Event) {
	for ev := range m.input {
		*m.output <- ev
	}
}
func (m *MultiNote) SetOutput(target *chan Event) {
	m.output = target
}

func (m *MultiNote) InputChan() *chan Event {
	return &m.input
}

func (m *MultiNote) Enable(currentNotes []Event) {
	go m.process(currentNotes)
}

func (m *MultiNote) Disable() {

}

type EffectManager struct {
	target       **chan Event
	effectEvents *chan Event
	outputEvents *chan Event

	MultiNote *MultiNote

	effects []Effect
}

func (m *EffectManager) Enable() {
	*m.target = m.effectEvents
}

func (m *EffectManager) Disable() {
	*m.target = m.outputEvents
}

func NewEffectManager(target **chan Event, effect, output *chan Event) EffectManager {
	var effects = make([]Effect, 0)

	multiNote := NewMultiNote()
	effects = append(effects, &multiNote)

	// chaining all effects together
	var prevInput = effect
	for _, e := range effects {
		e.SetOutput(prevInput)
		prevInput = e.InputChan()
	}

	return EffectManager{
		target:       target,
		effectEvents: effect,
		outputEvents: output,
		MultiNote:    &multiNote,
		effects:      effects,
	}
}

type Device struct {
	noLogs      bool // skips producing most of the log entries for maximum performance
	config      config.Config
	InputDevice input.Device

	effectEvents chan<- Event
	outputEvents chan<- Event
	target       *chan<- Event
	midiIn       <-chan Event

	inMutex sync.Mutex
	inMap   map[byte]map[byte]bool // channel: note

	effectManager EffectManager

	// instead of generating NoteOff events based on the current Device state (lazy approach), every emitted note
	// is being tracked and released precisely on related hardware button release.
	// This approach gives much nicer user experience as the User may conveniently hold some keys
	// and modify state on the fly (changing octave, channel etc.), NoteOff events will be emitted correctly anyway.
	noteTracker       map[evdev.EvCode][2]byte // 1: note, 2: channel
	analogNoteTracker map[string][2]byte       // 1: note, 2: channel
	// used to track active occurrence number for given channel/note for purpose of handling clashed notes.
	// more info in hidi.config at "collision_mode" option.
	activeNotesCounter map[byte]map[byte]int // map[channel]map[note]occurrence_number
	lastAnalogValue    map[evdev.EvCode]float64

	actionTracker map[config.Action]bool
	ccZeroed      map[byte]bool // 1: positive, 2: negative

	tmpMutex sync.Mutex

	octave   int8
	semitone int8
	channel  uint8
	velocity uint8
	// warning: currently lazy implementation
	multiNote  multiNote // list of additional note intervals (offsets)
	mapping    int
	ccLearning bool

	actionsPress   map[config.Action]func(*Device)
	actionsRelease map[config.Action]func(*Device)
}

func NewDevice(inputDevice input.Device, cfg config.DeviceConfig, midiEvents chan<- Event, midiIn <-chan Event, noLogs bool) Device {
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

	device := Device{
		noLogs:       noLogs,
		config:       cfg.Config,
		InputDevice:  inputDevice,
		outputEvents: midiEvents,
		effectEvents: make(chan Event, 8),
		target:       &midiEvents,
		midiIn:       midiIn,
		inMutex:      sync.Mutex{},
		inMap:        inmap,

		noteTracker:        make(map[evdev.EvCode][2]byte, 32),
		analogNoteTracker:  make(map[string][2]byte, 32),
		activeNotesCounter: activeNoteCounter,
		actionTracker:      make(map[config.Action]bool, 16),
		ccZeroed:           make(map[byte]bool, 32),
		lastAnalogValue:    make(map[evdev.EvCode]float64, 32),

		actionsPress: map[config.Action]func(*Device){
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
		},
		actionsRelease: map[config.Action]func(*Device){
			config.Learning: (*Device).CCLearningOff,
		},

		octave:     int8(cfg.Config.Defaults.Octave),
		semitone:   int8(cfg.Config.Defaults.Semitone),
		channel:    uint8(cfg.Config.Defaults.Channel - 1),
		multiNote:  multiNote{},
		mapping:    cfg.Config.Defaults.Mapping,
		ccLearning: false,
		velocity:   64,
	}

	effectManager := EffectManager{
		target:       nil,
		effectEvents: nil,
		outputEvents: nil,
		effects:      nil,
	}

	device.effectManager = effectManager

	return device
}

func (d *Device) EnableEffect() {

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
		d.tmpMutex.Lock()
		d.handleKEYEvent(event)
		d.tmpMutex.Unlock()
	case evdev.EV_ABS:
		d.tmpMutex.Lock()
		d.handleABSEvent(event)
		d.tmpMutex.Unlock()
	}
}

func (d *Device) handleInputEvents(wg *sync.WaitGroup, ctx context.Context) {
	log.Info(fmt.Sprintf("processing midi events"), d.logFields(logger.Debug)...)
	defer wg.Done()
root:
	for {
		select {
		case <-ctx.Done():
			break root
		case ev := <-d.midiIn:
			switch ev.Type() {
			case NoteOn:
				d.inMutex.Lock()
				d.inMap[ev.Channel()][ev.Note()] = true
				d.inMutex.Unlock()
			case NoteOff:
				d.inMutex.Lock()
				delete(d.inMap[ev.Channel()], ev.Note())
				d.inMutex.Unlock()
			}
		}
	}
	log.Info(fmt.Sprintf("processing midi events done"), d.logFields(logger.Debug)...)
}

func findController(c *openrgb.Client, name string) (openrgb.Device, int, error) {
	count, err := c.GetControllerCount()
	if err != nil {
		return openrgb.Device{}, 0, fmt.Errorf("failed to get controller count: %s", err)
	}

	if count == 0 {
		return openrgb.Device{}, 0, fmt.Errorf("no supported controllers available")
	}

	for i := 0; i < count; i++ {
		dev, err := c.GetDeviceController(i)
		if err != nil {
			return openrgb.Device{}, 0, fmt.Errorf("getting controller information failed (%d/%d): %s", i, count, err)
		}

		if dev.Type != 5 { // keyboard
			continue
		}

		if dev.Name == name {
			return dev, i, nil
		}
	}

	return openrgb.Device{}, 0, fmt.Errorf("controller \"%s\" not found", name)
}

func (d *Device) handleOpenrgb(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()

	host, port := "localhost", 6742

	log.Info(fmt.Sprintf("[OpenRGB] Connecting: %s:%d...", host, port), d.logFields(logger.Debug)...)

	var c *openrgb.Client
	var err error

	timeout := time.Now().Add(time.Second * 5)

	for {
		if time.Now().After(timeout) {
			log.Info("[OpenRGB] Connecting to server: Giving up", d.logFields(logger.Debug)...)
			return
		}

		c, err = openrgb.Connect(host, port)
		if err != nil {
			time.Sleep(time.Millisecond * 250)
			continue
		}
		break
	}

	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Cannot connect to server: %s", err), d.logFields(logger.Debug)...)
		return
	}

	log.Info(fmt.Sprintf("[OpenRGB] Connected, finding controller: \"%s\"...", d.config.OpenRGB.NameIdentifier), d.logFields(logger.Debug)...)

	var dev openrgb.Device
	var index int

	timeout = time.Now().Add(time.Second * 2)

	for {
		if time.Now().After(timeout) {
			log.Info("[OpenRGB] find controller: Giving up", d.logFields(logger.Debug)...)
			return
		}

		dev, index, err = findController(c, d.config.OpenRGB.NameIdentifier)
		if err != nil {
			time.Sleep(time.Millisecond * 250)
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
	someCounter := 0
root:
	for {
		select {
		case <-ctx.Done():
			break root
		default:
			break
		}
		time.Sleep(time.Millisecond * 100)

		d.tmpMutex.Lock()
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

		d.inMutex.Lock()
		for ch := 15; ch >= 0; ch-- {
			for note := range d.inMap[byte(ch)] {
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

		for note := range d.inMap[d.channel] {
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
		d.inMutex.Unlock()

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

		if d.config.OpenRGB.NameIdentifier == "HyperX Alloy Elite 2 (HP)" {
			// HSV animation on LED strip
			for i := 1; i < 19; i++ {
				id := nameToIndex[fmt.Sprintf("RGB Strip %d", i)]
				c := colorful.Hsv(float64(((i-1)*20+someCounter)%360), 1, 1)
				ledArray[id] = openrgb.Color{
					Red:   uint8(c.R * 255),
					Green: uint8(c.G * 255),
					Blue:  uint8(c.B * 255),
				}
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
		someCounter++
		if someCounter == 360 {
			someCounter = 0
		}
		d.tmpMutex.Unlock()
	}

	for i, _ := range ledArray {
		ledArray[i] = openrgb.Color{Red: 0xff}
	}
	c.UpdateLEDs(index, ledArray)
}

func (d *Device) ProcessEvents(wg *sync.WaitGroup, inputEvents <-chan *input.InputEvent) {
	defer wg.Done()
	log.Info("start ProcessEvents", d.logFields(logger.Debug)...)

	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go d.handleOpenrgb(wg, ctx)
	wg.Add(1)
	go d.handleInputEvents(wg, ctx)

	for ie := range inputEvents {
		d.processEvent(ie)
	}
	cancel()

	if len(d.noteTracker) > 0 || len(d.analogNoteTracker) > 0 {
		log.Info("active midi notes cleanup", d.logFields(logger.Debug)...)
	}

	for evcode := range d.noteTracker {
		d.NoteOff(&input.InputEvent{
			Source: input.DeviceInfo{Name: "shutdown cleanup"},
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
		if ie.Event.Type == evdev.EV_KEY && (ie.Event.Value == EV_KEY_RELEASE || ie.Event.Value == EV_KEY_REPEAT) {
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

	if !analogOk {
		if !d.noLogs {
			log.Info(
				fmt.Sprintf("Undefined ABS event: %s", ie.Event.String()),
				d.logFields(logger.Analog, zap.String("handler_event", ie.Source.Event()))...,
			)
		}
		return
	}

	// converting integer value to float and applying deadzone
	// -1.0 - 1.0 range if negative values are included, 0.0 - 1.0 otherwise
	var value float64
	var canBeNegative bool
	min := d.InputDevice.AbsInfos[ie.Source.Event()][ie.Event.Code].Minimum
	max := d.InputDevice.AbsInfos[ie.Source.Event()][ie.Event.Code].Maximum
	if min < 0 {
		canBeNegative = true
	}

	deadzone := d.config.AnalogDeadzones[ie.Event.Code]
	if ie.Event.Value < 0 {
		value = float64(ie.Event.Value) / math.Abs(float64(min))
		if value > -deadzone {
			value = 0
		} else {
			value = (value + deadzone) * (1.0 / (1.0 - deadzone))
		}
	} else {
		value = float64(ie.Event.Value) / math.Abs(float64(max))
		if value < deadzone {
			value = 0
		} else {
			value = (value - deadzone) * (1.0 / (1.0 - deadzone))
		}
	}

	// prevent from repeating value that was already sent before
	lastValue := d.lastAnalogValue[ie.Event.Code]
	if lastValue == value {
		return
	}
	d.lastAnalogValue[ie.Event.Code] = value

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

	// TODO: cleanup this mess
	switch analog.MappingType {
	case config.AnalogCC:
		var adjustedValue float64

		switch {
		case canBeNegative && analog.Bidirectional:
			adjustedValue = math.Abs(value)
			if value < 0 {
				d.outputEvents <- ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- ControlChangeEvent(d.channel, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case canBeNegative && !analog.Bidirectional:
			adjustedValue = (value + 1) / 2
			d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		case !canBeNegative && analog.Bidirectional:
			adjustedValue = math.Abs(value*2 - 1)
			if value < 0.5 {
				d.outputEvents <- ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- ControlChangeEvent(d.channel, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case !canBeNegative && !analog.Bidirectional:
			adjustedValue = value
			d.outputEvents <- ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		}
	case config.AnalogPitchBend:
		if canBeNegative {
			d.outputEvents <- PitchBendEvent(d.channel, value)
		} else {
			d.outputEvents <- PitchBendEvent(d.channel, value*2-1.0)
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

	var event Event
	switch d.config.CollisionMode {
	case config.CollisionOff, config.CollisionRetrigger:
		event = NoteEvent(NoteOn, d.channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs { // TODO: maybe move logging outside of device, but it will need InputEvent and Device reference tho
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	case config.CollisionNoRepeat:
		if d.activeNotesCounter[d.channel][note] > 0 {
			break
		}
		event = NoteEvent(NoteOn, d.channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	case config.CollisionInterrupt:
		if d.activeNotesCounter[d.channel][note] > 0 {
			event = NoteEvent(NoteOff, d.channel, note, 0)
			d.outputEvents <- event
			if !d.noLogs {
				log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
			}
		}

		event = NoteEvent(NoteOn, d.channel, note, d.velocity)
		d.outputEvents <- event
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	}

	d.noteTracker[ev.Event.Code] = [2]byte{note, d.channel}
	d.activeNotesCounter[d.channel][note]++
}

func (d *Device) NoteOff(ev *input.InputEvent) {
	noteAndChannel, ok := d.noteTracker[ev.Event.Code]
	if !ok {
		return
	}
	note, channel := noteAndChannel[0], noteAndChannel[1]

	var event Event
	switch d.config.CollisionMode {
	case config.CollisionOff:
		event = NoteEvent(NoteOff, channel, note, 0)
		d.outputEvents <- event
		delete(d.noteTracker, ev.Event.Code)
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	case config.CollisionNoRepeat, config.CollisionRetrigger, config.CollisionInterrupt:
		if d.activeNotesCounter[channel][note] != 1 {
			delete(d.noteTracker, ev.Event.Code)
			break
		}
		event = NoteEvent(NoteOff, channel, note, 0)
		d.outputEvents <- event
		delete(d.noteTracker, ev.Event.Code)
		if !d.noLogs {
			log.Info(event.String(), d.logFields(logger.Keys, zap.String("handler_event", ev.Source.Event()))...)
		}
	}

	d.activeNotesCounter[channel][note]--
}

func (d *Device) AnalogNoteOn(identifier string, note byte) { // TODO: multinote, collision handler
	noteCalculatored := int(note) + int(d.octave*12) + int(d.semitone)
	if noteCalculatored < 0 || noteCalculatored > 127 {
		return
	}
	note = uint8(noteCalculatored)

	d.analogNoteTracker[identifier] = [2]byte{note, d.channel}
	event := NoteEvent(NoteOn, d.channel, note, 64)
	d.outputEvents <- event
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
	d.outputEvents <- event
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
		log.Info(fmt.Sprintf("Multinote mode engaged, intervals: %v/[%s]", d.multiNote, d.multiNote.String()), d.logFields(logger.Action)...)
	}
}

func (d *Device) Panic() {
	d.outputEvents <- ControlChangeEvent(d.channel, AllNotesOff, 0)

	// Some plugins may not respect AllNotesOff control change message, there is a simple workaround
	for note := uint8(0); note < 128; note++ {
		d.outputEvents <- NoteEvent(NoteOff, d.channel, note, 0)
	}
	if !d.noLogs {
		log.Info("Panic!", d.logFields(logger.Action)...)
	}

	// resetting LEDs for external midi input as well
	d.inMutex.Lock()
	inmap := make(map[byte]map[byte]bool)
	for i := byte(0); i < 16; i++ {
		inmap[i] = make(map[byte]bool)
	}
	d.inMap = inmap
	d.inMutex.Unlock()
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
		d.multiNote.String(),
	)
}

type State struct {
	Octave    int8
	Semitone  int8
	Channel   uint8
	Notes     int
	MultiNote string
	Mapping   string
}

func (d *Device) State() State {
	return State{
		Octave:    d.octave,
		Semitone:  d.semitone,
		Channel:   d.channel,
		Notes:     len(d.noteTracker) + len(d.analogNoteTracker),
		MultiNote: d.multiNote.String(),
		Mapping:   d.config.KeyMappings[d.mapping].Name,
	}
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
