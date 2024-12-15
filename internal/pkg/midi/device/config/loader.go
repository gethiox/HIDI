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

func LoadDeviceConfigs(ctx context.Context, wg *sync.WaitGroup) (DeviceConfigs, error) {
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

	for _, pair := range []dirInfo{
		{factoryGamepad, cfg.Factory.Gamepads, "factory"},
		{factoryKeyboard, cfg.Factory.Keyboards, "factory"},
		{userGamepad, cfg.User.Gamepads, "user"},
		{userKeyboard, cfg.User.Keyboards, "user"},
	} {
		err := loadDirectory(pair.root, pair.identifier, pair.configMap)

		if err != nil {
			return cfg, fmt.Errorf("loading \"%s\" directory failed: %w", pair.root, err)
		}
	}
	return cfg, nil
}

func loadDirectory(root, configType string, configMap ConfigMap) (err error) {
	err = filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())

		if !strings.HasSuffix(name, ".toml") {
			return nil
		}

		devCfg, err := readDeviceConfig(path, configType)
		if err != nil {
			log.Info(fmt.Sprintf("device config %s (%s) load failed: %s", name, configType, err), logger.Warning)
			return nil
		}
		configMap[devCfg.Config.ID] = devCfg

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk failed: %w", err)
	}
	return nil
}
