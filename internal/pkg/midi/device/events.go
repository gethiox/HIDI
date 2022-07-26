package device

import (
	"context"
	"fmt"
	"math"
	"sync"
	"syscall"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/holoplot/go-evdev"
	"go.uber.org/zap"
)

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
				d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case canBeNegative && !analog.Bidirectional:
			adjustedValue = (value + 1) / 2
			d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		case !canBeNegative && analog.Bidirectional:
			adjustedValue = math.Abs(value*2 - 1)
			if value < 0.5 {
				d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case !canBeNegative && !analog.Bidirectional:
			adjustedValue = value
			d.outputEvents <- midi.ControlChangeEvent(d.channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		}
	case config.AnalogPitchBend:
		if canBeNegative {
			d.outputEvents <- midi.PitchBendEvent(d.channel, value)
		} else {
			d.outputEvents <- midi.PitchBendEvent(d.channel, value*2-1.0)
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

func (d *Device) processEvent(event *input.InputEvent) {
	if event.Event.Type == evdev.EV_SYN {
		return
	}

	if event.Event.Type == evdev.EV_KEY && event.Event.Value == EV_KEY_REPEAT {
		return
	}

	switch event.Event.Type {
	case evdev.EV_KEY:
		d.eventProcessMutex.Lock()
		d.handleKEYEvent(event)
		d.eventProcessMutex.Unlock()
	case evdev.EV_ABS:
		d.eventProcessMutex.Lock()
		d.handleABSEvent(event)
		d.eventProcessMutex.Unlock()
	}
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
			case midi.NoteOn:
				d.externalTrackerMutex.Lock()
				d.externalNoteTracker[ev.Channel()][ev.Note()] = true
				d.externalTrackerMutex.Unlock()
			case midi.NoteOff:
				d.externalTrackerMutex.Lock()
				delete(d.externalNoteTracker[ev.Channel()], ev.Note())
				d.externalTrackerMutex.Unlock()
			}
		}
	}
	log.Info(fmt.Sprintf("processing midi events done"), d.logFields(logger.Debug)...)
}
