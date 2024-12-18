package device

import (
	"context"
	"fmt"
	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/device/config"
	"github.com/holoplot/go-evdev"
	"go.uber.org/zap"
	"math"
	"sync"
	"syscall"
)

func (d *Device) checkExitSequence() bool {
	if len(d.config.ExitSequence) == 0 {
		return false
	}

	for _, key := range d.config.ExitSequence {
		if _, ok := d.keyTracker[key]; !ok {
			return false
		}
	}
	d.sigs <- syscall.SIGINT
	return true
}

func (d *Device) handleKEYEvent(ie *input.InputEvent) {
	_, noteOk := d.config.KeyMappings[d.mapping].Midi[ie.Source.Name][ie.Event.Code]
	action, actionOk := d.config.ActionMapping[ie.Event.Code]

	if ie.Event.Value == EV_KEY_PRESS {
		d.keyTracker[ie.Event.Code] = struct{}{}
		ok := d.checkExitSequence()
		if ok {
			// TODO: handleKEYEvent hangs on sequence like alt+esc which involves panic sequence
			// this simple hack prevents from hanging
			return
		}
	} else {
		delete(d.keyTracker, ie.Event.Code)
	}

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
		// workaround for the case where keyboard mapping has ben changed while some key related to midi note
		// is still active and new mapping doesn't point to any note, therefore noteOk was evaluated to false
		if ie.Event.Value == EV_KEY_RELEASE {
			_, ok := d.noteTracker[ie.Event.Code]
			if ok {
				d.NoteOff(ie)
				break
			}
		}

		if ie.Event.Type == evdev.EV_KEY && (ie.Event.Value == EV_KEY_RELEASE || ie.Event.Value == EV_KEY_REPEAT) {
			break
		}

		if !d.noLogs {
			log.Info(fmt.Sprintf("Undefined KEY event: %s", ie.Event.String()), d.logFields(
				logger.KeysNotAssigned,
				zap.String("handler_event", ie.Source.DeviceInfo.Event()),
				zap.String("handler_subhandler", ie.Source.Name),
			)...)
		}
	}
}

func (d *Device) handleABSEvent(ie *input.InputEvent) {
	analog, analogOk := d.config.KeyMappings[d.mapping].Analog[ie.Source.Name][ie.Event.Code]

	if !analogOk {
		if !d.noLogs {
			log.Info(fmt.Sprintf("Undefined ABS event: %s", ie.Event.String()), d.logFields(
				logger.Analog,
				zap.String("handler_event", ie.Source.DeviceInfo.Event()),
				zap.String("handler_subhandler", ie.Source.Name),
			)...)
		}
		return
	}

	// converting integer value to float and applying deadzone
	// -1.0 - 1.0 range if negative values are included, 0.0 - 1.0 otherwise
	var value float64
	var canBeNegative bool
	absInfo := d.InputDevice.AbsInfos[ie.Source.DeviceInfo.Event()][ie.Event.Code]
	min := absInfo.Minimum
	max := absInfo.Maximum

	if min < 0 {
		canBeNegative = true
	}

	// Normalize Value
	if ie.Event.Value < 0 {
		value = float64(ie.Event.Value) / math.Abs(float64(min))
	} else {
		value = float64(ie.Event.Value) / math.Abs(float64(max))
	}

	// Put it always between -1.0 and 1.0 so we can deadzone the center
	if analog.DeadzoneAtCenter {
		value = value*2 - 1.0
		canBeNegative = true
	}

	deadzone, ok := d.config.KeyMappings[d.mapping].Deadzones[ie.Source.Name][ie.Event.Code]
	if !ok {
		deadzone, ok = d.config.KeyMappings[d.mapping].DefaultDeadzone[ie.Source.Name]
		if !ok {
			deadzone, ok = d.config.KeyMappings[d.mapping].DefaultDeadzone[""]
			if !ok {
				panic("tee hee")
			}
		}
	}

	if value < 0 {
		if value > -deadzone {
			value = 0
		} else {
			value = (value + deadzone) * (1.0 / (1.0 - deadzone))
		}
	} else {
		if value < deadzone {
			value = 0
		} else {
			value = (value - deadzone) * (1.0 / (1.0 - deadzone))
		}
	}

	// prevent from repeating value that was already sent before
	lastValue := d.lastAnalogValue[ie.Source.Name][ie.Event.Code]
	if lastValue == value {
		return
	}
	d.lastAnalogValue[ie.Source.Name][ie.Event.Code] = value

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
		log.Info(fmt.Sprintf("Analog event: %s", ie.Event.String()), d.logFields(
			logger.Analog,
			zap.String("handler_event", ie.Source.DeviceInfo.Event()),
			zap.String("handler_subhandler", ie.Source.Name),
		)...)
	}

	// TODO: cleanup this mess
	switch analog.MappingType {
	case config.AnalogCC:
		var adjustedValue float64

		channel := (d.channel + analog.ChannelOffset) % 16
		channelNeg := (d.channel + analog.ChannelOffsetNeg) % 16

		switch {
		case canBeNegative && analog.Bidirectional:
			adjustedValue = math.Abs(value)
			if value < 0 {
				d.outputEvents <- midi.ControlChangeEvent(channelNeg, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- midi.ControlChangeEvent(channelNeg, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case canBeNegative && !analog.Bidirectional:
			adjustedValue = (value + 1) / 2
			d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		case !canBeNegative && analog.Bidirectional:
			adjustedValue = math.Abs(value*2 - 1)
			if value < 0.5 {
				d.outputEvents <- midi.ControlChangeEvent(channelNeg, analog.CCNeg, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CC] {
					d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, 0)
					d.ccZeroed[analog.CC] = true
				}
				d.ccZeroed[analog.CCNeg] = false
			} else {
				d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, byte(int(float64(127)*adjustedValue)))
				if !d.ccZeroed[analog.CCNeg] {
					d.outputEvents <- midi.ControlChangeEvent(channelNeg, analog.CCNeg, 0)
					d.ccZeroed[analog.CCNeg] = true
				}
				d.ccZeroed[analog.CC] = false
			}
		case !canBeNegative && !analog.Bidirectional:
			adjustedValue = value
			d.outputEvents <- midi.ControlChangeEvent(channel, analog.CC, byte(int(float64(127)*adjustedValue)))
		}
	case config.AnalogPitchBend:
		channel := (d.channel + analog.ChannelOffset) % 16
		if canBeNegative {
			d.outputEvents <- midi.PitchBendEvent(channel, value)
		} else {
			d.outputEvents <- midi.PitchBendEvent(channel, value*2-1.0)
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
				d.AnalogNoteOn(identifierNeg, analog.NoteNeg, analog.ChannelOffsetNeg, ie)
			}
			d.AnalogNoteOff(identifier, ie)
		case value > -0.49 && value < 0.49:
			d.AnalogNoteOff(identifier, ie)
			d.AnalogNoteOff(identifierNeg, ie)
		case value >= 0.5:
			_, ok := d.analogNoteTracker[identifier]
			if !ok {
				d.AnalogNoteOn(identifier, analog.Note, analog.ChannelOffset, ie)
			}
			d.AnalogNoteOff(identifierNeg, ie)
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
		log.Info(fmt.Sprintf("unexpected AnalogID type: %+v", analog.MappingType), d.logFields(
			logger.Warning,
			zap.String("handler_event", ie.Source.DeviceInfo.Event()),
			zap.String("handler_subhandler", ie.Source.Name),
		)...)
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

func (d *Device) ProcessEvents(inputEvents <-chan *input.InputEvent) {
	wg := sync.WaitGroup{}

	log.Info("start ProcessEvents", d.logFields(logger.Debug)...)

	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(2)
	go d.handleOpenrgb(ctx, &wg)
	go d.handleInputEvents(ctx, &wg)

	for ie := range inputEvents {
		d.processEvent(ie)
	}
	cancel()
	log.Info("input events closed", d.logFields(logger.Debug)...)

	if len(d.noteTracker) > 0 || len(d.analogNoteTracker) > 0 {
		log.Info("active midi notes cleanup", d.logFields(logger.Debug)...)
	}

	for evcode := range d.noteTracker {
		d.NoteOff(&input.InputEvent{
			Source: input.Handler{
				Name:       "",
				DeviceInfo: input.DeviceInfo{Name: "shutdown cleanup"},
			},
			Event: evdev.InputEvent{
				Time:  syscall.Timeval{},
				Type:  evdev.EV_KEY,
				Code:  evcode,
				Value: 0,
			},
		})
	}
	for identifier := range d.analogNoteTracker {
		d.AnalogNoteOff(identifier, &input.InputEvent{})
	}

	log.Info("virtual midi device waiting...", d.logFields(logger.Debug)...)
	wg.Wait()
	log.Info("virtual midi device exited", d.logFields(logger.Debug)...)
}

func (d *Device) handleInputEvents(ctx context.Context, wg *sync.WaitGroup) {
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
