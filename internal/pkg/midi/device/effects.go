package device

import (
	"fmt"

	"github.com/gethiox/HIDI/internal/pkg/midi"
)

type multiNote []int

func (m multiNote) String() string {
	if len(m) == 0 {
		return "None"
	}

	var intervals = ""
	for i, interval := range m {
		name, ok := midi.IntervalToString[interval]
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
	InputChan() *chan midi.Event
	SetOutput(target *chan midi.Event)
	Enable(currentNotes []midi.Event)
	Disable()
}

type MultiNote struct {
	inputMap     map[byte]byte // channel: note
	generatedMap map[byte]byte // channel: note
	input        chan midi.Event
	output       *chan midi.Event
	offsets      multiNote
}

func NewMultiNote() MultiNote {
	return MultiNote{
		inputMap:     make(map[byte]byte, 32),
		generatedMap: make(map[byte]byte, 32),
		input:        make(chan midi.Event, 8),
		output:       nil,
		offsets:      multiNote{},
	}
}

func (m *MultiNote) process(currentNotes []midi.Event) {
	for ev := range m.input {
		*m.output <- ev
	}
}
func (m *MultiNote) SetOutput(target *chan midi.Event) {
	m.output = target
}

func (m *MultiNote) InputChan() *chan midi.Event {
	return &m.input
}

func (m *MultiNote) Enable(currentNotes []midi.Event) {
	go m.process(currentNotes)
}

func (m *MultiNote) Disable() {

}

type EffectManager struct {
	target       **chan midi.Event
	effectEvents *chan midi.Event
	outputEvents *chan midi.Event

	MultiNote *MultiNote

	effects []Effect
}

func (m *EffectManager) Enable() {
	*m.target = m.effectEvents
}

func (m *EffectManager) Disable() {
	*m.target = m.outputEvents
}

func NewEffectManager(target **chan midi.Event, effect, output *chan midi.Event) EffectManager {
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

func (d *Device) EnableEffect() {

}
