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
		Velocity int    `toml:"velocity"`
	} `toml:"defaults"`

	ActionMapping map[string]string `toml:"action_mapping"`

	OpenRGB struct {
		White          int `toml:"white"`
		Black          int `toml:"black"`
		C              int `toml:"c"`
		Unavailable    int `toml:"unavailable"`
		Other          int `toml:"other"`
		Active         int `toml:"active"`
		ActiveExternal int `toml:"active_external"`
	} `toml:"open_rgb"`

	KeyMappings []struct {
		Name       string `toml:"name"`
		KeyMapping []struct {
			SubHandler string            `toml:"subhandler"`
			Map        map[string]string `toml:"map"`
		} `toml:"keys"`
		AnalogMapping []struct {
			SubHandler      string  `toml:"subhandler"`
			DefaultDeadzone float64 `toml:"default_deadzone,omitempty"`
			Map             map[string]struct {
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
				DeadzoneAtCenter      bool    `toml:"deadzone_at_center,omitempty"`
			} `toml:"map"`
			Deadzones map[string]float64 `toml:"deadzones,omitempty"`
		} `toml:"analog,omitempty"`
	} `toml:"mapping"`
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
		return evdev.EvCode(0), fmt.Errorf("EvCode name \"%s\" not found / not supported", key)
	}
	return evcode, nil

}

func ParseData(data []byte) (Config, error) {
	cfg := TOMLDeviceConfig{}

	d := toml.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()

	err := d.Decode(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing failed: %w", err)
	}

	var keyMapping []KeyMapping
	var actionMapping = make(map[evdev.EvCode]Action)

	for _, mapping := range cfg.KeyMappings {
		name := mapping.Name
		var midiMapping = make(map[string]map[evdev.EvCode]Key)

		for _, subMapping := range mapping.KeyMapping {
			midiMappingTmp := make(map[evdev.EvCode]Key)

			for evcodeRaw, valueRaw := range subMapping.Map {
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
					midiMappingTmp[evcode] = Key{Note: byte(noteInt), ChannelOffset: byte(offsetInt)}
					continue
				}

				note, err := StringToNote(noteRaw)
				if err == nil {
					midiMappingTmp[evcode] = Key{Note: note, ChannelOffset: byte(offsetInt)}
					continue
				}
				return Config{}, fmt.Errorf("[%s] %s: failed to parse note: %v", name, evcodeRaw, err)
			}

			if len(midiMappingTmp) > 0 {
				midiMapping[subMapping.SubHandler] = midiMappingTmp
			}
		}

		var analogMapping = make(map[string]map[evdev.EvCode]Analog)
		var deadzones = make(map[string]map[evdev.EvCode]float64)
		var defaultDeadzone = make(map[string]float64)

		for _, subMapping := range mapping.AnalogMapping {
			var analogMappingTmp = make(map[evdev.EvCode]Analog)
			var deadzonesTmp = make(map[evdev.EvCode]float64)

			for evcodeRaw, analog := range subMapping.Map {
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

					analogMappingTmp[evcode] = Analog{
						MappingType:      mappingType,
						CC:               CC,
						CCNeg:            CCNeg,
						ChannelOffset:    byte(analog.ChannelOffset),
						ChannelOffsetNeg: byte(analog.ChannelOffsetNegative),
						FlipAxis:         analog.FlipAxis,
						Bidirectional:    bidirectional,
						DeadzoneAtCenter: analog.DeadzoneAtCenter,
					}
				case AnalogPitchBend:
					analogMappingTmp[evcode] = Analog{
						MappingType:      mappingType,
						FlipAxis:         analog.FlipAxis,
						ChannelOffset:    byte(analog.ChannelOffset),
						DeadzoneAtCenter: analog.DeadzoneAtCenter,
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

					analogMappingTmp[evcode] = Analog{
						MappingType:      mappingType,
						Action:           action,
						ActionNeg:        actionNegative,
						FlipAxis:         analog.FlipAxis,
						Bidirectional:    bidirectional,
						DeadzoneAtCenter: analog.DeadzoneAtCenter,
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

					analogMappingTmp[evcode] = Analog{
						MappingType:      mappingType,
						Note:             note,
						NoteNeg:          noteNeg,
						FlipAxis:         analog.FlipAxis,
						Bidirectional:    bidirectional,
						DeadzoneAtCenter: analog.DeadzoneAtCenter,
					}
				default:
					return Config{}, fmt.Errorf("[%s] %s: unexpected mapping type: %s", name, evcodeRaw, mappingType)
				}
			}

			for evcodeRaw, value := range subMapping.Deadzones {
				evcode, err := TomlKeyToEvCode(evcodeRaw, evdev.ABSFromString)
				if err != nil {
					return Config{}, fmt.Errorf("[deadzones] %w", err)
				}
				deadzonesTmp[evcode] = value
			}

			analogMapping[subMapping.SubHandler] = analogMappingTmp
			deadzones[subMapping.SubHandler] = deadzonesTmp
			defaultDeadzone[subMapping.SubHandler] = subMapping.DefaultDeadzone
		}

		keyMapping = append(keyMapping, KeyMapping{
			Name:            name,
			Midi:            midiMapping,
			Analog:          analogMapping,
			Deadzones:       deadzones,
			DefaultDeadzone: defaultDeadzone,
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

	if cfg.Defaults.Velocity < 0 || cfg.Defaults.Velocity > 127 {
		return Config{}, fmt.Errorf("velocity \"%d\" not in 1-127 range", cfg.Defaults.Velocity)
	}
	velocity := cfg.Defaults.Velocity
	if velocity == 0 {
		velocity = 64
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
		CollisionMode: collisionMode,
		Defaults: Defaults{
			Octave:   cfg.Defaults.Octave,
			Semitone: cfg.Defaults.Semitone,
			Channel:  cfg.Defaults.Channel,
			Mapping:  mappingIndex,
			Velocity: velocity,
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
