package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
)

// runManager is the main program process, before exiting from that function it needs to ensure that
// all goroutine execution has completed
func runManager(ctx context.Context, cfg HIDIConfig, midiEvents chan<- midi.Event, grab bool, devices map[*midi.Device]*midi.Device, configNotifier chan<- validate.NotifyMessage) {
	deviceConfigChange := config.DetectDeviceConfigChanges(ctx)

	wg := sync.WaitGroup{}

	log.Info(fmt.Sprintf("Run manager"))
root:
	for {
		select {
		case <-ctx.Done():
			log.Info(fmt.Sprintf("ending run manager"))
			break root
		default:
			break
		}

		configs, err := config.LoadDeviceConfigs(configNotifier)
		if err != nil {
			log.Info(fmt.Sprintf("Device Configs load failed: %s", err))
			os.Exit(1)
		}

		ctxConfigChange, cancel := context.WithCancel(context.Background())

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-deviceConfigChange:
				log.Info(fmt.Sprintf("handling config change"))
				cancel()
			case <-ctx.Done():
				log.Info(fmt.Sprintf("handling interrupt"))
				cancel()
			}
		}()

	device:
		for d := range input.MonitorNewDevices(ctxConfigChange, cfg.HIDI.StabilizationPeriod, cfg.HIDI.DiscoveryRate) {
			// TODO: inspect this code against possible race-condition

			var inputEvents <-chan input.InputEvent
			var err error

			appearedAt := time.Now()

			log.Info(fmt.Sprintf("Opening device... [\"%s\"]", d.Name))
			for {
				inputEvents, err = d.ProcessEvents(ctxConfigChange, grab, cfg.HIDI.EVThrottling)
				if err != nil {
					if time.Now().Sub(appearedAt) > time.Second*5 {
						log.Info(fmt.Sprintf("failed to open device on time, giving up [\"%s\"]", d.Name))
						continue device
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
				break
			}
			log.Info(fmt.Sprintf("Device Opened! [\"%s\"]", d.Name))

			wg.Add(1)
			go func(dev input.Device) {
				defer wg.Done()
				log.Info(fmt.Sprintf("Loading config for keyboard... [\"%s\"]", dev.Name))
				conf, err := configs.FindConfig(dev.ID, dev.DeviceType)

				if err != nil {
					panic(err)
				}
				log.Info(fmt.Sprintf("Config loaded! [\"%s\"]", dev.Name))
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents)
				devices[&midiDev] = &midiDev
				log.Info(fmt.Sprintf("Starting to process events [\"%s\"]", dev.Name))
				wg.Add(1)
				midiDev.ProcessEvents(&wg)
				log.Info(fmt.Sprintf("Event processing finished [\"%s\"]", dev.Name))
				delete(devices, &midiDev)
			}(d)
		}
	}
	wg.Wait()
	log.Info(fmt.Sprintf("Exit manager"))
}
