package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"hidi/internal/pkg/hidi"
	"hidi/internal/pkg/input"
	"hidi/internal/pkg/logg"
	"hidi/internal/pkg/midi"
)

func runManager(ctx context.Context, cfg hidi.HIDIConfig, logs chan logg.LogEntry, midiEvents chan midi.Event, grab bool, devices map[*midi.Device]*midi.Device) {
	deviceConfigChange := midi.DetectDeviceConfigChanges(ctx, logs)

ultra:
	for {
		select {
		case <-ctx.Done():
			log.Printf("ending run manager")
			break ultra
		default:
			break
		}

		fmt.Printf("DUPA DEBUG #1\n")
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

		newDevices := input.MonitorNewDevices(ctxConfigChange)
	device:
		for d := range newDevices {
			// TODO: inspect this code against possible race-condition

			var inputEvents <-chan input.InputEvent
			var err error

			appearedAt := time.Now()

			logs <- logg.Debugf("[\"%s\"] Opening device...", d.Name)
			for {
				inputEvents, err = d.ProcessEvents(ctxConfigChange, grab, cfg.HIDI.EVThrottling)
				if err != nil {
					if time.Now().Sub(appearedAt) > time.Second*5 {
						logs <- logg.Warning(fmt.Sprintf("failed to open \"%s\" device on time, giving up", d.Name))
						continue device
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
				break
			}
			logs <- logg.Debugf("[\"%s\"] Device Opened!", d.Name)

			go func(dev input.Device) {
				logs <- logg.Debugf("[\"%s\"] Loading config for keyboard...", dev.Name)
				conf, err := configs.FindConfig(dev.ID, dev.DeviceType)

				if err != nil {
					panic(err)
				}
				logs <- logg.Debugf("[\"%s\"] Config loaded!", dev.Name)
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents, logs)
				devices[&midiDev] = &midiDev
				logs <- logg.Debugf("[\"%s\"] Starting to process events", dev.Name)
				midiDev.ProcessEvents()
				logs <- logg.Debugf("[\"%s\"] Event processing finished", dev.Name)
				delete(devices, &midiDev)
			}(d)
		}
	}
}
