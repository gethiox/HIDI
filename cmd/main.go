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

	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
)

var midiEventsEmitted, score uint // counter for display info

//go:embed pony.txt
var pony string

func processMidiEvents(ctx context.Context, wg *sync.WaitGroup, ioDevice *os.File, midiEvents, otherMidiEvents <-chan midi.Event) {
	defer wg.Done()
	var ev midi.Event
root:
	for {
		select {
		case <-ctx.Done():
			break root
		case ev = <-midiEvents:
			if ev[0]&0b11110000 == midi.NoteOn {
				score += 1
			}
		case ev = <-otherMidiEvents:
			break
		}

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
	<-c.done
}

func (c *ChanneledLogger) ProcessLogs() {
	for entry := range c.channel {
		fmt.Printf("%s", entry)
	}
	close(c.done)
}

func monitorConfChanges(ctx context.Context, wg *sync.WaitGroup, c <-chan validate.NotifyMessage, events chan midi.Event) {
	defer wg.Done()
	for d := range c {
		p := NewPlayer(d.Data)
		p.Play(events, ctx, d.Bpm)
	}
}

func main() {
	var grab, debug, profile, noPony bool
	var midiDevice int

	flag.BoolVar(&profile, "profile", false, "runs internal web server for performance profiling")
	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage, see README before use")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&noPony, "nopony", false, "oh my... You can disable me if you want to, I.. I don't really mind. I'm fine")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	myLittleLogger := NewBufferedLogWriter(128)
	log.SetOutput(&myLittleLogger)
	go myLittleLogger.ProcessLogs()

	// this wait-group has to be propagated everywhere where usual logging appear
	wg := sync.WaitGroup{}

	var server *http.Server

	if profile {
		addr := "0.0.0.0:8080"
		log.Printf("profiling enabled and hosted on %s", addr)
		server = &http.Server{Addr: addr, Handler: nil}
		wg.Add(1)
		go func() {
			log.Printf("profiling server exited: %v", server.ListenAndServe())
			wg.Done()
		}()
	}

	var sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()
		var counter int
		for sig := range sigs {
			if counter > 0 {
				fmt.Println("Dirty exit")
				os.Exit(1)
			}
			log.Printf("siganl received: %v", sig)
			cancel()
			if server != nil {
				err := server.Close()
				if err != nil {
					log.Printf("failed to close server: %v", err)
				}
			}
			counter++
		}
	}()

	cfg := LoadHIDIConfig("./config/hidi.config")
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
	var otherMidiEvents = make(chan midi.Event)

	confNotifier := make(chan validate.NotifyMessage)
	wg.Add(1)
	go monitorConfChanges(ctx, &wg, confNotifier, otherMidiEvents)

	wg.Add(1)
	eventCtx, cancelEvents := context.WithCancel(context.Background())
	go processMidiEvents(eventCtx, &wg, ioDevice, midiEvents, otherMidiEvents)
	var devices = make(map[*midi.Device]*midi.Device, 16)
	wg.Add(1)
	go display.HandleDisplay(ctx, &wg, cfg.Screen, devices, &midiEventsEmitted, &score)

	runManager(ctx, cfg, midiEvents, grab, devices, confNotifier)

	cancelEvents()
	log.Printf("waiting...")
	// closing logger can be safely invoked only when all internally running goroutines (that may emit logs) are done
	close(confNotifier)
	close(sigs)
	close(otherMidiEvents)
	close(midiEvents)
	wg.Wait()
	myLittleLogger.Close()

	if !noPony {
		fmt.Printf(pony, score)
	}
}
