package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/gethiox/HIDI/internal/pkg/midi/device"
	"github.com/gethiox/HIDI/internal/pkg/midi/device/config"
	"github.com/gethiox/HIDI/internal/pkg/utils"
	"go.uber.org/zap"
)

type Manager struct {
	config ManagerConfig

	midiOut chan<- midi.Event
	midiIn  <-chan midi.Event

	devicesMutex *sync.Mutex
	devices      map[*device.Device]*device.Device

	sigs chan os.Signal
}

type ManagerConfig struct {
	HIDI           HIDIConfig
	Grab, NoLogs   bool
	OpenRGBPort    int
	IgnoredDevices []input.PhysicalID
}

func NewManager(
	config ManagerConfig,
	midiOut chan<- midi.Event,
	midiIn <-chan midi.Event,
	devicesMutex *sync.Mutex,
	devices map[*device.Device]*device.Device,
	sigs chan os.Signal,
) Manager {
	return Manager{
		config:       config,
		midiOut:      midiOut,
		midiIn:       midiIn,
		devicesMutex: devicesMutex,
		devices:      devices,
		sigs:         sigs,
	}
}

// Run is the main program process, before exiting from that function it needs to ensure that
// all goroutine execution has completed
func (m Manager) Run(ctx context.Context) {
	deviceConfigChange := config.DetectDeviceConfigChanges(ctx)

	wg := sync.WaitGroup{}
	midiEventsInSpawner := utils.NewDynamicFanOut(m.midiIn)

	log.Info("Run manager", logger.Debug)
	log.Info(fmt.Sprintf("ignored keyboards: %+v", m.config.IgnoredDevices), logger.Debug)

root:
	for {
		select {
		case <-ctx.Done():
			break root
		default:
			break
		}

		configs, err := config.LoadDeviceConfigs(ctx, &wg)
		if err != nil {
			log.Info(fmt.Sprintf("Device Configs load failed: %s", err), logger.Error)
			os.Exit(1)
		}

		ctxDevice, cancel := context.WithCancel(context.Background())

		go func() {
			select {
			case <-deviceConfigChange:
				log.Info("handling config change", logger.Debug)
				cancel()
			case <-ctx.Done():
				cancel()
			}
		}()

	device:
		for d := range input.MonitorNewDevices(ctxDevice, m.config.HIDI.HIDI.StabilizationPeriod, m.config.HIDI.HIDI.DiscoveryRate) {
			log.Info(fmt.Sprintf("ignored devices: %+v", m.config.IgnoredDevices), zap.String("device_name", d.Name), logger.Debug)
			log.Info(fmt.Sprintf("device id: %+v", d.ID), zap.String("device_name", d.Name), logger.Debug)
			for _, id := range m.config.IgnoredDevices {
				if d.PhysicalUUID() == id {
					log.Info("ignoring device", zap.String("device_name", d.Name), logger.Debug)
					continue device
				} else {
					log.Info("not ignoring device", zap.String("device_name", d.Name), logger.Debug)
				}
			}

			log.Info("Loading config for device...", zap.String("device_name", d.Name), logger.Debug)
			conf, err := configs.FindConfig(d.ID, d.DeviceType)
			if err != nil {
				if errors.Is(err, config.UnsupportedDeviceType) {
					log.Info(fmt.Sprintf("failed to load config for device: %v", err), zap.String("device_name", d.Name), logger.Warning)
					continue
				}
				log.Info(fmt.Sprintf("failed to load config for device: %v", err), zap.String("device_name", d.Name), logger.Error)
				continue
			}
			log.Info(fmt.Sprintf("config loaded: %s", conf.ConfigFile), logger.Debug)

			var inputEvents <-chan *input.InputEvent

			appearedAt := time.Now()

			log.Info("Opening device...", zap.String("device_name", d.Name), logger.Debug)
			for {
				inputEvents, err = d.ProcessEvents(ctxDevice, m.config.Grab, m.config.HIDI.HIDI.EVThrottling)
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
				id, midiIn, err := midiEventsInSpawner.SpawnOutput()
				if err != nil {
					panic(err)
				}

				midiDev := device.NewDevice(dev, conf, m.midiOut, midiIn, m.config.NoLogs, m.config.OpenRGBPort, m.sigs)
				m.devicesMutex.Lock()
				m.devices[&midiDev] = &midiDev
				m.devicesMutex.Unlock()
				log.Info("Device connected", zap.String("device_name", dev.Name),
					zap.String("config", fmt.Sprintf("%s (%s)", conf.ConfigFile, conf.ConfigType)),
					zap.String("device_type", dev.DeviceType.String()),
					logger.Info,
				)

				midiDev.ProcessEvents(inputEvents)

				log.Info("Device disconnected", zap.String("device_name", dev.Name), logger.Info)
				err = midiEventsInSpawner.DespawnOutput(id)
				if err != nil {
					log.Info(
						fmt.Sprintf("failed to despawn midi input channel: %s", err),
						zap.String("device_name", dev.Name), logger.Error,
					)
				}
				m.devicesMutex.Lock()
				delete(m.devices, &midiDev)
				m.devicesMutex.Unlock()
			}(d, conf)
		}
	}

	log.Info("Waiting in manager", logger.Debug)
	wg.Wait()
	log.Info("Exit manager", logger.Debug)
}
