package midi

import (
	"fmt"
	"os"
	"strings"
)

func DetectDevices() []IODevice {
	fd, err := os.Open("/dev/snd")
	if err != nil {
		panic(err)
	}
	entries, err := fd.ReadDir(0)
	if err != nil {
		panic(err)
	}

	var devices = make([]IODevice, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasPrefix(entry.Name(), "midi") {
			devices = append(devices, IODevice{path: fmt.Sprintf("/dev/snd/%s", entry.Name())})
		}
	}

	return devices
}

type IODevice struct {
	path string
}

func (d *IODevice) Open() (*os.File, error) {
	return os.OpenFile(d.path, os.O_RDWR|os.O_SYNC, 0)
}
