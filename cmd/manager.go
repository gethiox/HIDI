package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"hidi/internal/pkg/hidi"
	"hidi/internal/pkg/input"
	"hidi/internal/pkg/midi"
)

func runManager(ctx context.Context, wg *sync.WaitGroup, cfg hidi.HIDIConfig, midiEvents chan midi.Event, grab bool, devices map[*midi.Device]*midi.Device) {
	wg.Add(1)
	defer wg.Done()

	deviceConfigChange := midi.DetectDeviceConfigChanges(ctx)

root:
	for {
		select {
		case <-ctx.Done():
			log.Printf("ending run manager")
			break root
		default:
			break
		}

		configs, err := midi.LoadDeviceConfigs()
		if err != nil {
			log.Printf("Device Configs load failed: %s", err)
			os.Exit(1)
		}

		ctxConfigChange, cancel := context.WithCancel(context.Background())

		go func() {
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
		for d := range input.MonitorNewDevices(ctxConfigChange, cfg) {
			// TODO: inspect this code against possible race-condition

			var inputEvents <-chan input.InputEvent
			var err error

			appearedAt := time.Now()

			log.Printf("[\"%s\"] Opening device...", d.Name)
			for {
				inputEvents, err = d.ProcessEvents(ctxConfigChange, grab, cfg.HIDI.EVThrottling)
				if err != nil {
					if time.Now().Sub(appearedAt) > time.Second*5 {
						log.Printf("failed to open \"%s\" device on time, giving up", d.Name)
						continue device
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
				break
			}
			log.Printf("[\"%s\"] Device Opened!", d.Name)

			go func(dev input.Device) {
				log.Printf("[\"%s\"] Loading config for keyboard...", dev.Name)
				conf, err := configs.FindConfig(dev.ID, dev.DeviceType)

				if err != nil {
					panic(err)
				}
				log.Printf("[\"%s\"] Config loaded!", dev.Name)
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents)
				devices[&midiDev] = &midiDev
				log.Printf("[\"%s\"] Starting to process events", dev.Name)
				midiDev.ProcessEvents()
				log.Printf("[\"%s\"] Event processing finished", dev.Name)
				delete(devices, &midiDev)
			}(d)
		}
	}
}
