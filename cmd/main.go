package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"pi-midi-keyboard/internal/pkg/input"
	"pi-midi-keyboard/internal/pkg/midi"

	"github.com/holoplot/go-evdev"
)

func processMidiEvents(ioDevice *os.File, me <-chan midi.Event) {
	for ev := range me {
		_, err := ioDevice.Write(ev)
		if err != nil {
			log.Printf("failed to write midi event: %s", err)
			continue
		}
	}
}

func main() {
	var debug bool
	var midiDevice int
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	var midiEvents = make(chan midi.Event)
	var logs = make(chan string, 128)

	go func() {
		if debug {
			for l := range logs {
				log.Printf("> %s", l)
			}
		} else {
			for range logs {
				// just consume logs
			}
		}
	}()

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		panic("there is no midi devices, we're deeply sorry")
	}

	if len(ioDevices) < midiDevice+1 {
		panic(fmt.Sprintf("midi device with \"%d\" ID not exist. there is %d midi devices avaialbe",
			midiDevice, len(ioDevices)))
	}

	ioDevice, err := ioDevices[midiDevice].Open()
	if err != nil {
		panic(err)
	}

	go processMidiEvents(ioDevice, midiEvents)

	wg := sync.WaitGroup{}

device:
	for d := range input.MonitorNewDevices() {
		wg.Add(1)

		var inputEvents <-chan *evdev.InputEvent
		var err error

		open := time.Now()

		for {
			inputEvents, err = d.Open()
			if err != nil {
				if time.Now().Sub(open) > time.Second*2 {
					log.Print("failed to open device on time, giving up")
					continue device
				}
				time.Sleep(time.Millisecond * 100)
				continue
			}
			break
		}

		midiDev := midi.NewDevice(d, inputEvents, midiEvents, logs)
		go func() {
			midiDev.ProcessEvents()
			wg.Done()
		}()
	}

	wg.Wait()
}
