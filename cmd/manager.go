package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"hidi/internal/pkg/input"
	"hidi/internal/pkg/midi"
	"hidi/internal/pkg/midi/config"
	"hidi/internal/pkg/midi/config/validate"
)

// runManager is the main program process, before exiting from that function it needs to ensure that
// all goroutine execution has completed
func runManager(ctx context.Context, cfg HIDIConfig, midiEvents chan<- midi.Event, grab bool, devices map[*midi.Device]*midi.Device, configNotifier chan<- validate.NotifyMessage) {
	deviceConfigChange := config.DetectDeviceConfigChanges(ctx)

	wg := sync.WaitGroup{}

	log.Printf("Run manager")
root:
	for {
		select {
		case <-ctx.Done():
			log.Printf("ending run manager")
			break root
		default:
			break
		}

		configs, err := config.LoadDeviceConfigs(configNotifier)
		if err != nil {
			log.Printf("Device Configs load failed: %s", err)
			os.Exit(1)
		}

		ctxConfigChange, cancel := context.WithCancel(context.Background())

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-deviceConfigChange:
				log.Printf("handling config change")
				cancel()
			case <-ctx.Done():
				log.Printf("handling interrupt")
				cancel()
			}
		}()

	device:
		for d := range input.MonitorNewDevices(ctxConfigChange, cfg.HIDI.StabilizationPeriod, cfg.HIDI.DiscoveryRate) {
			// TODO: inspect this code against possible race-condition

			var inputEvents <-chan input.InputEvent
			var err error

			appearedAt := time.Now()

			log.Printf("Opening device... [\"%s\"]", d.Name)
			for {
				inputEvents, err = d.ProcessEvents(ctxConfigChange, grab, cfg.HIDI.EVThrottling)
				if err != nil {
					if time.Now().Sub(appearedAt) > time.Second*5 {
						log.Printf("failed to open device on time, giving up [\"%s\"]", d.Name)
						continue device
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
				break
			}
			log.Printf("Device Opened! [\"%s\"]", d.Name)

			wg.Add(1)
			go func(dev input.Device) {
				defer wg.Done()
				log.Printf("Loading config for keyboard... [\"%s\"]", dev.Name)
				conf, err := configs.FindConfig(dev.ID, dev.DeviceType)

				if err != nil {
					panic(err)
				}
				log.Printf("Config loaded! [\"%s\"]", dev.Name)
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents)
				devices[&midiDev] = &midiDev
				log.Printf("Starting to process events [\"%s\"]", dev.Name)
				midiDev.ProcessEvents()
				log.Printf("Event processing finished [\"%s\"]", dev.Name)
				delete(devices, &midiDev)
			}(d)
		}
	}
	wg.Wait()
	log.Printf("Exit manager")
}
