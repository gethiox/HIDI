package utils

import (
	"fmt"
	"math"
	"sync"
)

type DynamicFanOut[T any] struct {
	input    <-chan T
	inputCap int

	closed  bool
	mutex   sync.Mutex
	outputs map[int64]chan T
}

func NewDynamicFanOut[T any](input <-chan T) *DynamicFanOut[T] {
	f := DynamicFanOut[T]{
		input:    input,
		inputCap: cap(input),
		outputs:  make(map[int64]chan T),
		mutex:    sync.Mutex{},
	}
	go f.run()
	return &f
}

func (f *DynamicFanOut[T]) run() {
	for e := range f.input {
		f.mutex.Lock()
		for _, o := range f.outputs {
			o <- e
		}
		f.mutex.Unlock()
	}
	f.closed = true
}

// SpawnOutput creates new output channel and its ID for later despawning.
// Output channel size has size of input channel, output chanel will always be buffered with at least size 1.
func (f *DynamicFanOut[T]) SpawnOutput() (int64, <-chan T, error) {
	if f.closed {
		return 0, nil, fmt.Errorf("input channel is closed")
	}

	ocap := f.inputCap
	if ocap == 0 {
		ocap = 1
	}
	newChan := make(chan T, ocap)
	var id int64
	var found bool

	f.mutex.Lock()
	for id = 0; id < math.MaxInt64; id++ {
		_, ok := f.outputs[id]
		if !ok {
			found = true
			break
		}
	}
	if !found {
		return 0, nil, fmt.Errorf("no space available")
	}

	f.outputs[id] = newChan
	f.mutex.Unlock()
	return id, newChan, nil
}

// DespawnOutput removes output channel with given ID
func (f *DynamicFanOut[T]) DespawnOutput(id int64) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	c, ok := f.outputs[id]
	if !ok {
		return fmt.Errorf("output id %d not found", id)
	}
	close(c)
	delete(f.outputs, id)

	return nil
}
