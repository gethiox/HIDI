package main

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/amenzhinsky/go-memexec"
	"github.com/awesome-gocui/gocui"
	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
	"github.com/gethiox/HIDI/internal/pkg/midi/device"
	"github.com/logrusorgru/aurora"
)

var midiEventsEmitted, score uint // counter for display info

//go:embed pony.txt
var pony string

func processMidiEvents(ctx context.Context, wg *sync.WaitGroup, ioDevice *os.File,
	midiEventsOut, otherMidiEvents <-chan midi.Event, midiEventsIn chan<- midi.Event) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ev midi.Event
		var ok bool
	root:
		for {
			select {
			case <-ctx.Done():
				break root
			case ev, ok = <-midiEventsOut:
				if ok { // todo: investigate
					if ev[0]&0b11110000 == midi.NoteOn {
						score += 1
					}
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
		err := ioDevice.Close()
		if err != nil {
			log.Info(fmt.Sprintf("Failed to close ioDevice: %s", err), logger.Warning)
		}
		log.Info("Processing output midi events stopped", logger.Debug)
	}()

	var buf = make([]byte, 1024)

	wg.Add(1)
	go func() {
		defer wg.Done()

		var prevLeftovers = make([]byte, 0)
	root:
		for {
			select {
			case <-ctx.Done():
				break root
			default:
				break
			}

			n, err := ioDevice.Read(buf)
			if err != nil {
				log.Info(fmt.Sprintf("failed to read midi event: %v", err), logger.Warning)
				continue
			}

			prevLeftovers = append(prevLeftovers, buf[:n]...)
			events, leftOvers := midi.ExtractEvents(prevLeftovers)
			prevLeftovers = leftOvers
			for _, e := range events {
				midiEventsIn <- e
			}
		}
		log.Info("Processing input midi events stopped", logger.Debug)
	}()
}

type dynamicFanOut[T any] struct {
	input    <-chan T
	inputCap int
	rand     *rand.Rand

	mutex   sync.Mutex
	outputs map[int64]chan T
}

func newDynamicFanOut[T any](input <-chan T) dynamicFanOut[T] {
	f := dynamicFanOut[T]{
		rand:     rand.New(rand.NewSource(time.Now().Unix())),
		input:    input,
		inputCap: cap(input),
		outputs:  make(map[int64]chan T),
	}
	go f.run()
	return f
}

func (f *dynamicFanOut[T]) run() {
	for e := range f.input {
		f.mutex.Lock()
		for _, o := range f.outputs {
			o <- e
		}
		f.mutex.Unlock()
	}
}

// SpawnOutput creates new output channel and its ID
func (f *dynamicFanOut[T]) SpawnOutput() (int64, <-chan T) {
	ocap := f.inputCap
	if ocap == 0 {
		ocap = 1
	}
	newChan := make(chan T, ocap)
	var id int64

	f.mutex.Lock()
	for {
		id = f.rand.Int63()
		_, ok := f.outputs[id]
		if !ok {
			break
		}
	}
	f.outputs[id] = newChan
	f.mutex.Unlock()
	return id, newChan
}

// DespawnOutput removes output channel with given ID
func (f *dynamicFanOut[T]) DespawnOutput(id int64) error {
	f.mutex.Lock()
	c, ok := f.outputs[id]
	if !ok {
		return fmt.Errorf("output id %d not found", id)
	}
	close(c)
	delete(f.outputs, id)
	f.mutex.Unlock()
	return nil
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

func runUI(cfg HIDIConfig, ui bool, sigs chan os.Signal) *gocui.Gui {
	var g *gocui.Gui
	if ui {
		var err error
		g, err = GetCli()
		if err != nil {
			panic(err)
		}

		go func() {
			if err := g.MainLoop(); err != nil {
				if err != gocui.ErrQuit {
					panic(err)
				}
				g.Close()
				sigs <- syscall.SIGINT // pretend that we received signal when exited from gui
			}
			g.Close()
		}()

		go func() {
			last := time.Now()
			for {
				g.UpdateAsync(Layout) // high impact on performance/cpu usugae, especially in combination with hw display handler
				now := time.Now()
				time.Sleep(cfg.HIDI.LogViewRate - (now.Sub(last)))
				last = now
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
	ui       = flag.Bool("ui", false, "engage debug ui")
	orgb     = flag.Bool("openrgb", false, "enable OpenRGB support")
	force256 = flag.Bool("256", false, "force 256 color mode")
	nocolor  = flag.Bool("nocolor", false, "disable color")
	logLevel = flag.Int("loglevel", 3,
		"logging level, each level enables additional information class (0-4, default: 3)\n"+
			"more verbose levels may slightly impact overall performance, try to not go beyond 3 when not necessary\n"+
			"\navailable options:\n"+
			"0: general info (eg. device appearance status)\n"+
			"1: action events (octave_up, channel_down etc.)\n"+
			"2: key events (keyboard keys and gamepad buttons)\n"+
			"3: unassigned key events (keyboard keys and gamepad buttons not assigned to current mapping configuration)\n"+
			"4: analog assigned and unassigned events",
	)
	noPony     = flag.Bool("nopony", false, "oh my... You can disable me if you want to, I.. I don't really mind. I'm fine")
	midiDevice = flag.Int("mididevice", 0, "select N-th midi device, default: 0 (first)")
	silent     = flag.Bool("silent", false, "no output logging, best performance")
)

func init() {
	flag.Parse()
	*logLevel += 2
	rand.Seed(time.Now().Unix())
}

type logBuffer struct {
	buffer         [][]byte
	size, position int
}

func (b *logBuffer) WriteMessage(message []byte) {
	b.buffer[b.position] = message
	if b.position+1 == b.size {
		b.position = 0
	} else {
		b.position++
	}
}

func (b *logBuffer) ReadLastMessages(n int) [][]byte {
	if n > b.size {
		n = b.size
	}
	var data = make([][]byte, 0)
	for i := n; i > 0; i-- {
		data = append(data, b.buffer[((b.position-i)%b.size+b.size)%b.size])
	}
	return data
}

func newLogBuffer(size int) logBuffer {
	return logBuffer{
		buffer:   make([][]byte, size),
		size:     size,
		position: 0,
	}
}

func processSimpleOutput() {
	if *silent {
		for range logger.Messages { // silently consume incoming messages
		}
	} else {
		fmt.Printf("for nicer output use -ui flag\n")
		au := aurora.NewAurora(!*nocolor)
		for data := range logger.Messages {
			msg, err := unpack(data)
			if err != nil {
				fmt.Printf("%s\n", string(data))
				continue
			}
			m := prepareString(msg, au, -1, *logLevel)
			if m != "" {
				fmt.Printf("%s\n", m)
			}
		}
	}
}

func runBinary(wg *sync.WaitGroup, ctx context.Context, port int) {
	defer wg.Done()
	exe, err := memexec.New(OpenRGB)
	if err != nil {
		panic(err)
	}
	wg.Add(1)
	defer func() {
		defer wg.Done()
		err := exe.Close()
		if err != nil {
			log.Info(fmt.Sprintf("failed to close memory exec: %s", err), logger.Error)
		}
	}()

	cmd := exe.Command("--server", "--noautoconnect", "--server-port", fmt.Sprintf("%d", port))

	out1, in1 := io.Pipe()
	out2, in2 := io.Pipe()

	defer out1.Close()
	defer in1.Close()
	defer out2.Close()
	defer in2.Close()

	cmd.Stdout = in1
	cmd.Stderr = in2

	log.Info("[OpenRGB] start", logger.Debug)
	err = cmd.Start()
	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Failed to start: %s", err), logger.Error)
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		time.Sleep(time.Millisecond * 200)
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			log.Info(fmt.Sprintf("[OpenRGB] failed to send signal: %s", err), logger.Error)
		} else {
			log.Info("[OpenRGB] interrupt success", logger.Info)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scan1 := bufio.NewScanner(out1)
		for scan1.Scan() {
			log.Info("[OpenRGB] o> "+scan1.Text(), logger.Debug)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan2 := bufio.NewScanner(out2)
		for scan2.Scan() {
			log.Info("[OpenRGB] e> "+scan2.Text(), logger.Debug)
		}
	}()

	err = cmd.Wait()
	if err != nil {
		log.Info(fmt.Sprintf("[OpenRGB] Execution error: %s", err), logger.Error)
	}
	log.Info("[OpenRGB] Done", logger.Debug)

}

func main() {
	ioDevices, err := midi.DetectDevices()
	if err != nil {
		fmt.Printf("MIDI Device detection failed: %s\n", err)
		os.Exit(1)
	}

	if len(ioDevices) == 0 {
		fmt.Printf("There is no midi devices available, we're deeply sorry\n")
		os.Exit(1)
	}

	if len(ioDevices) < *midiDevice+1 {
		fmt.Printf(
			"MIDI device with \"%d\" ID does not exist. There is %d MIDI devices available in total\n",
			midiDevice, len(ioDevices))
		os.Exit(1)
	}

	ioDevice, err := ioDevices[*midiDevice].Open()
	if err != nil {
		fmt.Printf("Failed to open MIDI device: %v\n", err)
		os.Exit(1)
	}

	err = createConfigDirectoryIfNeeded()
	if err != nil {
		log.Info(fmt.Sprintf("configuration upkeep task failed: %s", err), logger.Warning)
	}

	cfg, err := LoadHIDIConfig(configDir + "/hidi.config")
	if err != nil {
		fmt.Printf("Failed to load hidi.config: %s\n", err)
		os.Exit(1)
	}

	// end of critical checks

	log.Info(fmt.Sprintf("HIDI config: %+v", cfg), logger.Debug)

	var sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	if *force256 == true {
		os.Setenv("TERM", "xterm-256color")
	}
	g := runUI(cfg, *ui && !*silent, sigs)

	// this wait-group has to be propagated everywhere where usual logging appear
	wg := sync.WaitGroup{}

	port := rand.Intn(65535 - 1024)
	port += 1024

	if *orgb {
		if len(OpenRGB) != 0 {
			log.Info(fmt.Sprintf("starting OpenRGB server (%s)", OpenRGBVersion), logger.Info)
			wg.Add(1)
			go runBinary(&wg, ctx, port)
		} else {
			log.Info("OpenRGB is not included in that build", logger.Warning)
		}
	}

	server := runProfileServer(&wg)

	wg.Add(1)
	go handleSigs(&wg, sigs, cancel, server, g)

	var midiEventsOut = make(chan midi.Event, 8)
	var midiEventsIn = make(chan midi.Event, 8)
	var otherMidiEvents = make(chan midi.Event, 8)

	confNotifier := make(chan validate.NotifyMessage)
	wg.Add(1)
	go monitorConfChanges(ctx, &wg, confNotifier, otherMidiEvents)

	eventCtx, cancelEvents := context.WithCancel(context.Background())
	processMidiEvents(eventCtx, &wg, ioDevice, midiEventsOut, otherMidiEvents, midiEventsIn)
	var devices = make(map[*device.Device]*device.Device, 16)
	var devicesMutex = sync.Mutex{}

	wg.Add(1)
	dd := GenerateDisplayData(ctx, &wg, cfg.Screen, devices, &devicesMutex, &midiEventsEmitted, &score)
	ddFanOut := newDynamicFanOut(dd)

	if cfg.Screen.Enabled {
		wg.Add(1)
		go func() {
			id, dd := ddFanOut.SpawnOutput()
			display.HandleDisplay(&wg, cfg.Screen, dd)
			_ = ddFanOut.DespawnOutput(id)
		}()
	}

	if *ui && !*silent {
		go logView(g, !*nocolor, *logLevel, cfg.HIDI.LogBufferSize)
		go overviewView(g, !*nocolor, devices, &devicesMutex)
		go func() {
			id, dd := ddFanOut.SpawnOutput()
			lcdView(g, dd)
			_ = ddFanOut.DespawnOutput(id)
		}()
	} else {
		go processSimpleOutput()
	}

	runManager(ctx, cfg, *grab, *silent, devices, &devicesMutex, midiEventsOut, midiEventsIn, confNotifier, port)

	cancelEvents()
	log.Info(fmt.Sprintf("waiting..."), logger.Debug)
	close(confNotifier)
	close(sigs)
	close(otherMidiEvents)
	close(midiEventsOut)

	// closing logger can be safely invoked only when all internally running goroutines (that may emit logs) are done
	wg.Wait()
	close(logger.Messages)

	if !*noPony {
		fmt.Printf(pony, score)
	}
}
