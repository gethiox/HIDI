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
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/gethiox/HIDI/cmd/hidi/openrgb"
	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/device"
	"github.com/gethiox/HIDI/internal/pkg/midi/driver"
	"github.com/gethiox/HIDI/internal/pkg/midi/driver/alsa"
	"github.com/gethiox/HIDI/internal/pkg/utils"
	"github.com/holoplot/go-evdev"
	"github.com/logrusorgru/aurora"
	gomidi "gitlab.com/gomidi/midi/v2"
)

// TODO: change integers to string in device configuration files under mapping.analog section

var (
	profile  = flag.Bool("profile", false, "runs web server for performance profiling (go tool pprof)")
	grab     = flag.Bool("grab", false, "grab input devices for exclusive usage")
	orgb     = flag.Bool("openrgb", false, "enable OpenRGB support")
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
	noPony          = flag.Bool("nopony", false, "oh my... You can disable me if you want to, I.. I don't really mind. I'm fine")
	midiDevice      = flag.Int("mididevice", 0, "select N-th midi device, default: 0 (first)")
	orgbPort        = flag.Int("orgbport", -1, "use external opengrb server")
	listMidiDevices = flag.Bool("listmididevices", false, "list available midi devices")
	listDevices     = flag.Bool("listdevices", false, "list available keyboards/gamepads")
	silent          = flag.Bool("silent", false, "no output logging, best performance")
	virtual         = flag.Bool("virtual", false, "create virtual alsa midi port instead of connecting to existing one")
	standalone      = flag.Bool("standalone", false, "start application and preserve selected by user keyboard as standard input device")
)

var log = logger.GetLogger()

func init() {
	flag.Parse()
	*logLevel += 2
	rand.Seed(time.Now().Unix())
}

//go:embed pony.txt
var pony string

func handleSigs(sigs <-chan os.Signal, cancel func()) {
	var counter int
	for sig := range sigs {
		if counter > 0 {
			fmt.Println("Dirty exit")
			os.Exit(1)
		}
		log.Info(fmt.Sprintf("siganl received: %v", sig), logger.Debug)
		cancel()
		counter++
	}
}

func runProfileServer(ctx context.Context) {
	var server *http.Server

	addr := "0.0.0.0:8080"
	log.Info(fmt.Sprintf("profiling enabled and hosted on %s", addr), logger.Info)
	server = &http.Server{Addr: addr, Handler: nil}

	go func() {
		defer server.Close()
		<-ctx.Done()
	}()

	log.Info(fmt.Sprintf("starting server"), logger.Info)
	err := server.ListenAndServe()
	if err != nil {
		log.Info(fmt.Sprintf("server exited with error: %s", err), logger.Warning)
	} else {
		log.Info("server exited", logger.Warning)
	}
}

type SortabeDevices []input.Device

func (d SortabeDevices) Len() int           { return len(d) }
func (d SortabeDevices) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d SortabeDevices) Less(i, j int) bool { return d[i].Name < d[j].Name }

func collectDevices(s time.Duration) []input.Device {
	ctx, cancel := context.WithCancel(context.Background())
	devices := input.MonitorNewDevices(ctx, time.Millisecond*100, time.Millisecond*500)
	time.Sleep(s)
	cancel()

	list := make([]input.Device, 0)
	for d := range devices {
		list = append(list, d)
	}
	return list
}

func main() {
	defer gomidi.CloseDriver()

	var ignoredIDs = make([]input.PhysicalID, 0)

	switch {
	case *listMidiDevices:
		for i, p := range alsa.GetPorts() {
			fmt.Printf("%d: %s\n", i, p.String())
		}
		os.Exit(0)
	case *listDevices:
		fmt.Printf("collecting devices...\n")
		devices := collectDevices(time.Second)
		for _, d := range devices {
			fmt.Printf("%s # [%s] (%s)\n", d.ID.String(), d.Name, d.DeviceType.String())
		}
		os.Exit(0)
	case *standalone:
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(time.Millisecond * 500)
			cancel()
		}()

		var devices = make(SortabeDevices, 0)

		for d := range input.MonitorNewDevices(ctx, time.Millisecond*100, time.Millisecond*500) {
			if d.DeviceType != input.KeyboardDevice {
				continue
			}

			devices = append(devices, d)
		}

		sort.Sort(devices)

		var pattedKeyboards = make(chan input.PhysicalID)
		wg := sync.WaitGroup{}
		ctx, cancel = context.WithCancel(context.Background())

		for i, d := range devices {
			events, err := d.ProcessEvents(ctx, false, time.Millisecond*10)
			if err != nil {
				fmt.Printf("device %d: %s (warning: failed to monitor device for events)\n", i, d.String())
				continue
			}

			fmt.Printf("device %d: %s (listening on this device)\n", i, d.String())

			wg.Add(1)
			go func(events <-chan *input.InputEvent, ID input.PhysicalID) {
				defer wg.Done()
				for ev := range events {
					if ev.Event.Type != evdev.EV_KEY {
						continue
					}

					pattedKeyboards <- ID
				}
			}(events, d.PhysicalUUID())
		}

		fmt.Printf("Pat the keyboard that you want to keep as computer input device\n")
		pattedDevice := <-pattedKeyboards
		cancel()
		wg.Wait()
		close(pattedKeyboards)

		var found bool
		for _, d := range devices {
			if d.PhysicalUUID() == pattedDevice {
				fmt.Printf("patted keybaord: %s\n", d.String())
				found = true
				ignoredIDs = append(ignoredIDs, pattedDevice)
				break
			}
		}

		if !found {
			fmt.Printf("critical error: received device id: \"%s\", but device not found in listed ones", pattedDevice)
			os.Exit(1)
		}
	}

	var midiPort driver.Port
	var err error

	if *virtual {
		midiPort, err = alsa.CreatePort("HIDI")
	} else {
		midiPort, err = alsa.PickMidiPort(*midiDevice)
	}

	if err != nil {
		fmt.Printf("Failed to get midi port: %s\n", err)
		os.Exit(1)
	}

	err = updateHIDIConfiguration()
	if err != nil {
		log.Info(fmt.Sprintf("configuration upkeep task failed: %s", err), logger.Warning)
	}

	cfg, err := LoadHIDIConfig(configDir + "/hidi.toml")
	if err != nil {
		fmt.Printf("Failed to load hidi.toml: %s\n", err)
		os.Exit(1)
	}

	devBlacklist, err := loadDeviceBlacklist()
	if err != nil {
		log.Info(fmt.Sprintf("Failed to load device blacklist: %s", err), logger.Warning)
	} else {
		for _, dev := range devBlacklist {
			log.Info(fmt.Sprintf("ignoring device %s", dev), logger.Warning)
			ignoredIDs = append(ignoredIDs, dev)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	var sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go handleSigs(sigs, cancel)

	if *profile {
		go runProfileServer(ctx)
	}

	wg := sync.WaitGroup{}

	port := rand.Intn(65535-1024) + 1024

	if *orgb {
		if *orgbPort != -1 {
			port = *orgbPort
		} else {
			if len(openrgb.OpenRGB) != 0 {
				log.Info(fmt.Sprintf(
					"starting OpenRGB %s server (%s)", openrgb.OpenRGBVersion, openrgb.OpenRGBArchitecture,
				), logger.Info)
				wg.Add(1)
				go utils.RunBinary(&wg, ctx, openrgb.OpenRGB, port)
			} else {
				log.Info("OpenRGB is not included in that build", logger.Warning)
			}
		}
	}

	var midiEventsOut = make(chan midi.Event, 8)
	var midiEventsIn = make(chan midi.Event, 8)

	score := midi.Score{}

	midi.ProcessMidiEvents(ctx, midiPort, midiEventsOut, midiEventsIn, &score)

	var devices = make(map[*device.Device]*device.Device, 16)
	var devicesMutex = sync.Mutex{}

	processLogs(ctx, sigs, cfg, devices, &devicesMutex)

	managerConfig := ManagerConfig{
		HIDI:           cfg,
		Grab:           *grab,
		NoLogs:         *silent,
		OpenRGBPort:    port,
		IgnoredDevices: ignoredIDs,
	}

	manager := NewManager(managerConfig, midiEventsOut, midiEventsIn, &devicesMutex, devices, sigs)
	manager.Run(ctx)

	log.Info(fmt.Sprintf("waiting..."), logger.Debug)

	close(midiEventsOut)

	wg.Wait()

	if !*noPony {
		time.Sleep(time.Millisecond * 200)
		fmt.Printf(pony, score.Score)
	}
}

func processLogs(
	ctx context.Context, sigs chan os.Signal,
	cfg HIDIConfig,
	devices map[*device.Device]*device.Device,
	devicesMutex *sync.Mutex,
) {
	go func() {
		if *silent {
			for range logger.Messages {
				// silently consume incoming messages
			}
		} else {
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
	}()

}
