package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"hidi/internal/pkg/input"
	"hidi/internal/pkg/logg"
	"hidi/internal/pkg/midi"

	"github.com/holoplot/go-evdev"
)

func processMidiEvents(ioDevice *os.File, midiEvents <-chan midi.Event, logs chan<- logg.LogEntry) {
	for ev := range midiEvents {
		_, err := ioDevice.Write(ev)
		if err != nil {
			logs <- logg.Warning(fmt.Sprintf("failed to write midi event: %v", err))
			continue
		}
	}
}

func processLogs(debug bool, logs <-chan logg.LogEntry) {
	for l := range logs {
		if !debug && l.Level == logg.LevelDebug {
			continue
		}
		log.Printf("> %s", l)
	}
}

func main() {
	var grab, debug bool
	var midiDevice int

	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage, see README before use")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		log.Print("There is no midi devices available, we're deeply sorry")
		os.Exit(1)
	}

	if len(ioDevices) < midiDevice+1 {
		log.Printf(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices avaialbe in total",
			midiDevice, len(ioDevices),
		)
		os.Exit(1)
	}

	ioDevice, err := ioDevices[midiDevice].Open()
	if err != nil {
		log.Printf("Failed to open MIDI device: %v", err)
		os.Exit(1)
	}

	var logs = make(chan logg.LogEntry, 128)
	var midiEvents = make(chan midi.Event)

	go processLogs(debug, logs)
	go processMidiEvents(ioDevice, midiEvents, logs)

device:
	for d := range input.MonitorNewDevices() {
		var inputEvents <-chan *evdev.InputEvent
		var err error

		appearedAt := time.Now()

		for {
			inputEvents, err = d.Open(grab)
			if err != nil {
				if time.Now().Sub(appearedAt) > time.Second*5 {
					logs <- logg.Warning(fmt.Sprintf("failed to open \"%s\" device on time, giving up", d.Name))
					continue device
				}
				time.Sleep(time.Millisecond * 100)
				continue
			}
			break
		}

		midiDev := midi.NewDevice(d, inputEvents, midiEvents, logs)
		go midiDev.ProcessEvents()
	}
	// TODO: graceful handle ctrl-c termination
}
