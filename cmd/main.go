package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	logs <- logg.Warning("Processing midi events stopped")
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
	logger.Infof("Processing logs stopped")
}

func main() {
	var grab, debug, profile bool
	var midiDevice int

	flag.BoolVar(&profile, "profile", false, "runs internal web server for performance profiling")
	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage, see README before use")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	if profile {
		addr := "0.0.0.0:8080"
		fmt.Printf("profiling enabled and hosted on %s\n", addr)
		go func() {
			fmt.Println(http.ListenAndServe(addr, nil))
		}()
	}

	var sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		var counter int
		for sig := range sigs {
			if counter > 0 {
				panic("force panic")
			}
			fmt.Printf("siganl received: %v\n", sig)
			cancel()
			counter++
		}
	}()

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

	fmt.Printf("DEBUG: runnning processLogs\n")
	go processLogs(l, logs)
	fmt.Printf("DEBUG: runnning processMidiEvents\n")
	go processMidiEvents(ioDevice, midiEvents, logs)

	var devices = make(map[*midi.Device]*midi.Device, 16)
	fmt.Printf("DEBUG: runnning HandleDisplays\n")
	go display.HandleDisplay(ctx, cfg, devices, &midiEventsEmitted)

	fmt.Printf("DEBUG: running runManager\n")
	runManager(ctx, cfg, logs, midiEvents, grab, devices)

	close(midiEvents)
	time.Sleep(time.Second * 1)
	close(logs)
	time.Sleep(time.Second * 1)
	fmt.Printf("exited\n")
}
