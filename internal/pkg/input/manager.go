package input

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/holoplot/go-evdev"
	"go.uber.org/zap"
)

func monitorNewHandlers(ctx context.Context, discoveryRate time.Duration) <-chan []string {
	var newHandlers = make(chan []string)

	go func() {
		var previous = make(map[string]bool)
		log.Info("monitoring nev event handlers", logger.Debug)

		firstRun := true
	root:
		for {
			if !firstRun {
				select {
				case <-ctx.Done():
					break root
				case <-time.After(discoveryRate):
					break
				}
			} else {
				firstRun = false
			}

			entries, err := os.ReadDir("/dev/input")
			if err != nil {
				panic(err)
			}

			var events = make(map[string]bool, len(entries))
			for _, e := range entries {
				if !e.IsDir() && strings.HasPrefix(e.Name(), "event") {
					events[e.Name()] = true
				}
			}

			var newEvents []string
			for ev := range events {
				if !previous[ev] {
					newEvents = append(newEvents, ev)
				}
			}

			var removedEvents []string
			for ev := range previous {
				if !events[ev] {
					removedEvents = append(removedEvents, ev)
				}
			}

			for _, ev := range removedEvents {
				delete(previous, ev)
			}
			for _, ev := range newEvents {
				previous[ev] = true
			}

			if len(newEvents) > 0 {
				newHandlers <- newEvents
			}
		}
		close(newHandlers)
	}()
	return newHandlers
}

// getDeviceInfo returns DeviceInfo for given event name, eg. event5
func getDeviceInfo(ev string) (DeviceInfo, error) {
	dPath := fmt.Sprintf("/dev/input/%s", ev)
	d, err := evdev.Open(dPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeviceInfo{}, fmt.Errorf("%s: %w", dPath, err)
		} else {
			return DeviceInfo{}, fmt.Errorf("failed to open evdev: %w", err)
		}
	}
	defer d.Close()

	capableTypes := d.CapableTypes() // todo: return error in go-evdev
	properties := d.Properties()     // todo: return error in go-evdev

	inputID, err := d.InputID()
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to read InputID: %w", err)
	}
	name, err := d.Name()
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to read Name: %w", err)
	}
	phys, err := d.PhysicalLocation()
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to read PhysicalLocation: %w", err)
	}
	uniq, _ := d.UniqueID()
	// todo: for some reason ioctl may return "no such file or directory" error (error code: 2)
	//       maybe this is expected error code when uniqueID is not available

	return DeviceInfo{
		ID: InputID{
			Bus:     inputID.BusType,
			Vendor:  inputID.Vendor,
			Product: inputID.Product,
			Version: inputID.Version,
		},
		Name:      name,
		Phys:      phys,
		Sysfs:     dPath,
		Uniq:      uniq,
		eventName: ev,

		CapableTypes: capableTypes,
		Properties:   properties,
	}, nil
}

func MonitorNewDevices(ctx context.Context, stabilizationPeriod, discoveryRate time.Duration) <-chan Device {
	var devChan = make(chan Device)

	go func() {
		log.Info("Monitor new devices engaged", logger.Debug)

		newEvents := monitorNewHandlers(ctx, discoveryRate)
		var events []string

		firstRun := true
	root:
		for {
			if !firstRun {
				select {
				case <-ctx.Done():
					break root
				case x := <-newEvents:
					events = append(events, x...)
					continue // new event handlers may appear between samples
				case <-time.After(stabilizationPeriod):
					break
				}
			} else {
				events = append(events, <-newEvents...)
				firstRun = false
			}

			if len(events) == 0 {
				continue
			}

			var deviceInfos []DeviceInfo
			for _, ev := range events {
				deviceInfo, err := getDeviceInfo(ev)
				if err != nil {
					log.Info(fmt.Sprintf("Failed to process event handler: %s", err), zap.String("handler_name", ev), logger.Error)
					continue
				}

				deviceInfos = append(deviceInfos, deviceInfo)
			}

			for _, device := range Normalize(deviceInfos) {
				log.Info(fmt.Sprintf("Normalized device: %+v", device), logger.Debug)
				devChan <- device
			}
			events = nil
		}

		log.Info("Monitor new devices disengaged", logger.Debug)
		close(devChan)
	}()

	return devChan
}
