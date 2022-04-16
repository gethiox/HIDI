package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"hidi/internal/pkg/input"
	"hidi/internal/pkg/logg"
	"hidi/internal/pkg/midi"

	"github.com/gethiox/go-evdev"
)

func runManager(logs chan logg.LogEntry, midiEvents chan midi.Event, grab bool, devices map[*midi.Device]*midi.Device) {
	deviceConfigChange := make(chan bool)
	go midi.DetectDeviceConfigChanges(logs, deviceConfigChange)

	for {
		configs, err := midi.LoadDeviceConfigs()
		if err != nil {
			log.Printf("Device Configs load failed: %s", err)
			os.Exit(1)
		}

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			for range deviceConfigChange {
				cancel()
				break
			}
		}()

		newDevices := input.MonitorNewDevices(ctx)
	device:
		for {
			var d input.Device

			select {
			case <-ctx.Done():
				break device
			case d = <-newDevices:
				break
			}

			var inputEvents <-chan *evdev.InputEvent
			var err error

			appearedAt := time.Now()

			logs <- logg.Debugf("[\"%s\"] Opening device...", d.Name)
			for {
				inputEvents, err = d.ProcessEvents(ctx, grab)
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
				// logs <- logg.Debugf("[\"%s\"] Config: %+v", conf)
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents, logs)
				devices[&midiDev] = &midiDev
				logs <- logg.Debugf("[\"%s\"] Starting to process events", dev.Name)
				midiDev.ProcessEvents(ctx)
				logs <- logg.Debugf("[\"%s\"] Event processing finished", dev.Name)
				delete(devices, &midiDev)
			}(d)
		}
	}
}
