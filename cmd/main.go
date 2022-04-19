package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"hidi/internal/pkg/display"
	"hidi/internal/pkg/hidi"
	"hidi/internal/pkg/midi"
)

var midiEventsEmitted uint16 // counter for display info

//go:embed pony.txt
var pony string

func processMidiEvents(wg *sync.WaitGroup, ioDevice *os.File, midiEvents <-chan midi.Event) {
	wg.Add(1)
	defer wg.Done()
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
	done    chan bool
}

func NewBufferedLogWriter(size int) ChanneledLogger {
	return ChanneledLogger{
		channel: make(chan string, size),
		done:    make(chan bool),
	}
}

func (c *ChanneledLogger) Write(p []byte) (n int, err error) {
	c.channel <- string(p)
	return len(p), nil
}

func (c *ChanneledLogger) Close() {
	close(c.channel)
}

func (c *ChanneledLogger) Wait() {
	<-c.done
}

func (c *ChanneledLogger) ProcessLogs() {
	for entry := range c.channel {
		fmt.Printf("%s", entry)
	}
	close(c.done)
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
				fmt.Println("Dirty exit")
				os.Exit(1)
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

	wg := sync.WaitGroup{}

	log.Print("DEBUG: runnning processMidiEvents")
	go processMidiEvents(&wg, ioDevice, midiEvents)

	var devices = make(map[*midi.Device]*midi.Device, 16)
	log.Print("DEBUG: runnning HandleDisplays")
	go display.HandleDisplay(ctx, &wg, cfg, devices, &midiEventsEmitted)

	log.Print("DEBUG: running runManager")
	runManager(ctx, &wg, cfg, midiEvents, grab, devices)

	close(midiEvents)
	wg.Wait()
	time.Sleep(time.Millisecond * 200) // todo, pass context to every goroutin that may produce logs
	myLittleLogger.Close()
	myLittleLogger.Wait()
	fmt.Println(pony)
}
