package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/config"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
	"go.uber.org/zap"
)

// runManager is the main program process, before exiting from that function it needs to ensure that
// all goroutine execution has completed
func runManager(
	ctx context.Context, cfg HIDIConfig,
	grab, noLogs bool, devices map[*midi.Device]*midi.Device,
	midiEvents chan<- midi.Event, configNotifier chan<- validate.NotifyMessage,
) {
	deviceConfigChange := config.DetectDeviceConfigChanges(ctx)

	wg := sync.WaitGroup{}

	log.Info("Run manager", logger.Debug)
root:
	for {
		select {
		case <-ctx.Done():
			break root
		default:
			break
		}

		configs, err := config.LoadDeviceConfigs(ctx, &wg, configNotifier)
		if err != nil {
			log.Info(fmt.Sprintf("Device Configs load failed: %s", err), logger.Error)
			os.Exit(1)
		}

		ctxDevice, cancel := context.WithCancel(context.Background())

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-deviceConfigChange:
				log.Info("handling config change", logger.Debug)
				cancel()
			case <-ctx.Done():
				cancel()
			}
		}()

	device:
		for d := range input.MonitorNewDevices(ctxDevice, cfg.HIDI.StabilizationPeriod, cfg.HIDI.DiscoveryRate) {
			// TODO: inspect this code against possible race-condition

			log.Info("Loading config for device...", zap.String("device_name", d.Name), logger.Debug)
			conf, err := configs.FindConfig(d.ID, d.DeviceType)

			if err != nil {
				log.Info(fmt.Sprintf("failed to load config for device: %v", err), zap.String("device_name", d.Name), logger.Warning)
				continue
			}

			var inputEvents <-chan *input.InputEvent

			appearedAt := time.Now()

			log.Info("Opening device...", zap.String("device_name", d.Name), logger.Debug)
			for {
				inputEvents, err = d.ProcessEvents(ctxDevice, grab, cfg.HIDI.EVThrottling)
				if err != nil {
					if time.Now().Sub(appearedAt) > time.Second*5 {
						log.Info("failed to open device on time, giving up", zap.String("device_name", d.Name), logger.Warning)
						continue device
					}
					time.Sleep(time.Millisecond * 100)
					continue
				}
				break
			}

			wg.Add(1)
			go func(dev input.Device, conf config.DeviceConfig) {
				defer wg.Done()
				midiDev := midi.NewDevice(dev, conf, inputEvents, midiEvents, noLogs)
				devices[&midiDev] = &midiDev
				log.Info("Device connected", zap.String("device_name", dev.Name),
					zap.String("config", fmt.Sprintf("%s (%s)", conf.ConfigFile, conf.ConfigType)),
					zap.String("device_type", dev.DeviceType.String()),
					logger.Info,
				)
				wg.Add(1)
				midiDev.ProcessEvents(&wg)
				log.Info("Device disconnected", zap.String("device_name", dev.Name), logger.Info)
				delete(devices, &midiDev)
			}(d, conf)
		}
	}
	wg.Wait()
	log.Info("Exit manager", logger.Debug)
}
