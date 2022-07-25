package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/midi/device"
)

var blocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
var heart, randomChar = '❤', '░'

func GenerateDisplayData(
	ctx context.Context, wg *sync.WaitGroup, cfg display.ScreenConfig,
	devices map[*device.Device]*device.Device, devicesMutex *sync.Mutex,
	midiEventCounter, score *uint,
) <-chan display.DisplayData {
	data := make(chan display.DisplayData)

	go func() {
		defer wg.Done()
		defer close(data)

		lastMidiEventsEmitted := *midiEventCounter

		var graph [20]uint

		var graphPointer int
		var min, x uint = 0, 1
		var counterMax = min - x
		var lastProcessingDuration time.Duration

		var buffer [4]string

	root:
		for {
			start := time.Now()

			var handlerCount int

			devicesMutex.Lock()
			var devCount = len(devices)

			for _, midiDev := range devices {
				handlerCount += len(midiDev.InputDevice.Handlers)
			}
			devicesMutex.Unlock()

			var eventsPerSecond uint

			if lastMidiEventsEmitted > *midiEventCounter {
				eventsPerSecond = (counterMax - lastMidiEventsEmitted) + *midiEventCounter // handling counter overflow
			} else {
				eventsPerSecond = *midiEventCounter - lastMidiEventsEmitted
			}
			lastMidiEventsEmitted = *midiEventCounter

			graph[graphPointer] = eventsPerSecond
			if graphPointer < 19 {
				graphPointer++
			} else {
				graphPointer = 0
			}

			buffer[0] = fmt.Sprintf("devices: %11d", devCount)
			buffer[1] = fmt.Sprintf("handlers: %10d", handlerCount)
			buffer[2] = fmt.Sprintf("events: %12d", eventsPerSecond)

			var maxGraph uint
			for _, graphVal := range graph {
				if graphVal > maxGraph {
					maxGraph = graphVal
				}
			}
			if maxGraph < 8 {
				maxGraph = 8
			}

			buffer[3] = ""
			for i := 0; i < 20; i++ {
				index := (graphPointer + i) % 20
				graphVal := graph[index]
				if graphVal == 0 {
					buffer[3] += " "
					continue
				}
				realVal := float64(graphVal) / (float64(maxGraph) + 1) * 7
				buffer[3] += string(blocks[int(realVal)])
			}
			lastProcessingDuration = time.Now().Sub(start)

			data <- display.DisplayData{
				Lines:   buffer,
				LastMsg: false,
			}

			select {
			case <-ctx.Done():
				break root
			case <-time.After((time.Duration(cfg.UpdateRate) * time.Second) - lastProcessingDuration):
				break
			}
		}

		if !cfg.HaveExitMessage() {
			buffer[0] = "                    "
			buffer[1] = " thanks for playing "
			buffer[2] = fmt.Sprintf("    %s with HIDI %s  ", string(randomChar), string(heart))
			msg := fmt.Sprintf("(score: %d)", *score)
			msgCenter := fmt.Sprintf("%*s", -20, fmt.Sprintf("%*s", (20+len(msg))/2, msg))
			buffer[3] = msgCenter

		} else {
			for i, msg := range cfg.ExitMessage {
				buffer[i] = msg[:20]
			}
		}

		data <- display.DisplayData{
			Lines:   buffer,
			LastMsg: true,
		}
	}()

	return data
}
