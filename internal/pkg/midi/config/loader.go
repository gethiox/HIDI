package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi/config/validate"
)

const (
	factoryGamepad  = "hidi-config/factory/gamepad"
	factoryKeyboard = "hidi-config/factory/keyboard"
	userGamepad     = "hidi-config/user/gamepad"
	userKeyboard    = "hidi-config/user/keyboard"
)

var (
	UnsupportedDeviceType = errors.New("unsupported device type")
)

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
		cfg, ok = c.User.Keyboards[input.InputID{}] // picking user default if exist
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
		cfg, ok = c.User.Gamepads[input.InputID{}] // picking user default if exist
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

	return DeviceConfig{}, fmt.Errorf("%w: %s", UnsupportedDeviceType, devType)
}

type dirInfo struct {
	root       string
	configMap  ConfigMap
	identifier string
}

func LoadDeviceConfigs(ctx context.Context, wg *sync.WaitGroup, configNotifier chan<- validate.NotifyMessage) (DeviceConfigs, error) {
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

	var userFails int

	for _, pair := range []dirInfo{
		{factoryGamepad, cfg.Factory.Gamepads, "factory"},
		{factoryKeyboard, cfg.Factory.Keyboards, "factory"},
		{userGamepad, cfg.User.Gamepads, "user"},
		{userKeyboard, cfg.User.Keyboards, "user"},
	} {
		err, fails, _ := loadDirectory(pair.root, pair.identifier, pair.configMap)

		if pair.identifier == "user" {
			userFails += fails
		}

		if err != nil {
			return cfg, fmt.Errorf("loading \"%s\" directory failed: %w", pair.root, err)
		}
	}

	validate.ValidateConfig(ctx, wg, configNotifier, userFails)

	return cfg, nil
}

func loadDirectory(root, configType string, configMap ConfigMap) (err error, fails, success int) {
	err = filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())

		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			devCfg, err := readDeviceConfig(path, configType)
			if err != nil {
				log.Info(fmt.Sprintf("device config %s load failed: %s", name, err), logger.Warning)
				fails++
				return nil
			}
			success++
			configMap[devCfg.ID] = devCfg
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk failed: %w", err), fails, success
	}
	return nil, fails, success
}
