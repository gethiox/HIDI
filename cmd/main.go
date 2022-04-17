package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"

	"hidi/internal/pkg/display"
	"hidi/internal/pkg/hidi"
	"hidi/internal/pkg/logg"
	"hidi/internal/pkg/midi"

	"github.com/d2r2/go-logger"

	"github.com/op/go-logging"
)

var midiEventsEmitted uint16 // counter for display info

func processMidiEvents(ioDevice *os.File, midiEvents <-chan midi.Event, logs chan<- logg.LogEntry) {
	for ev := range midiEvents {
		_, err := ioDevice.Write(ev)
		if err != nil {
			logs <- logg.Warning(fmt.Sprintf("failed to write midi event: %v", err))
			continue
		}
		midiEventsEmitted += 1
	}
}

// asynchronous log processing for latency sensitive tasks
func processLogs(logger logger.PackageLog, logs <-chan logg.LogEntry) {
	for l := range logs {
		switch l.Level {
		case logg.LevelDebug:
			logger.Debugf("> %s", l)
		case logg.LevelWarning:
			logger.Warningf("> %s", l)
		case logg.LevelInfo:
			logger.Infof("> %s", l)
		}
	}
}

func main() {
	go func() {
		fmt.Println(http.ListenAndServe("0.0.0.0:8080", nil))
	}()

	var grab, debug bool
	var midiDevice int

	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage, see README before use")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	l := logger.NewPackageLogger("main", logger.DebugLevel)

	if debug {
		logging.SetLevel(logging.DEBUG, "main")
	}

	cfg := hidi.LoadHIDIConfig("./config/hidi.config")
	l.Debugf("HIDI config: %+v", cfg)

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		l.Infof("There is no midi devices available, we're deeply sorry")
		os.Exit(1)
	}

	if len(ioDevices) < midiDevice+1 {
		l.Infof(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices available in total",
			midiDevice, len(ioDevices),
		)
		os.Exit(1)
	}

	ioDevice, err := ioDevices[midiDevice].Open()
	if err != nil {
		l.Infof("Failed to open MIDI device: %v", err)
		os.Exit(1)
	}

	var logs = make(chan logg.LogEntry, 128)
	var midiEvents = make(chan midi.Event)

	go processLogs(l, logs)
	go processMidiEvents(ioDevice, midiEvents, logs)

	var devices = make(map[*midi.Device]*midi.Device, 16)
	go display.HandleDisplay(cfg, devices, &midiEventsEmitted)

	runManager(cfg, logs, midiEvents, grab, devices)
	// TODO: graceful handle ctrl-c termination
}
