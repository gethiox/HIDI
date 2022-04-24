package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/cli"
	"github.com/gethiox/HIDI/internal/pkg/display"
	log2 "github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
	"github.com/jroimartin/gocui"
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
			log.Info(fmt.Sprintf("failed to write midi event: %v", err))
			continue
		}
		midiEventsEmitted += 1
	}
	log.Info("Processing midi events stopped")
}

func main() {
	var grab, profile, noPony bool
	var midiDevice, debug int

	flag.BoolVar(&profile, "profile", false, "runs web server for performance profiling (go tool pprof)")
	flag.BoolVar(&grab, "grab", false, "grab input devices for exclusive usage")
	flag.IntVar(&debug, "debug", 0,
		"logging level, each level enables additional information class (0-4, default: 0)\n"+
			"more verbose levels may slightly impact overall performance, try to not go beyond 3 when not necessary\n"+
			"\navailable options:\n"+
			"0: standard (general device appearance status, warnings, errors)\n"+
			"1: action events (octave_up, channel_down etc.)\n"+
			"2: key events (keyboard keys and gamepad buttons)\n"+
			"3: unassigned key events (keyboard keys and gamepad buttons not assigned to current mapping configuration)\n"+
			"4: analog assigned and unassigned events",
	)
	flag.BoolVar(&noPony, "nopony", false, "oh my... You can disable me if you want to, I.. I don't really mind. I'm fine")
	flag.IntVar(&midiDevice, "mididevice", 0, "select N-th midi device, default: 0 (first)")
	flag.Parse()

	g, err := cli.GetCli()
	if err != nil {
		panic(err)
	}

	go func() {
		defer g.Close()
		if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
			panic(err)
		}
	}()

	go func() {
		for {
			g.Update(cli.Layout)
			time.Sleep(time.Millisecond * 10)
		}
	}()

	time.Sleep(time.Millisecond * 500) // waiting for view init TODO: fix
	f, err := cli.NewFeeder(g, cli.ViewLogs)
	if err != nil {
		panic(err)
	}

	createConfigDirectory()

	// this wait-group has to be propagated everywhere where usual logging appear
	wg := sync.WaitGroup{}

	go func() {
		for msg := range log2.Messages {
			f.Write(msg)
		}
	}()

	var server *http.Server

	if profile {
		addr := "0.0.0.0:8080"
		log.Info(fmt.Sprintf("profiling enabled and hosted on %s", addr))
		server = &http.Server{Addr: addr, Handler: nil}
		wg.Add(1)
		go func() {
			log.Info(fmt.Sprintf("profiling server exited: %v", server.ListenAndServe()))
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
			log.Info(fmt.Sprintf("siganl received: %v", sig))
			cancel()
			if server != nil {
				err := server.Close()
				if err != nil {
					log.Info(fmt.Sprintf("failed to close server: %v", err))
				}
			}
			counter++
		}
	}()

	cfg := LoadHIDIConfig("./config/hidi.config")
	log.Info(fmt.Sprintf("HIDI config: %+v", cfg))

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		log.Info("There is no midi devices available, we're deeply sorry")
		os.Exit(1)
	}

	if len(ioDevices) < midiDevice+1 {
		log.Info(fmt.Sprintf(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices available in total",
			midiDevice, len(ioDevices),
		))
		os.Exit(1)
	}

	ioDevice, err := ioDevices[midiDevice].Open()
	if err != nil {
		log.Info(fmt.Sprintf("Failed to open MIDI device: %v", err))
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
	log.Info(fmt.Sprintf("waiting..."))
	// closing logger can be safely invoked only when all internally running goroutines (that may emit logs) are done
	close(confNotifier)
	close(sigs)
	close(otherMidiEvents)
	close(midiEvents)

	wg.Wait()
	close(log2.Messages)

	if !noPony {
		fmt.Printf(pony, score)
	}
}
