package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/logger"
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
			log.Info(fmt.Sprintf("failed to write midi event: %v", err), logger.Warning)
			continue
		}
		midiEventsEmitted += 1
	}
	log.Info("Processing midi events stopped", logger.Debug)
}

func FanOut[T any](input <-chan T) (<-chan T, <-chan T) {
	size := cap(input)
	if size == 0 {
		// at least size of 1 to prevent from output channels blocking by each other
		// also to keep running just one goroutine
		size = 1
	}
	var output1 = make(chan T, size)
	var output2 = make(chan T, size)

	go func() {
		for v := range input {
			output1 <- v
			output2 <- v
		}
		close(output1)
		close(output2)
	}()
	return output1, output2
}

func handleSigs(wg *sync.WaitGroup, sigs <-chan os.Signal, cancel func(), server *http.Server, g *gocui.Gui) {
	defer wg.Done()
	var counter int
	for sig := range sigs {
		if counter > 0 {
			fmt.Println("Dirty exit")
			os.Exit(1)
		}
		log.Info(fmt.Sprintf("siganl received: %v", sig), logger.Debug)
		cancel()
		if server != nil {
			err := server.Close()
			if err != nil {
				log.Info(fmt.Sprintf("failed to close server: %v", err), logger.Warning)
			}
		}
		if *ui {
			g.Close()
		}
		counter++
	}
}

func runUI(cfg HIDIConfig, ui bool) *gocui.Gui {
	var g *gocui.Gui
	if ui {
		var err error
		g, err = GetCli()
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
				g.Update(Layout) // high impact on performance/cpu usugae, especially in combination with hw display handler
				time.Sleep(cfg.HIDI.LogViewRate)
			}
		}()

		time.Sleep(time.Millisecond * 500) // waiting for view init TODO: fix
	}
	return g
}

func runProfileServer(wg *sync.WaitGroup) *http.Server {
	var server *http.Server
	if *profile {
		addr := "0.0.0.0:8080"
		log.Info(fmt.Sprintf("profiling enabled and hosted on %s", addr), logger.Info)
		server = &http.Server{Addr: addr, Handler: nil}
		wg.Add(1)
		go func() {
			log.Info(fmt.Sprintf("profiling server exited: %v", server.ListenAndServe()), logger.Info)
			wg.Done()
		}()
	}
	return server
}

var (
	profile  = flag.Bool("profile", false, "runs web server for performance profiling (go tool pprof)")
	grab     = flag.Bool("grab", false, "grab input devices for exclusive usage")
	ui       = flag.Bool("ui", true, "disable ui")
	nocolor  = flag.Bool("nocolor", false, "disable color")
	logLevel = flag.Int("loglevel", 2,
		"logging level, each level enables additional information class (0-6, default: 2)\n"+
			"more verbose levels may slightly impact overall performance, try to not go beyond 3 when not necessary\n"+
			"\navailable options:\n"+
			"0: errors\n"+
			"1: warnings\n"+
			"2: general info (eg. device appearance status)\n"+
			"3: action events (octave_up, channel_down etc.)\n"+
			"4: key events (keyboard keys and gamepad buttons)\n"+
			"5: unassigned key events (keyboard keys and gamepad buttons not assigned to current mapping configuration)\n"+
			"6: analog assigned and unassigned events",
	)
	noPony     = flag.Bool("nopony", false, "oh my... You can disable me if you want to, I.. I don't really mind. I'm fine")
	midiDevice = flag.Int("mididevice", 0, "select N-th midi device, default: 0 (first)")
	silient    = flag.Bool("silient", false, "no output logging")

	cfg = LoadHIDIConfig("./config/hidi.config")
)

func init() {
	flag.Parse()
	rand.Seed(time.Now().Unix())
}

func main() {
	log.Info(fmt.Sprintf("HIDI config: %+v", cfg), logger.Debug)

	g := runUI(cfg, *ui)
	createConfigDirectoryIfNeeded()

	// this wait-group has to be propagated everywhere where usual logging appear
	wg := sync.WaitGroup{}

	server := runProfileServer(&wg)

	var sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go handleSigs(&wg, sigs, cancel, server, g)

	ioDevices := midi.DetectDevices()
	if len(ioDevices) == 0 {
		log.Info("There is no midi devices available, we're deeply sorry", logger.Error)
		os.Exit(1)
	}

	if len(ioDevices) < *midiDevice+1 {
		log.Info(fmt.Sprintf(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices available in total",
			midiDevice, len(ioDevices),
		), logger.Error)
		os.Exit(1)
	}

	ioDevice, err := ioDevices[*midiDevice].Open()
	if err != nil {
		log.Info(fmt.Sprintf("Failed to open MIDI device: %v", err), logger.Error)
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
	dd := GenerateDisplayData(ctx, &wg, cfg.Screen, devices, &midiEventsEmitted, &score)
	dd1, dd2 := FanOut(dd)

	if cfg.Screen.Enabled {
		wg.Add(1)
		go display.HandleDisplay(&wg, cfg.Screen, dd1)
	} else {
		go func() {
			for range dd1 {
			}
		}()
	}

	if *ui {
		go logView(g, !*nocolor, *logLevel)
		go overviewView(g, !*nocolor, devices)
		go lcdView(g, dd2)
	} else {
		go func() {
			for range dd2 {
			}
		}()
		go func() {
			for range logger.Messages {
			}
		}()
	}

	runManager(ctx, cfg, midiEvents, *grab, devices, confNotifier)

	cancelEvents()
	log.Info(fmt.Sprintf("waiting..."), logger.Debug)
	close(confNotifier)
	close(sigs)
	close(otherMidiEvents)
	close(midiEvents)

	// closing logger can be safely invoked only when all internally running goroutines (that may emit logs) are done
	wg.Wait()
	close(logger.Messages)

	if !*noPony {
		fmt.Printf(pony, score)
	}
}
