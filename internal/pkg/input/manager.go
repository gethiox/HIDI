package input

import (
	"context"
	"log"
	"time"

	"github.com/d2r2/go-logger"
)

func fetchDevices() []Device {
	infos, err := GetHandlers()
	if err != nil {
		panic(err)
	}

	devices := Normalize(infos)
	return devices
}

func MonitorNewDevices(ctx context.Context) <-chan Device {
	l := logger.NewPackageLogger("input", logger.DebugLevel)
	var devChan = make(chan Device)

	var trackedDevs = make(map[PhysicalID]Device)
	var missingDevs []Device
	var newDevs []Device

	go func() {
		log.Printf("Monitor new devices enagged")
	root:
		for {
			select {
			case <-ctx.Done():
				break root
			default:
				break
			}

			current := fetchDevices()

			for _, d := range current {
				_, ok := trackedDevs[d.PhysicalUUID()]
				if !ok {
					newDevs = append(newDevs, d)
				}
			}

		outer:
			for _, d := range trackedDevs {
				for _, dd := range current {
					if d.PhysicalUUID() == dd.PhysicalUUID() {
						continue outer
					}
				}
				missingDevs = append(missingDevs, d)
			}

			if len(newDevs) > 0 {
				l.Infof("New Devices: %d", len(newDevs))
				for _, d := range newDevs {
					l.Infof("- %s", d.String())
					trackedDevs[d.PhysicalUUID()] = d
				}
			}

			if len(missingDevs) > 0 {
				l.Infof("Removed Devices: %d", len(missingDevs))
				for _, d := range missingDevs {
					l.Infof("- %s", d.String())
					delete(trackedDevs, d.PhysicalUUID())
				}
			}

			for _, d := range newDevs {
				devChan <- d
			}

			newDevs = nil
			missingDevs = nil
			time.Sleep(time.Second)
		}
		log.Printf("Monitor new devices disengaged")
		close(devChan)
	}()

	return devChan
}
