package device

import (
	"errors"
	"fmt"
	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/device/config"
	"github.com/holoplot/go-evdev"
	"github.com/stretchr/testify/assert"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func getFactoryKeyboardConfiguration() (config.DeviceConfig, error) {
	data, err := os.ReadFile("../../../../cmd/hidi/hidi-config/factory/keyboard/0_default.toml")
	if err != nil {
		return config.DeviceConfig{}, fmt.Errorf("failed to read config file: %w", err)
	}

	c, err := config.ParseData(data)
	if err != nil {
		return config.DeviceConfig{}, fmt.Errorf("failed to parse config: %w", err)
	}

	return config.DeviceConfig{
		ConfigFile: "0_default.toml",
		ConfigType: "factory",
		Config:     c,
	}, nil
}

func readN(ch chan midi.Event, n int) ([]midi.Event, error) {
	events := make([]midi.Event, 0, n)

	count := 0
	for {
		select {
		case event := <-ch:
			events = append(events, event)
			count++
		case <-time.After(time.Millisecond * 10):
			if count != n {
				return events, errors.New(fmt.Sprintf("expected %d events, got %d", n, count))
			}
			return events, nil
		}
	}
}

func key(code evdev.EvCode, value int32) *input.InputEvent {
	return &input.InputEvent{
		Source: input.DeviceInfo{
			Name: "Dummy",
		},
		Event: evdev.InputEvent{
			Time:  syscall.Timeval{},
			Type:  evdev.EV_KEY,
			Code:  code,
			Value: value,
		},
	}
}

func TestFactoryConfigIntegration(t *testing.T) {
	cfg, err := getFactoryKeyboardConfiguration()
	assert.Equal(t, nil, err)

	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	// trigger two notes
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_X, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_X, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 4)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c0"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c0"), 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("d0"), 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("d0"), 0), events[3])

	// octave up
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)

	events, err = readN(midiEvents, 2)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c1"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c1"), 0), events[1])

	// two octaves up
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	// octave down
	kbdEvents <- key(evdev.KEY_F1, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F1, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)

	events, err = readN(midiEvents, 4)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c3"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c3"), 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c2"), 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c2"), 0), events[3])

	// octave reset
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F1, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F1, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)

	events, err = readN(midiEvents, 2)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c0"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c0"), 0), events[1])

	// 3 semitones up
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	// 1 semitones down
	kbdEvents <- key(evdev.KEY_F3, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F3, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	// semitone reset
	kbdEvents <- key(evdev.KEY_F3, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F3, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F4, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)

	events, err = readN(midiEvents, 6)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("d#0"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("d#0"), 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("d0"), 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("d0"), 0), events[3])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c0"), 64), events[4])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c0"), 0), events[5])

	// 3 channels up
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	// 1 channel down
	kbdEvents <- key(evdev.KEY_F5, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F5, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)
	// channel reset
	kbdEvents <- key(evdev.KEY_F5, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F5, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_F6, EV_KEY_RELEASE)
	// trigger one note
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_Z, EV_KEY_RELEASE)

	events, err = readN(midiEvents, 6)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 3, config.StringToNoteUnsafe("c0"), 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 3, config.StringToNoteUnsafe("c0"), 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 2, config.StringToNoteUnsafe("c0"), 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 2, config.StringToNoteUnsafe("c0"), 0), events[3])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, config.StringToNoteUnsafe("c0"), 64), events[4])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, config.StringToNoteUnsafe("c0"), 0), events[5])

	close(kbdEvents)
	wg.Wait()
	close(midiEvents)
}

func TestCollisionOff(t *testing.T) {
	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	cfg := config.DeviceConfig{
		ConfigFile: "/virtual",
		ConfigType: "factory",
		Config: config.Config{
			KeyMappings: []config.KeyMapping{
				{
					Name: "Default",
					Midi: map[evdev.EvCode]config.Key{
						evdev.KEY_A: {Note: 0, ChannelOffset: 0},
						evdev.KEY_B: {Note: 0, ChannelOffset: 0},
					},
					Analog: map[evdev.EvCode]config.Analog{},
				},
			},
			ActionMapping: map[evdev.EvCode]config.Action{},
			ExitSequence:  []evdev.EvCode{},
			CollisionMode: config.CollisionOff,
			Defaults: config.Defaults{
				Octave:   0,
				Semitone: 0,
				Channel:  1,
				Mapping:  0,
			},
		},
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 4)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[3])

	close(kbdEvents)
	wg.Wait()
	close(midiEvents)
}

func TestCollisionNoRepeat(t *testing.T) {
	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	cfg := config.DeviceConfig{
		ConfigFile: "/virtual",
		ConfigType: "factory",
		Config: config.Config{
			KeyMappings: []config.KeyMapping{
				{
					Name: "Default",
					Midi: map[evdev.EvCode]config.Key{
						evdev.KEY_A: {Note: 0, ChannelOffset: 0},
						evdev.KEY_B: {Note: 0, ChannelOffset: 0},
					},
					Analog: map[evdev.EvCode]config.Analog{},
				},
			},
			ActionMapping: map[evdev.EvCode]config.Action{},
			ExitSequence:  []evdev.EvCode{},
			CollisionMode: config.CollisionNoRepeat,
			Defaults: config.Defaults{
				Octave:   0,
				Semitone: 0,
				Channel:  1,
				Mapping:  0,
			},
		},
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 2)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[1])

	close(kbdEvents)
	wg.Wait()
	close(midiEvents)
}

func TestCollisionInterrupt(t *testing.T) {
	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	cfg := config.DeviceConfig{
		ConfigFile: "/virtual",
		ConfigType: "factory",
		Config: config.Config{
			KeyMappings: []config.KeyMapping{
				{
					Name: "Default",
					Midi: map[evdev.EvCode]config.Key{
						evdev.KEY_A: {Note: 0, ChannelOffset: 0},
						evdev.KEY_B: {Note: 0, ChannelOffset: 0},
					},
					Analog: map[evdev.EvCode]config.Analog{},
				},
			},
			ActionMapping: map[evdev.EvCode]config.Action{},
			ExitSequence:  []evdev.EvCode{},
			CollisionMode: config.CollisionInterrupt,
			Defaults: config.Defaults{
				Octave:   0,
				Semitone: 0,
				Channel:  1,
				Mapping:  0,
			},
		},
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 4)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[3])

	close(kbdEvents)
	wg.Wait()
	close(midiEvents)
}

func TestCollisionRetrigger(t *testing.T) {
	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	cfg := config.DeviceConfig{
		ConfigFile: "/virtual",
		ConfigType: "factory",
		Config: config.Config{
			KeyMappings: []config.KeyMapping{
				{
					Name: "Default",
					Midi: map[evdev.EvCode]config.Key{
						evdev.KEY_A: {Note: 0, ChannelOffset: 0},
						evdev.KEY_B: {Note: 0, ChannelOffset: 0},
					},
					Analog: map[evdev.EvCode]config.Analog{},
				},
			},
			ActionMapping: map[evdev.EvCode]config.Action{},
			ExitSequence:  []evdev.EvCode{},
			CollisionMode: config.CollisionRetrigger,
			Defaults: config.Defaults{
				Octave:   0,
				Semitone: 0,
				Channel:  1,
				Mapping:  0,
			},
		},
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 3)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[2])

	close(kbdEvents)
	wg.Wait()
	close(midiEvents)
}

func TestChannelOffset(t *testing.T) {
	inputDevice := input.Device{
		Name:       "Dummy",
		DeviceType: input.KeyboardDevice,
	}

	cfg := config.DeviceConfig{
		ConfigFile: "/virtual",
		ConfigType: "factory",
		Config: config.Config{
			KeyMappings: []config.KeyMapping{
				{
					Name: "Default",
					Midi: map[evdev.EvCode]config.Key{
						evdev.KEY_A: {Note: 0, ChannelOffset: 0},
						evdev.KEY_B: {Note: 0, ChannelOffset: 4},
					},
					Analog: map[evdev.EvCode]config.Analog{},
				},
			},
			ActionMapping: map[evdev.EvCode]config.Action{
				evdev.KEY_F1: config.ChannelDown,
				evdev.KEY_F2: config.ChannelUp,
			},
			ExitSequence:  []evdev.EvCode{},
			CollisionMode: config.CollisionOff,
			Defaults: config.Defaults{
				Octave:   0,
				Semitone: 0,
				Channel:  1,
				Mapping:  0,
			},
		},
	}

	kbdEvents := make(chan *input.InputEvent)
	midiEvents := make(chan midi.Event, 2560)

	d := NewDevice(inputDevice, cfg, midiEvents, nil, true, 0, nil, nil)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		d.ProcessEvents(kbdEvents)
		wg.Done()
	}()

	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)
	// channel uo
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_F2, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_A, EV_KEY_RELEASE)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_PRESS)
	kbdEvents <- key(evdev.KEY_B, EV_KEY_RELEASE)

	events, err := readN(midiEvents, 8)
	assert.Equal(t, nil, err)
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 0, 0, 64), events[0])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 0, 0, 0), events[1])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 4, 0, 64), events[2])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 4, 0, 0), events[3])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 1, 0, 64), events[4])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 1, 0, 0), events[5])
	assert.Equal(t, midi.NoteEvent(midi.NoteOn, 5, 0, 64), events[6])
	assert.Equal(t, midi.NoteEvent(midi.NoteOff, 5, 0, 0), events[7])

}
