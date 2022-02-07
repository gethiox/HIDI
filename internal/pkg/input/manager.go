package input

import (
	"log"
	"time"
)

func fetchDevices() []Device {
	infos, err := GetHandlers()
	if err != nil {
		panic(err)
	}

	devices := Normalize(infos)
	return devices
}

func MonitorNewDevices() <-chan Device {
	var devChan = make(chan Device)

	var trackedDevs = make(map[PhysicalID]Device)
	var missingDevs []Device
	var newDevs []Device

	go func() {
		for {
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
				log.Printf("New Devices: %d", len(newDevs))
				for _, d := range newDevs {
					log.Printf("- %s", d.String())
					trackedDevs[d.PhysicalUUID()] = d
				}
			}

			if len(missingDevs) > 0 {
				log.Printf("Removed Devices: %d", len(missingDevs))
				for _, d := range missingDevs {
					log.Printf("- %s", d.String())
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
	}()

	return devChan
}
