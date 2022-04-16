package midi

import (
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

	"github.com/d2r2/go-hd44780"
	"github.com/fsnotify/fsnotify"
	"github.com/gethiox/go-evdev"
	"github.com/go-ini/ini"
	"gopkg.in/yaml.v3"
)

const (
	factoryGamepad  = "./config/factory/gamepad"
	factoryKeyboard = "./config/factory/keyboard"
	userGamepad     = "./config/user/gamepad"
	userKeyboard    = "./config/user/keyboard"
)

type HIDIConfig struct {
	Screen struct {
		Enabled bool
		LcdType hd44780.LcdType
		Bus     int
		Address uint8
	}
}

func NewHIDIConfig(path string) HIDIConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	cfg, err := ini.Load(data)
	if err != nil {
		panic(err)
	}

	var c HIDIConfig
	screen, _ := cfg.GetSection("screen")
	screenSupport, _ := screen.GetKey("enabled")
	screenType, _ := screen.GetKey("type")
	screenAddress, _ := screen.GetKey("address")
	screenBus, _ := screen.GetKey("bus")

	b, err := screenSupport.Bool()
	if err != nil {
		panic(err)
	}
	c.Screen.Enabled = b
	switch t := screenType.Value(); t {
	case "16x2":
		c.Screen.LcdType = hd44780.LCD_16x2
	case "20x4":
		c.Screen.LcdType = hd44780.LCD_20x4
	default:
		panic("oof")
	}

	i, err := screenBus.Int()
	if err != nil {
		panic(err)
	}
	c.Screen.Bus = i
	i, err = screenAddress.Int()
	if err != nil {
		panic(err)
	}
	c.Screen.Address = uint8(i)

	return c
}

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
	ID       string `yaml:"id"`
	CC       uint8  `yaml:"cc"`
	FlipAxis bool   `yaml:"flip_axis"`
}

type DeviceConfig struct {
	ConfigFile string
	ID         input.InputID
	Uniq       string
	Config     Config
}

// readDeviceConfig parses yaml file and provide ready to use DeviceConfig
func readDeviceConfig(path string) (DeviceConfig, error) {
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
							panic(fmt.Sprintf("note value outside of 0-127 range"))
						}
						midiMapping[evcode] = byte(noteInt)
						continue
					}

					note, err := StringToNote(valueRaw)
					if err == nil {
						if note < 0 || note > 127 {
							panic(fmt.Sprintf("note value outside of 0-127 range"))
						}
						midiMapping[evcode] = note
						continue
					}
					panic(fmt.Errorf("failed to parse note: %v", err))
				case isAbs:
					var mapping YamlAnalogMapping
					evcode := evdev.ABSFromString[evcodeRaw]

					err := yaml.Unmarshal([]byte(valueRaw), &mapping)
					if err != nil {
						panic(fmt.Sprintf("cannot unmarshal analog configuration for \"%s\" key: %v", evcodeRaw, err))
					}
					fmt.Printf("mapping raw: %s\n", valueRaw)

					fmt.Printf("mapping: %+v\n", mapping)
					analogMapping[evcode] = Analog{
						id:       NameToAnalogID[mapping.ID],
						cc:       mapping.CC,
						flipAxis: mapping.FlipAxis,
					}
				default:
					panic(fmt.Sprintf("unsupported value type of \"%s\" key: %s", evcodeRaw, valueRaw))
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
			fmt.Printf("Warning: evcode not found: %v\n", evcodeRaw)
			continue
		}
		action, ok := NameToAction[actionRaw]
		if !ok {
			fmt.Printf("Warning: action not found: %v\n", actionRaw)
			continue
		}

		actionMapping[evcode] = action
	}

	devConfig := DeviceConfig{
		ConfigFile: path2.Base(path),
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

	return DeviceConfig{}, fmt.Errorf("shiet aaa")
}

func loadDirectory(root string, configMap ConfigMap) error {
	err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())

		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			devCfg, err := readDeviceConfig(path)
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
	}{
		{factoryGamepad, cfg.Factory.Gamepads},
		{factoryKeyboard, cfg.Factory.Keyboards},
		{userGamepad, cfg.User.Gamepads},
		{userKeyboard, cfg.User.Keyboards},
	}

	for _, pair := range pairs {
		err := loadDirectory(pair.root, pair.configMap)
		if err != nil {
			return cfg, fmt.Errorf("loading \"%s\" directory failed: %w", pair.root, err)
		}
	}

	return cfg, nil
}

func DetectDeviceConfigChanges(logs chan logg.LogEntry) <-chan bool {
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
