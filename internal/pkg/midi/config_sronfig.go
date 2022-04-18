package midi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	path2 "path"
	"path/filepath"
	"strconv"
	"strings"

	"hidi/internal/pkg/input"
	"hidi/internal/pkg/logg"

	"github.com/fsnotify/fsnotify"
	"github.com/gethiox/go-evdev"
	"gopkg.in/yaml.v3"
)

const (
	factoryGamepad  = "./config/factory/gamepad"
	factoryKeyboard = "./config/factory/keyboard"
	userGamepad     = "./config/user/gamepad"
	userKeyboard    = "./config/user/keyboard"
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

type YamlAnalogMapping struct {
	ID           string `yaml:"id"`
	CC           string `yaml:"cc"`
	CCNegative   string `yaml:"cc_negative"`
	Note         string `yaml:"note"`
	NoteNegative string `yaml:"note_negative"`
	FlipAxis     bool   `yaml:"flip_axis"`
}

type DeviceConfig struct {
	ConfigFile string
	Name       string // factory or user
	ID         input.InputID
	Uniq       string
	Config     Config
}

// readDeviceConfig parses yaml file and provide ready to use DeviceConfig
func readDeviceConfig(path, name string) (DeviceConfig, error) {
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
					var mapping YamlAnalogMapping
					evcode := evdev.ABSFromString[evcodeRaw]

					err := yaml.Unmarshal([]byte(valueRaw), &mapping)
					if err != nil {
						return DeviceConfig{}, fmt.Errorf("[%s] %s: cannot unmarshal analog configuration: %v", name, evcodeRaw, err)
					}

					var bidirectional bool
					var notes [2]byte
					for i, noteRaw := range []string{mapping.Note, mapping.NoteNegative} {
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
					var cc [2]byte
					for i, ccRaw := range []string{mapping.CC, mapping.CCNegative} {
						ccInt, err := strconv.Atoi(ccRaw)
						if err == nil {
							if ccInt < 0 || ccInt > 119 {
								return DeviceConfig{}, fmt.Errorf("[%s] %s: cc value outside of 0-119 range: %d", name, evcodeRaw, ccInt)
							}
							cc[i] = byte(ccInt)
							if i == 1 {
								bidirectional = true
							}
						}
					}

					analogMapping[evcode] = Analog{
						id:            NameToAnalogID[mapping.ID],
						cc:            cc[0],
						ccNeg:         cc[1],
						note:          notes[0],
						noteNeg:       notes[1],
						flipAxis:      mapping.FlipAxis,
						bidirectional: bidirectional,
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
		action, ok := NameToAction[actionRaw]
		if !ok {
			return DeviceConfig{}, fmt.Errorf("[actions] unsupported action: %s", actionRaw)
		}
		actionMapping[evcode] = action
	}

	devConfig := DeviceConfig{
		ConfigFile: path2.Base(path),
		Name:       name,
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

type ConfigMap map[input.InputID]DeviceConfig

type DeviceConfigs struct {
	Factory struct {
		Keyboards ConfigMap
		Gamepads  ConfigMap
	}
	User struct {
		Keyboards ConfigMap
		Gamepads  ConfigMap
	}
}

func (c *DeviceConfigs) FindConfig(id input.InputID, devType input.DeviceType) (DeviceConfig, error) {
	// check user first
	switch devType {
	case input.KeyboardDevice:
		cfg, ok := c.User.Keyboards[id]
		if ok {
			return cfg, nil
		}
		cfg, ok = c.Factory.Keyboards[id]
		if ok {
			return cfg, nil
		}
		cfg, ok = c.Factory.Keyboards[input.InputID{}] // picking default config
		if ok {
			return cfg, nil
		}
		return DeviceConfig{}, errors.New("default keyboard config not found")

	case input.JoystickDevice:
		cfg, ok := c.User.Gamepads[id]
		if ok {
			return cfg, nil
		}
		cfg, ok = c.Factory.Gamepads[id]
		if ok {
			return cfg, nil
		}
		cfg, ok = c.Factory.Gamepads[input.InputID{}] // picking default config
		if ok {
			return cfg, nil
		}
		return DeviceConfig{}, errors.New("default gamepad config not found")
	}

	return DeviceConfig{}, fmt.Errorf("unsupported device type config: %s", devType)
}

func loadDirectory(root string, configMap ConfigMap, name string) error {
	err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())

		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			devCfg, err := readDeviceConfig(path, name)
			if err != nil {
				log.Printf("device config %s load failed: %s", name, err)
				return nil
			}
			configMap[devCfg.ID] = devCfg
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk failed: %w", err)
	}
	return nil
}

func LoadDeviceConfigs() (DeviceConfigs, error) {
	cfg := DeviceConfigs{
		Factory: struct{ Keyboards, Gamepads ConfigMap }{
			Keyboards: make(ConfigMap),
			Gamepads:  make(ConfigMap),
		},
		User: struct{ Keyboards, Gamepads ConfigMap }{
			Keyboards: make(ConfigMap),
			Gamepads:  make(ConfigMap),
		},
	}

	pairs := []struct {
		root      string
		configMap ConfigMap
		name      string
	}{
		{factoryGamepad, cfg.Factory.Gamepads, "factory"},
		{factoryKeyboard, cfg.Factory.Keyboards, "factory"},
		{userGamepad, cfg.User.Gamepads, "user"},
		{userKeyboard, cfg.User.Keyboards, "user"},
	}

	for _, pair := range pairs {
		err := loadDirectory(pair.root, pair.configMap, pair.name)
		if err != nil {
			return cfg, fmt.Errorf("loading \"%s\" directory failed: %w", pair.root, err)
		}
	}

	return cfg, nil
}

func DetectDeviceConfigChanges(ctx context.Context, logs chan logg.LogEntry) <-chan bool {
	// TODO: TODO ctx
	var change = make(chan bool)

	go func() {
		defer close(change)
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return
		}

		for _, path := range []string{
			factoryGamepad,
			factoryKeyboard,
			userGamepad,
			userKeyboard,
		} {
			err = watcher.Add(path)
		}

		for event := range watcher.Events {
			if event.Op != fsnotify.Write {
				continue
			}

			name := strings.ToLower(event.Name)
			if strings.HasSuffix(name, "yml") || strings.HasSuffix(name, "yaml") {
				logs <- logg.Infof("config change detected: %s", event.Name)
				change <- true
			}
		}
	}()

	return change
}
