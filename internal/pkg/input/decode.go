package input

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// GetHandlers returns a list of available input handlers in the system.
// Note: there is non-zero probability that returned list may be incomplete,
// no matter where they come from, either /proc/bus/input/devices or /dev/input listing has the same behavior.
// This is needed to be handled when user wants to have a complete group of handlers for given hardware device.
func GetHandlers() ([]DeviceInfo, error) {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil, err
	}

	di, err := unmarshal(data)
	if err != nil {
		return nil, err
	}

	return di, nil
}

// zeroHexPadUint32 prepares string to be used by hex.DecodeString()
func zeroHexPadUint32(s string) string {
	return fmt.Sprintf("%08s", s)
}

// unmarshal parses /proc/bus/input/devices file
func unmarshal(data []byte) ([]DeviceInfo, error) {
	var devices = make([]DeviceInfo, 0)

	if len(data) == 0 {
		return devices, nil
	}

	sdata := string(data)

	var device DeviceInfo

	var emptyLineCounter = 0
	for _, line := range strings.Split(sdata, "\n") {
		if line == "" {
			emptyLineCounter += 1
			if emptyLineCounter < 2 {
				devices = append(devices, device)
				device = DeviceInfo{}
			}
			continue
		}
		emptyLineCounter = 0

		label := line[:1]
		info := line[3:]

		switch label {
		case "I":
			ps := reflect.ValueOf(&device.ID)
			s := ps.Elem()

			for _, param := range strings.Split(info, " ") {
				fields := strings.Split(param, "=")
				l, v := fields[0], fields[1]
				f := s.FieldByName(l)

				hv, err := hex.DecodeString(v)
				if err != nil {
					return devices, fmt.Errorf("hex decoding failed: %v", err)
				}
				uv := binary.BigEndian.Uint16(hv)

				f.SetUint(uint64(uv))
			}
		case "N":
			device.Name = info[6 : len(info)-1]
		case "P":
			device.Phys = info[5:]
		case "S":
			device.Sysfs = info[6:]
		case "U":
			device.Uniq = info[5:]
		case "H":
			// If there is at least one handler, there is additional space at the end of the line
			handlersChain := info[9:]
			trimmed := strings.TrimRight(handlersChain, " ")
			handlers := strings.Split(trimmed, " ")
			device.Handlers = handlers
		case "B":
			ps := reflect.ValueOf(&device.Bitmaps)
			s := ps.Elem()

			fields := strings.Split(info, "=")
			l, vs := fields[0], fields[1]
			f := s.FieldByName(l)
			for i, v := range strings.Split(vs, " ") {
				v = zeroHexPadUint32(v)

				hv, err := hex.DecodeString(v)
				if err != nil {
					return devices, fmt.Errorf("hex decoding failed: %v", err)
				}
				uv := binary.BigEndian.Uint32(hv)
				// TODO: ensure values are stored in the correct way, namely order
				f.Index(i).SetUint(uint64(uv))
			}
		}
	}

	return devices, nil
}
