package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/d2r2/go-hd44780"
	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/go-ini/ini"
)

type HIDI struct {
	EVThrottling        time.Duration
	LogViewRate         time.Duration
	DiscoveryRate       time.Duration
	StabilizationPeriod time.Duration
}

type HIDIConfig struct {
	HIDI   HIDI
	Screen display.ScreenConfig
}

func LoadHIDIConfig(path string) HIDIConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	cfg, err := ini.Load(data)
	if err != nil {
		panic(err)
	}

	var c HIDIConfig

	// [HIDI]
	hidi, _ := cfg.GetSection("HIDI")
	evThrottling, _ := hidi.GetKey("pool_rate")
	i, err := evThrottling.Int()
	if err != nil {
		panic(err)
	}
	c.HIDI.EVThrottling = time.Second / time.Duration(i)
	discoveryRate, _ := hidi.GetKey("discovery_rate")
	i, err = discoveryRate.Int()
	if err != nil {
		panic(err)
	}
	c.HIDI.DiscoveryRate = time.Second / time.Duration(i)

	stabilizationPeriod, _ := hidi.GetKey("stabilization_period")
	i, err = stabilizationPeriod.Int()
	if err != nil {
		panic(err)
	}
	c.HIDI.StabilizationPeriod = time.Millisecond * time.Duration(i)

	logViewRate, _ := hidi.GetKey("log_view_rate")
	i, err = logViewRate.Int()
	if err != nil {
		panic(err)
	}
	c.HIDI.LogViewRate = time.Second / time.Duration(i)

	// [screen]
	screen, _ := cfg.GetSection("screen")
	screenSupport, _ := screen.GetKey("enabled")
	screenType, _ := screen.GetKey("type")
	screenAddress, _ := screen.GetKey("address")
	screenBus, _ := screen.GetKey("bus")
	updateRate, _ := screen.GetKey("update_rate")
	message1, _ := screen.GetKey("exit_message1")
	message2, _ := screen.GetKey("exit_message2")
	message3, _ := screen.GetKey("exit_message3")
	message4, _ := screen.GetKey("exit_message4")

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

	i, err = screenBus.Int()
	if err != nil {
		panic(err)
	}
	c.Screen.Bus = i

	i, err = screenAddress.Int()
	if err != nil {
		panic(err)
	}
	c.Screen.Address = uint8(i)

	i, err = updateRate.Int()
	if err != nil {
		panic(err)
	}
	c.Screen.UpdateRate = i

	c.Screen.ExitMessage[0] = message1.String()
	c.Screen.ExitMessage[1] = message2.String()
	c.Screen.ExitMessage[2] = message3.String()
	c.Screen.ExitMessage[3] = message4.String()

	return c
}

//go:embed config/hidi.config
//go:embed config/*/*/*
var templateConfig embed.FS

// createConfigDirectory creates config directory if necessary.
// It also updates Factory device configs, hidi.config stays intact.
func createConfigDirectoryIfNeeded() {
	f, err := os.OpenFile("config", os.O_RDONLY, 0)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			panic(fmt.Errorf("cannot open config directory: %v", err))
		}
		log.Info("config not exist, generating tree...", logger.Info)

		// create config subdirectories and files
		err = fs.WalkDir(templateConfig, "config", func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				err := os.Mkdir(path, 0o777)
				if err != nil {
					panic(err)
				}
				return nil
			}

			fd, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
			if err != nil {
				panic(err)
			}
			defer fd.Close()

			data, err := fs.ReadFile(templateConfig, path)
			if err != nil {
				panic(err)
			}

			_, err = fd.Write(data)
			if err != nil {
				panic(err)
			}

			log.Info(fmt.Sprintf("Created \"%s\" file", path), logger.Debug)
			return nil
		})

		if err != nil {
			panic(err)
		}
	} else {
		f.Close()
		// update factory configs
		err = fs.WalkDir(templateConfig, "config/factory", func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				_, err := os.Stat(path)
				if err == nil {
					return nil
				}
				if !errors.Is(err, os.ErrNotExist) {
					panic(err)
				}
				// ensure directories exists
				err = os.Mkdir(path, 0o777)
				if err != nil {
					panic(err)
				}
				return nil
			}
			fd, err := os.OpenFile(path, os.O_RDONLY, 0)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					panic(fmt.Errorf("cannot open file: %v", err))
				}
				// factory file does not exist
				fd, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
				if err != nil {
					panic(err)
				}
				defer fd.Close()

				data, err := fs.ReadFile(templateConfig, path)
				if err != nil {
					panic(err)
				}

				_, err = fd.Write(data)
				if err != nil {
					panic(err)
				}
			}
			// factory file exist
			data, err := io.ReadAll(fd)
			if err != nil {
				fd.Close()
				panic(err)
			}
			fd.Close()

			newData, err := fs.ReadFile(templateConfig, path)
			if err != nil {
				panic(err)
			}

			if bytes.Equal(data, newData) {
				log.Info(fmt.Sprintf("File \"%s\" not changed", path), logger.Debug)
				return nil
			}
			log.Info(fmt.Sprintf("File \"%s\" changed, replacing data...", path), logger.Debug)
			fd, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
			if err != nil {
				panic(err)
			}
			defer fd.Close()

			_, err = fd.Write(newData)
			if err != nil {
				panic(err)
			}

			return nil
		})

		if err != nil {
			panic(err)
		}
	}
}
