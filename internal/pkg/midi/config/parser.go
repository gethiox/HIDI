package config

import (
	"fmt"
	"io"
	"os"
	path2 "path"
	"strconv"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/holoplot/go-evdev"
	"gopkg.in/yaml.v3"
)

type YamlDeviceConfig struct {
	Identifier struct {
		Bus     uint16 `yaml:"bus"`
		Vendor  uint16 `yaml:"vendor"`
		Product uint16 `yaml:"product"`
		Version uint16 `yaml:"version"`
		Uniq    string `yaml:"uniq"`
	} `yaml:"identifier"`
	ActionMapping map[string]string              `yaml:"action_mapping"`
	KeyMappings   []map[string]map[string]string `yaml:"midi_mappings"`
}

type YamlCustomMapping struct {
	MappingType    string `yaml:"type"`
	CC             string `yaml:"cc"`
	CCNegative     string `yaml:"cc_negative"`
	Note           string `yaml:"note"`
	NoteNegative   string `yaml:"note_negative"`
	Action         string `yaml:"action"`
	ActionNegative string `yaml:"action_negative"`
	FlipAxis       bool   `yaml:"flip_axis"`
}

type DeviceConfig struct {
	ConfigFile string
	ConfigType string // factory or user
	ID         input.InputID
	Uniq       string
	Config     Config
}

// readDeviceConfig parses yaml file and provide ready to use DeviceConfig
func readDeviceConfig(path, configType string) (DeviceConfig, error) {
	cfg := YamlDeviceConfig{}
	fd, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("opening config file failed: %w", err)
	}
	defer fd.Close()

	data, err := io.ReadAll(fd)
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("reading file data failed: %w", err)
	}

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("parsing yaml failed: %w", err)
	}

	var keyMapping []KeyMapping
	var actionMapping = make(map[evdev.EvCode]Action)

	for _, mappings := range cfg.KeyMappings {
		for name, mappingRaw := range mappings {
			var midiMapping = make(map[evdev.EvCode]byte)
			var analogMapping = make(map[evdev.EvCode]Analog)

			for evcodeRaw, valueRaw := range mappingRaw {
				_, isKey := evdev.KEYFromString[evcodeRaw]
				_, isAbs := evdev.ABSFromString[evcodeRaw]
				switch {
				case isKey:
					evcode := evdev.KEYFromString[evcodeRaw]

					noteInt, err := strconv.Atoi(valueRaw)
					if err == nil {
						if noteInt < 0 || noteInt > 127 {
							return DeviceConfig{}, fmt.Errorf("[%s] %s: note value outside of 0-127 range: %d", name, evcodeRaw, noteInt)
						}
						midiMapping[evcode] = byte(noteInt)
						continue
					}

					note, err := StringToNote(valueRaw)
					if err == nil {
						midiMapping[evcode] = note
						continue
					}
					return DeviceConfig{}, fmt.Errorf("[%s] %s: failed to parse note: %v", name, evcodeRaw, err)
				case isAbs:
					var mapping YamlCustomMapping
					evcode := evdev.ABSFromString[evcodeRaw]

					err := yaml.Unmarshal([]byte(valueRaw), &mapping)
					if err != nil {
						return DeviceConfig{}, fmt.Errorf("[%s] %s: cannot unmarshal analog configuration: %v", name, evcodeRaw, err)
					}

					var bidirectional bool

					var actions [2]Action
					for i, actionRaw := range []string{mapping.Action, mapping.ActionNegative} {
						if actionRaw == "" {
							continue
						}

						action := Action(actionRaw)
						if !SupportedActions[action] {
							return DeviceConfig{}, fmt.Errorf("[%s] %s: action not supported: %s", name, evcodeRaw, actionRaw)
						}

						actions[i] = action
						if i == 1 {
							bidirectional = true
						}
					}

					var notes [2]byte
					for i, noteRaw := range []string{mapping.Note, mapping.NoteNegative} {
						if noteRaw == "" {
							continue
						}

						noteInt, err := strconv.Atoi(noteRaw)
						if err == nil {
							if noteInt < 0 || noteInt > 127 {
								return DeviceConfig{}, fmt.Errorf("[%s] %s: note value outside of 0-127 range: %d", name, evcodeRaw, noteInt)
							}
							notes[i] = byte(noteInt)
							if i == 1 {
								bidirectional = true
							}
							continue
						}

						note, err := StringToNote(noteRaw)
						if err == nil {
							notes[i] = note
							if i == 1 {
								bidirectional = true
							}
							continue
						}
					}
					var ccs [2]byte
					for i, ccRaw := range []string{mapping.CC, mapping.CCNegative} {
						ccInt, err := strconv.Atoi(ccRaw)
						if err != nil {
							continue
						}

						if ccInt < 0 || ccInt > 119 {
							return DeviceConfig{}, fmt.Errorf("[%s] %s: cc value outside of 0-119 range: %d", name, evcodeRaw, ccInt)
						}
						ccs[i] = byte(ccInt)
						if i == 1 {
							bidirectional = true
						}
					}

					mappingType := MappingType(mapping.MappingType)
					if !SupportedMappingTypes[mappingType] {
						return DeviceConfig{}, fmt.Errorf("[%s] %s: mapping type not supported: %s", name, evcodeRaw, mapping.MappingType)
					}

					analogMapping[evcode] = Analog{
						MappingType:   mappingType,
						CC:            ccs[0],
						CCNeg:         ccs[1],
						Note:          notes[0],
						NoteNeg:       notes[1],
						Action:        actions[0],
						ActionNeg:     actions[1],
						FlipAxis:      mapping.FlipAxis,
						Bidirectional: bidirectional,
					}
				default:
					return DeviceConfig{}, fmt.Errorf("[%s] unsupported EvCode: %s", name, evcodeRaw)
				}
			}

			keyMapping = append(keyMapping, KeyMapping{
				Name:   name,
				Midi:   midiMapping,
				Analog: analogMapping,
			})
		}
	}

	for evcodeRaw, actionRaw := range cfg.ActionMapping {
		evcode, ok := evdev.KEYFromString[evcodeRaw]
		if !ok {
			return DeviceConfig{}, fmt.Errorf("[actions] unsupported EvCode: %s", evcodeRaw)
		}
		action := Action(actionRaw)
		if !SupportedActions[action] {
			return DeviceConfig{}, fmt.Errorf("[actions] unsupported action: %s", action)
		}
		actionMapping[evcode] = action
	}

	devConfig := DeviceConfig{
		ConfigFile: path2.Base(path),
		ConfigType: configType,
		ID: input.InputID{
			Bus:     cfg.Identifier.Bus,
			Vendor:  cfg.Identifier.Vendor,
			Product: cfg.Identifier.Product,
			Version: cfg.Identifier.Version,
		},
		Uniq: cfg.Identifier.Uniq,
		Config: Config{
			KeyMappings:     keyMapping,
			ActionMapping:   actionMapping,
			AnalogDeadzones: nil,
		},
	}
	return devConfig, nil
}
