package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/input"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/pelletier/go-toml/v2"
)

type HIDI struct {
	EVThrottling        time.Duration
	DiscoveryRate       time.Duration
	StabilizationPeriod time.Duration
}

type HIDIConfig struct {
	HIDI HIDI
}

type HIDIConfigRaw struct {
	HIDI struct {
		PoolRate            int `toml:"pool_rate"`
		DiscoveryRate       int `toml:"discovery_rate"`
		StabilizationPeriod int `toml:"stabilization_period"`
		LogViewRate         int `toml:"log_view_rate"`
		LogBufferSize       int `toml:"log_buffer_size"`
	} `toml:"HIDI"`
}

func LoadHIDIConfig(path string) (HIDIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HIDIConfig{}, fmt.Errorf("cannot read \"%s\" file: %w", path, err)
	}

	var rawConfig HIDIConfigRaw
	err = toml.Unmarshal(data, &rawConfig)
	if err != nil {
		return HIDIConfig{}, err
	}

	var config HIDIConfig

	config.HIDI.EVThrottling = time.Second / time.Duration(rawConfig.HIDI.PoolRate)
	config.HIDI.DiscoveryRate = time.Second / time.Duration(rawConfig.HIDI.DiscoveryRate)
	config.HIDI.StabilizationPeriod = time.Millisecond * time.Duration(rawConfig.HIDI.StabilizationPeriod)

	return config, err
}

//go:embed hidi-config/hidi.toml
//go:embed "hidi-config/device blacklist.txt"
//go:embed hidi-config/*/*/*
//go:embed hidi-config/factory/README
//go:embed hidi-config/user/README.md
var templateConfig embed.FS

const configDir = "hidi-config"

// createConfigDirectory creates config directory if necessary.
// It also updates Factory device configs, hidi.toml stays intact.
func updateHIDIConfiguration() error {
	cdir, err := os.OpenFile(configDir, os.O_RDONLY, 0)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot open config directory: %w", err)
		}
		log.Info("config not exist, generating tree...", logger.Info)

		// create config subdirectories and files
		err = fs.WalkDir(templateConfig, configDir, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				err := os.Mkdir(path, 0o777)
				if err != nil {
					return fmt.Errorf("cannot create \"%s\" directory: %w", path, err)
				}
				return nil
			}

			dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
			if err != nil {
				return fmt.Errorf("cannot open \"%s\" file: %w", path, err)
			}
			defer dst.Close()

			data, err := fs.ReadFile(templateConfig, path)
			if err != nil {
				return fmt.Errorf("cannot read \"%s\" template file: %w", path, err)
			}

			_, err = dst.Write(data)
			if err != nil {
				return fmt.Errorf("cannot write data into \"%s\" file: %w", path, err)
			}

			log.Info(fmt.Sprintf("Created \"%s\" file", path), logger.Debug)
			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		log.Info("config generation done", logger.Info)

		return nil
	}
	cdir.Close()

	// update factory configs
	err = fs.WalkDir(templateConfig, configDir+"/factory", func(path string, entry fs.DirEntry, err error) error {
		if entry.IsDir() {
			_, err := os.Stat(path)
			if err == nil {
				return nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("unexpected error when reading \"%s\" directory: %w", path, err)
			}
			// ensure directories exists
			err = os.Mkdir(path, 0o777)
			if err != nil {
				return fmt.Errorf("cannot create \"%s\" directory: %w", path, err)
			}
			return nil
		}
		src, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("cannot open \"%s\" file: %w", path, err)
			}
			// factory file does not exist
			log.Info(fmt.Sprintf("Creating new factory configuration: \"%s\"", path), logger.Debug)
			fd, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
			if err != nil {
				return fmt.Errorf("cannot open \"%s\" file for writing: %w", path, err)
			}
			defer fd.Close()

			data, err := fs.ReadFile(templateConfig, path)
			if err != nil {
				return fmt.Errorf("cannot read \"%s\" file template: %w", path, err)
			}

			_, err = fd.Write(data)
			if err != nil {
				return fmt.Errorf("cannot write data into \"%s\" file: %w", path, err)
			}
			return nil
		}
		defer src.Close()

		// factory file exist, overwriting
		data, err := io.ReadAll(src)
		if err != nil {
			return fmt.Errorf("cannot read \"%s\" file template: %w", path, err)
		}

		newData, err := fs.ReadFile(templateConfig, path)
		if err != nil {
			return fmt.Errorf("cannot open \"%s\" file template: %w", path, err)
		}

		if bytes.Equal(data, newData) {
			log.Info(fmt.Sprintf("File \"%s\" not changed", path), logger.Debug)
			return nil
		}
		log.Info(fmt.Sprintf("File \"%s\" changed, replacing data...", path), logger.Debug)
		dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if err != nil {
			return fmt.Errorf("cannot open \"%s\" file: %w", path, err)
		}
		defer dst.Close()

		_, err = dst.Write(newData)
		if err != nil {
			return fmt.Errorf("cannot overwrite \"%s\" file: %w", path, err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("update factory configs failed: %w", err)
	}

	// create device blacklist.txt if does not exist.
	blacklistPath := configDir + "/device blacklist.txt"
	fd, err := os.OpenFile(blacklistPath, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		dst, err := os.OpenFile(blacklistPath, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			return fmt.Errorf("cannot open \"device blacklist.txt\": %w", err)
		}
		defer dst.Close()

		data, err := fs.ReadFile(templateConfig, blacklistPath)
		if err != nil {
			return fmt.Errorf("cannot read \"%s\" template file: %w", blacklistPath, err)
		}

		_, err = dst.Write(data)
		if err != nil {
			return fmt.Errorf("cannot write data into \"%s\" file: %w", blacklistPath, err)
		}

		log.Info(fmt.Sprintf("Created \"%s\" file", blacklistPath), logger.Debug)
		return nil
	}
	fd.Close()

	return nil
}

var regex = regexp.MustCompile(
	`(?i)Bus: 0x([0-9a-f]{4}), Vendor: 0x([0-9a-f]{4}), Product: 0x([0-9a-f]{4}), Version: 0x([0-9a-f]{4})`,
)

func parseDeviceBlacklist() ([]input.InputID, error) {
	data, err := os.ReadFile(configDir + "/device blacklist.txt")
	if err != nil {
		return nil, fmt.Errorf("cannot read \"%s\" file: %w", configDir+"/device blacklist.txt", err)
	}
	text := string(data)

	var ids = make([]input.InputID, 0)
	for lineno, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			log.Info(fmt.Sprintf("[blacklist] ignoring commend under line %d", lineno+1), logger.Debug)
			continue
		}
		if line == "" {
			continue
		}

		result := regex.FindAllStringSubmatch(line, 1)

		if len(result) == 0 {
			log.Info(fmt.Sprintf("[blacklist] parsing failed for line %d", lineno+1), logger.Warning)
			continue
		}

		var busRaw, vendorRaw, productRaw, versionRaw = result[0][1], result[0][2], result[0][3], result[0][4]

		bus, err1 := strconv.ParseUint(busRaw, 16, 16)
		vendor, err2 := strconv.ParseUint(vendorRaw, 16, 16)
		product, err3 := strconv.ParseUint(productRaw, 16, 16)
		version, err4 := strconv.ParseUint(versionRaw, 16, 16)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			log.Info(fmt.Sprintf("[blacklist] converting hex values failed for line %d", lineno+1), logger.Warning)
			continue
		}

		ids = append(ids, input.InputID{
			Bus:     uint16(bus),
			Vendor:  uint16(vendor),
			Product: uint16(product),
			Version: uint16(version),
		})
	}
	return ids, nil
}

func loadDeviceBlacklist() ([]input.PhysicalID, error) {
	ids, err := parseDeviceBlacklist()
	if err != nil {
		return nil, fmt.Errorf("parsing \"device blacklist.txt\" failed: %w", err)
	}

	for _, id := range ids {
		log.Info(fmt.Sprintf("[blacklist] loaded device: %s", id.String()), logger.Info)
	}

	devs := collectDevices(time.Second)

	var pids = make([]input.PhysicalID, 0)

	for _, dev := range devs {
		for _, id := range ids {
			if dev.ID == id {
				pids = append(pids, dev.PhysicalUUID())
			}
		}
	}

	return pids, nil
}
