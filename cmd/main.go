package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hidi/internal/pkg/display"
	"hidi/internal/pkg/hidi"
	"hidi/internal/pkg/midi"
)

var midiEventsEmitted uint16 // counter for display info

func processMidiEvents(ioDevice *os.File, midiEvents <-chan midi.Event) {
	for ev := range midiEvents {
		_, err := ioDevice.Write(ev)
		if err != nil {
			log.Printf("failed to write midi event: %v", err)
			continue
		}
		midiEventsEmitted += 1
	}
	log.Print("Processing midi events stopped")
}

type ChanneledLogger struct {
	channel chan string
}

func NewBufferedLogWriter(size int) ChanneledLogger {
	return ChanneledLogger{
		channel: make(chan string, size),
	}
}

func (c *ChanneledLogger) Write(p []byte) (n int, err error) {
	c.channel <- string(p)
	return len(p), nil
}

func (c *ChanneledLogger) Close() {
	close(c.channel)
}

func (c *ChanneledLogger) ProcessLogs() {
	for entry := range c.channel {
		fmt.Printf("%s", entry)
	}
	fmt.Println("Processing logs stopped")
}

func main() {
	var grab, debug, profile bool
	var midiDevice int

	flag.BoolVar(&profile, "profile", false, "runs internal web server for performance profiling")
	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage, see README before use")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	myLittleLogger := NewBufferedLogWriter(128)
	log.SetOutput(&myLittleLogger)
	go myLittleLogger.ProcessLogs()

	log.Printf("czo")

	if profile {
		addr := "0.0.0.0:8080"
		log.Printf("profiling enabled and hosted on %s", addr)
		go func() {
			log.Print(http.ListenAndServe(addr, nil))
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
			log.Printf("siganl received: %v", sig)
			cancel()
			counter++
		}
	}()

	cfg := hidi.LoadHIDIConfig("./config/hidi.config")
	log.Printf("HIDI config: %+v", cfg)

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		log.Print("There is no midi devices available, we're deeply sorry")
		os.Exit(1)
	}

	if len(ioDevices) < midiDevice+1 {
		log.Printf(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices available in total",
			midiDevice, len(ioDevices),
		)
		os.Exit(1)
	}

	ioDevice, err := ioDevices[midiDevice].Open()
	if err != nil {
		log.Printf("Failed to open MIDI device: %v", err)
		os.Exit(1)
	}

	var midiEvents = make(chan midi.Event)

	log.Print("DEBUG: runnning processMidiEvents")
	go processMidiEvents(ioDevice, midiEvents)

	var devices = make(map[*midi.Device]*midi.Device, 16)
	log.Print("DEBUG: runnning HandleDisplays")
	go display.HandleDisplay(ctx, cfg, devices, &midiEventsEmitted)

	log.Print("DEBUG: running runManager")
	runManager(ctx, cfg, midiEvents, grab, devices)

	close(midiEvents)
	time.Sleep(time.Second * 1)
	myLittleLogger.Close()
	time.Sleep(time.Second * 1)
	fmt.Printf("exited\n")
}
