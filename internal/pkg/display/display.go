package display

import (
	"fmt"
	"time"

	"hidi/internal/pkg/midi"

	device "github.com/d2r2/go-hd44780"
	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
)

func getDisplay(addr uint8, bus int, lcdType device.LcdType) (*device.Lcd, error) {
	logger.ChangePackageLogLevel("i2c", logger.InfoLevel)

	lcdRaw, err := i2c.NewI2C(addr, bus)
	if err != nil {
		return nil, err
	}

	lcd, err := device.NewLcd(lcdRaw, lcdType)
	if err != nil {
		return nil, err
	}

	return lcd, nil
}

func HandleDisplay(cfg midi.HIDIConfig, devices map[*midi.Device]*midi.Device, midiEventCounter *uint16) {
	if !cfg.Screen.Enabled {
		return
	}

	lcd, err := getDisplay(cfg.Screen.Address, cfg.Screen.Bus, cfg.Screen.LcdType)
	if err != nil {
		return
	}

	var barChars = [][]byte{
		{
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b11111,
		}, {
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b11111,
			0b11111,
		}, {
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b11111,
			0b11111,
			0b11111,
		}, {
			0b00000,
			0b00000,
			0b00000,
			0b00000,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
		}, {
			0b00000,
			0b00000,
			0b00000,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
		}, {
			0b00000,
			0b00000,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
		}, {
			0b00000,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
		}, {
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
			0b11111,
		},
	}

	lcd.TestWriteCGRam()

	for i, char := range barChars {
		var location = uint8(i) & 0x7

		lcd.Command(device.CMD_CGRAM_Set | (location << 3))
		lcd.Write(char)
	}

	lcd.BacklightOn()
	lcd.Clear()

	lastMidiEventsEmitted := *midiEventCounter

	var graph []uint16
	var graphPointer int
	for i := 0; i < 20; i++ {
		graph = append(graph, 0)
	}

	ls := time.Now().Second()

	for {
		t := time.Now()
		if t.Second() == ls {
			time.Sleep(time.Millisecond * 10)
			continue
		}
		ls = t.Second()

		var devCount = len(devices)
		var eventsPerSecond uint16

		if lastMidiEventsEmitted > *midiEventCounter {
			eventsPerSecond = (0xffff - lastMidiEventsEmitted) + *midiEventCounter
		} else {
			eventsPerSecond = *midiEventCounter - lastMidiEventsEmitted
		}
		lastMidiEventsEmitted = *midiEventCounter

		graph[graphPointer] = eventsPerSecond
		if graphPointer < 19 {
			graphPointer++
		} else {
			graphPointer = 0
		}

		var handlerCount int

		for _, midiDev := range devices {
			handlerCount += len(midiDev.InputDevice.Handlers)
		}

		lcd.Home()
		lcd.SetPosition(0, 0)
		fmt.Fprintf(lcd, "devices: %11d", devCount)
		lcd.SetPosition(1, 0)
		fmt.Fprintf(lcd, "handlers: %10d", handlerCount)
		lcd.SetPosition(2, 0)
		fmt.Fprintf(lcd, "events/s: %10d", eventsPerSecond)
		lcd.SetPosition(3, 0)
		var maxGraph uint16
		for _, graphVal := range graph {
			if graphVal > maxGraph {
				maxGraph = graphVal
			}
		}
		if maxGraph < 8 {
			maxGraph = 8
		}

		for i := 0; i < 20; i++ {
			index := (graphPointer + i) % 20
			graphVal := graph[index]
			if graphVal == 0 {
				lcd.Write([]byte{' '})
				continue
			}

			realVal := float64(graphVal) / (float64(maxGraph) + 1) * 7

			lcd.Write([]byte{byte(realVal)})
		}
	}
}
