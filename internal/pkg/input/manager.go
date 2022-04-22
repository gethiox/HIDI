package input

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"
)

func monitorNewHandlers(ctx context.Context, discoveryRate time.Duration) <-chan []string {
	var newHandlers = make(chan []string)

	go func() {
		var previous = make(map[string]bool)
		log.Printf("monitoring nev event handlers")

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
				log.Printf("sending new events: %+v", newEvents)
				newHandlers <- newEvents
			}
		}
		close(newHandlers)
	}()
	return newHandlers
}

func MonitorNewDevices(ctx context.Context, stabilizationPeriod, discoveryRate time.Duration) <-chan Device {
	var devChan = make(chan Device)

	go func() {
		log.Print("Monitor new devices engaged")

		newEvents := monitorNewHandlers(ctx, discoveryRate)
		var events []string

		log.Printf("merging proces engaged")
		firstRun := true

	root:
		for {
			if !firstRun {
				select {
				case <-ctx.Done():
					break root
				case x := <-newEvents:
					events = append(events, x...)
					continue // new event handlers may appear between samplings
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
				dPath := fmt.Sprintf("/dev/input/%s", ev)
				d, err := evdev.Open(dPath)
				if err != nil {
					panic(err)
				}

				inputID, _ := d.InputID()
				name, _ := d.Name()
				phys, err := d.PhysicalLocation()
				uniq, err := d.UniqueID()
				capableTypes := d.CapableTypes()
				properties := d.Properties()

				deviceInfo := DeviceInfo{
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
				}
				deviceInfos = append(deviceInfos, deviceInfo)
			}

			for _, device := range Normalize(deviceInfos) {
				devChan <- device
			}
			events = nil
		}

		log.Print("Monitor new devices disengaged")
		close(devChan)
	}()

	return devChan
}
