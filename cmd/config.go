package main

import (
	"os"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/display"

	"github.com/d2r2/go-hd44780"
	"github.com/go-ini/ini"
)

type HIDI struct {
	EVThrottling        time.Duration
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
