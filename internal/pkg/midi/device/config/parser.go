package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	path2 "path"
	"strconv"
	"strings"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/holoplot/go-evdev"
	"github.com/pelletier/go-toml/v2"
	"github.com/realbucksavage/openrgb-go"
)

type GyroDescToml struct {
	Axis                string  `toml:"axis"`
	Type                string  `toml:"type"`
	CC                  int     `toml:"cc"`
	ActivationKey       string  `toml:"activation_key"`
	ActivationMode      string  `toml:"activation_mode"`
	ResetOnDeactivation bool    `toml:"reset_on_deactivation"`
	FlipAxis            bool    `toml:"flip_axis"`
	ValueMultiplier     float64 `toml:"value_multiplier"`
}

type TOMLDeviceConfig struct {
	CollisionMode string   `toml:"collision_mode"`
	ExitSequence  []string `toml:"exit_sequence"`

	Identifier struct {
		Bus     uint16 `toml:"bus"`
		Vendor  uint16 `toml:"vendor"`
		Product uint16 `toml:"product"`
		Version uint16 `toml:"version"`
		Uniq    string `toml:"uniq"`
	} `toml:"identifier"`

	Defaults struct {
		Octave   int    `toml:"octave"`
		Semitone int    `toml:"semitone"`
		Channel  int    `toml:"channel"`
		Mapping  string `toml:"mapping"`
	} `toml:"defaults"`

	ActionMapping map[string]string `toml:"action_mapping"`

	Gyro []GyroDescToml `toml:"gyro"`

	OpenRGB struct {
		White          int `toml:"white"`
		Black          int `toml:"black"`
		C              int `toml:"c"`
		Unavailable    int `toml:"unavailable"`
		Other          int `toml:"other"`
		Active         int `toml:"active"`
		ActiveExternal int `toml:"active_external"`
	} `toml:"open_rgb"`

	Deadzone struct {
		Default   float64            `toml:"default"`
		Deadzones map[string]float64 `toml:"deadzones"`
	} `toml:"deadzone,omitempty"`

	KeyMappings []struct {
		Name          string            `toml:"name"`
		KeyMapping    map[string]string `toml:"keys"`
		AnalogMapping map[string]struct {
			Type                  string  `toml:"type"`
			CC                    *int    `toml:"cc,omitempty"`
			CCNegative            *int    `toml:"cc_negative,omitempty"`
			Note                  *int    `toml:"note,omitempty"`
			NoteNegative          *int    `toml:"note_negative,omitempty"`
			ChannelOffset         int     `toml:"channel_offset"`
			ChannelOffsetNegative int     `toml:"channel_offset_negative"`
			Action                *string `toml:"action,omitempty"`
			ActionNegative        *string `toml:"action_negative,omitempty"`
			FlipAxis              bool    `toml:"flip_axis"`
		} `toml:"analog,omitempty"`
	} `toml:"mapping"`
}

type YamlDeviceConfig struct {
	Identifier struct {
		Bus     uint16 `yaml:"bus"`
		Vendor  uint16 `yaml:"vendor"`
		Product uint16 `yaml:"product"`
		Version uint16 `yaml:"version"`
		Uniq    string `yaml:"uniq"`
	} `yaml:"identifier"`

	Defaults struct {
		Octave   int    `yaml:"octave"`
		Semitone int    `yaml:"semitone"`
		Channel  int    `yaml:"channel"`
		Mapping  string `yaml:"mapping"`
	} `yaml:"defaults"`

	CollisionMode   string                         `yaml:"collision_mode"`
	Deadzones       map[string]float64             `yaml:"deadzones"`
	DefaultDeadzone float64                        `yaml:"default_deadzone"`
	ActionMapping   map[string]string              `yaml:"action_mapping"`
	KeyMappings     []map[string]map[string]string `yaml:"midi_mappings"`
	ExitSequence    []string                       `yaml:"exit_sequence"`

	OpenRGB struct {
		Colors struct {
			White          int `yaml:"white"`
			Black          int `yaml:"black"`
			C              int `yaml:"c"`
			Unavailable    int `yaml:"unavailable"`
			Other          int `yaml:"other"`
			Active         int `yaml:"active"`
			ActiveExternal int `yaml:"active_external"`
		} `yaml:"colors"`
	} `yaml:"open_rgb"`
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
	Config     Config
}

func TomlKeyToEvCode(key string, lookupTable map[string]evdev.EvCode) (evdev.EvCode, error) {
	if strings.HasPrefix(key, "x") {
		keyTrimmed := strings.TrimPrefix(key, "x")
		evcode, err := strconv.ParseUint(keyTrimmed, 16, 16)
		if err != nil {
			return evdev.EvCode(0), fmt.Errorf("convertion hex value \"%s\" failed: %w", keyTrimmed, err)
		}
		return evdev.EvCode(evcode), nil
	}

	evcode, ok := lookupTable[key]
	if !ok {
		return evdev.EvCode(0), fmt.Errorf("EvCode name \"%s\" not found / not supported")
	}
	return evcode, nil

}

func ParseData(data []byte) (Config, error) {
	cfg := TOMLDeviceConfig{}

	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()

	err := d.Decode(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing yaml failed: %w", err)
	}

	var keyMapping []KeyMapping
	var actionMapping = make(map[evdev.EvCode]Action)
	var deadzones = make(map[evdev.EvCode]float64)

	for _, mapping := range cfg.KeyMappings {
		// for name, mappingRaw := range mappings
		name := mapping.Name
		var midiMapping = make(map[evdev.EvCode]Key)
		var analogMapping = make(map[evdev.EvCode]Analog)

		for evcodeRaw, valueRaw := range mapping.KeyMapping {
			evcode, err := TomlKeyToEvCode(evcodeRaw, evdev.KEYFromString)
			if err != nil {
				return Config{}, fmt.Errorf("[%s] %s: failed to parse evcode key: %w", name, evcodeRaw, err)
			}

			noteAndOffset := strings.Split(valueRaw, ",")
			var noteRaw, offsetRaw string
			switch len(noteAndOffset) {
			case 1:
				noteRaw = noteAndOffset[0]
				offsetRaw = "0"
			case 2:
				noteRaw = noteAndOffset[0]
				offsetRaw = noteAndOffset[1]
			default:
				return Config{}, fmt.Errorf("[%s] %s: unsupported comma-separated field count: %d (expected 1 or 2)", name, evcodeRaw, len(noteAndOffset))
			}

			offsetInt, err := strconv.Atoi(offsetRaw)
			if err != nil {
				return Config{}, fmt.Errorf("[%s] %s: failed to parse channel offset value", name, evcodeRaw)
			}
			if offsetInt < 0 || offsetInt > 15 {
				return Config{}, fmt.Errorf("[%s] %s: channel offset outside of 0-15 range", name, evcodeRaw)
			}

			noteInt, err := strconv.Atoi(noteRaw)
			if err == nil {
				if noteInt < 0 || noteInt > 127 {
					return Config{}, fmt.Errorf("[%s] %s: note value outside of 0-127 range: %d", name, evcodeRaw, noteInt)
				}
				midiMapping[evcode] = Key{Note: byte(noteInt), ChannelOffset: byte(offsetInt)}
				continue
			}

			note, err := StringToNote(noteRaw)
			if err == nil {
				midiMapping[evcode] = Key{Note: note, ChannelOffset: byte(offsetInt)}
				continue
			}
			return Config{}, fmt.Errorf("[%s] %s: failed to parse note: %v", name, evcodeRaw, err)
		}

		for evcodeRaw, analog := range mapping.AnalogMapping {
			evcode, err := TomlKeyToEvCode(evcodeRaw, evdev.ABSFromString)
			if err != nil {
				return Config{}, fmt.Errorf("[%s] %s: failed to parse evcode key: %w", name, evcodeRaw, err)
			}

			mappingType := MappingType(analog.Type)
			if !SupportedMappingTypes[mappingType] {
				return Config{}, fmt.Errorf("[%s] %s: mapping type not supported: %s", name, evcodeRaw, analog.Type)
			}

			switch mappingType {
			case AnalogCC:
				var bidirectional bool
				var CC, CCNeg uint8

				if analog.CC == nil {
					return Config{}, fmt.Errorf("[%s] %s: cc value not set", name, evcodeRaw)
				}

				if *analog.CC < 0 || *analog.CC > 119 {
					return Config{}, fmt.Errorf("[%s] %s: cc value outside of 0-119 range: %d", name, evcodeRaw, *analog.CC)
				}

				CC = byte(*analog.CC)

				if analog.CCNegative != nil {
					if *analog.CCNegative < 0 || *analog.CCNegative > 119 {
						return Config{}, fmt.Errorf("[%s] %s: cc value outside of 0-119 range: %d", name, evcodeRaw, *analog.CCNegative)
					}
					CCNeg = byte(*analog.CCNegative)
					bidirectional = true
				}

				analogMapping[evcode] = Analog{
					MappingType:      mappingType,
					CC:               CC,
					CCNeg:            CCNeg,
					ChannelOffset:    byte(analog.ChannelOffset),
					ChannelOffsetNeg: byte(analog.ChannelOffsetNegative),
					FlipAxis:         analog.FlipAxis,
					Bidirectional:    bidirectional,
				}
			case AnalogPitchBend:
				analogMapping[evcode] = Analog{
					MappingType:   mappingType,
					FlipAxis:      analog.FlipAxis,
					ChannelOffset: byte(analog.ChannelOffset),
				}
			case AnalogActionSim:
				var bidirectional bool

				if analog.Action == nil {
					return Config{}, fmt.Errorf("[%s] %s: action value not set", name, evcodeRaw)
				}

				action := Action(*analog.Action)
				if !SupportedActions[action] {
					return Config{}, fmt.Errorf("[%s] %s: action not supported: %s", name, evcodeRaw, *analog.Action)
				}

				var actionNegative Action

				if analog.Action != nil {
					actionNegative = Action(*analog.ActionNegative)
					if !SupportedActions[action] {
						return Config{}, fmt.Errorf("[%s] %s: action not supported: %s", name, evcodeRaw, *analog.ActionNegative)
					}
					bidirectional = true
				}

				analogMapping[evcode] = Analog{
					MappingType:   mappingType,
					Action:        action,
					ActionNeg:     actionNegative,
					FlipAxis:      analog.FlipAxis,
					Bidirectional: bidirectional,
				}

			case AnalogKeySim:
				var bidirectional bool
				var note, noteNeg uint8

				if analog.Note == nil {
					return Config{}, fmt.Errorf("[%s] %s: note value not set", name, evcodeRaw)
				}

				if *analog.Note < 0 || *analog.Note > 127 {
					return Config{}, fmt.Errorf("[%s] %s: note value outside of 0-127 range: %d", name, evcodeRaw, *analog.Note)
				}

				note = byte(*analog.Note)

				if analog.NoteNegative != nil {
					if *analog.NoteNegative < 0 || *analog.NoteNegative > 127 {
						return Config{}, fmt.Errorf("[%s] %s: note value outside of 0-127 range: %d", name, evcodeRaw, *analog.NoteNegative)
					}
					noteNeg = byte(*analog.Note)
					bidirectional = true
				}

				analogMapping[evcode] = Analog{
					MappingType:   mappingType,
					Note:          note,
					NoteNeg:       noteNeg,
					FlipAxis:      analog.FlipAxis,
					Bidirectional: bidirectional,
				}
			default:
				return Config{}, fmt.Errorf("[%s] %s: unexpected mapping type: %s", name, evcodeRaw, mappingType)
			}
		}

		keyMapping = append(keyMapping, KeyMapping{
			Name:   name,
			Midi:   midiMapping,
			Analog: analogMapping,
		})

	}

	for evcodeRaw, actionRaw := range cfg.ActionMapping {
		evcode, err := TomlKeyToEvCode(evcodeRaw, evdev.KEYFromString)
		if err != nil {
			return Config{}, fmt.Errorf("[actions] %w", err)
		}
		action := Action(actionRaw)
		if !SupportedActions[action] {
			return Config{}, fmt.Errorf("[actions] unsupported action: %s", action)
		}
		actionMapping[evcode] = action
	}

	for evcodeRaw, value := range cfg.Deadzone.Deadzones {
		evcode, err := TomlKeyToEvCode(evcodeRaw, evdev.ABSFromString)
		if err != nil {
			return Config{}, fmt.Errorf("[deadzones] %w", err)
		}
		deadzones[evcode] = value
	}

	collisionMode := CollisionMode(cfg.CollisionMode)
	if !SupportedCollisionModes[collisionMode] {
		return Config{}, fmt.Errorf("[collision_mode] unsupported collision_mode: %s", collisionMode)
	}

	var mappingIndex = -1
	for i, mapping := range keyMapping {
		if mapping.Name == cfg.Defaults.Mapping {
			mappingIndex = i
		}
	}
	if mappingIndex == -1 {
		return Config{}, fmt.Errorf("default mapping \"%s\" not found", cfg.Defaults.Mapping)
	}

	var exitSequence []evdev.EvCode
	for _, key := range cfg.ExitSequence {
		evcode, err := TomlKeyToEvCode(key, evdev.KEYFromString)
		if err != nil {
			return Config{}, fmt.Errorf("[exit_sequence] %w", err)
		}
		exitSequence = append(exitSequence, evcode)
	}

	var gyro = make(map[evdev.EvCode][]GyroDesc)
	var axisStringToIdx = map[string]int{"x": 0, "y": 1, "z": 2}

	for _, desc := range cfg.Gyro {
		mappingType := MappingType(desc.Type)
		if !SupportedGyroMappingTypes[mappingType] {
			return Config{}, fmt.Errorf("[gyro] unsupported type: %s", desc.Type)
		}

		switch mappingType {
		case AnalogPitchBend:
			break
		case AnalogCC:
			if desc.CC < 0 || desc.CC > 119 {
				return Config{}, fmt.Errorf("[gyro] cc value outside of 0-119 range: %d", desc.CC)
			}
		default:
			return Config{}, fmt.Errorf("[gyro] unexpected mapping type: %s", mappingType)
		}

		activationKey, ok := evdev.KEYFromString[desc.ActivationKey]
		if !ok {
			return Config{}, fmt.Errorf("[gyro] EvCode %s not exist", desc.ActivationKey)
		}

		activationMode := GyroMode(desc.ActivationMode)
		if !SupportedGyroActivationTypes[activationMode] {
			return Config{}, fmt.Errorf("[gyro] not supported activation mode: %s", desc.ActivationMode)
		}

		axis, ok := axisStringToIdx[desc.Axis]
		if !ok {
			return Config{}, fmt.Errorf("[gyro] incorrect axis: %s", desc.Axis)
		}

		gyroDesc := GyroDesc{
			Axis:                axis,
			Type:                mappingType,
			CC:                  desc.CC,
			ActivationMode:      activationMode,
			ResetOnDeactivation: desc.ResetOnDeactivation,
			FlipAxis:            desc.FlipAxis,
			ValueMultiplier:     desc.ValueMultiplier,
		}

		_, ok = gyro[activationKey]
		if !ok {
			gyro[activationKey] = []GyroDesc{gyroDesc}
		} else {
			gyro[activationKey] = append(gyro[activationKey], gyroDesc)
		}
	}

	convertToColor := func(v int) openrgb.Color {
		return openrgb.Color{
			Red:   byte(v >> 16),
			Green: byte(v >> 8),
			Blue:  byte(v),
		}
	}

	devConfig := Config{
		ID: input.InputID{
			Bus:     cfg.Identifier.Bus,
			Vendor:  cfg.Identifier.Vendor,
			Product: cfg.Identifier.Product,
			Version: cfg.Identifier.Version,
		},
		Uniq:          cfg.Identifier.Uniq,
		KeyMappings:   keyMapping,
		ActionMapping: actionMapping,
		ExitSequence:  exitSequence,
		Deadzone: Deadzone{
			Deadzones: deadzones,
			Default:   cfg.Deadzone.Default,
		},
		CollisionMode: collisionMode,
		Defaults: Defaults{
			Octave:   cfg.Defaults.Octave,
			Semitone: cfg.Defaults.Semitone,
			Channel:  cfg.Defaults.Channel,
			Mapping:  mappingIndex,
		},
		OpenRGB: OpenRGB{
			Colors: Colors{
				White:          convertToColor(cfg.OpenRGB.White),
				Black:          convertToColor(cfg.OpenRGB.Black),
				C:              convertToColor(cfg.OpenRGB.C),
				Unavailable:    convertToColor(cfg.OpenRGB.Unavailable),
				Other:          convertToColor(cfg.OpenRGB.Other),
				Active:         convertToColor(cfg.OpenRGB.Active),
				ActiveExternal: convertToColor(cfg.OpenRGB.ActiveExternal),
			},
		},
		Gyro: gyro,
	}
	return devConfig, nil
}

func readDeviceConfig(path, configType string) (DeviceConfig, error) {
	fd, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("opening config file failed: %w", err)
	}
	defer fd.Close()

	data, err := io.ReadAll(fd)
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("reading file data failed: %w", err)
	}

	conf, err := ParseData(data)
	if err != nil {
		return DeviceConfig{}, err
	}

	return DeviceConfig{
		ConfigFile: path2.Base(path),
		ConfigType: configType,
		Config:     conf,
	}, nil
}
