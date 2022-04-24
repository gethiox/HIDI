package display

import (
	"context"
	"fmt"
	"sync"
	"time"

	device "github.com/d2r2/go-hd44780"
	"github.com/d2r2/go-i2c"
	shittyLogger "github.com/d2r2/go-logger"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
)

var log = logger.GetLogger()

func getDisplay(addr uint8, bus int, lcdType device.LcdType) (*device.Lcd, *i2c.I2C, error) {
	shittyLogger.ChangePackageLogLevel("i2c", shittyLogger.InfoLevel)

	lcdRaw, err := i2c.NewI2C(addr, bus)
	if err != nil {
		return nil, nil, err
	}

	lcd, err := device.NewLcd(lcdRaw, lcdType)
	if err != nil {
		return nil, lcdRaw, err
	}

	return lcd, lcdRaw, nil
}

func loadCustomCharacters(lcd *device.Lcd, characters [][]byte) {
	for i, char := range characters {
		var location = uint8(i) & 0x7

		lcd.Command(device.CMD_CGRAM_Set | (location << 3))
		lcd.Write(char)
	}

}

func HandleDisplay(ctx context.Context, wg *sync.WaitGroup, cfg ScreenConfig, devices map[*midi.Device]*midi.Device, midiEventCounter, score *uint) {
	defer wg.Done()
	if !cfg.Enabled {
		return
	}

	lcd, bus, err := getDisplay(cfg.Address, cfg.Bus, cfg.LcdType)
	if err != nil {
		if bus != nil {
			bus.Close()
		}
		return
	}

	var barChars = [][]byte{
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1F}, // "▁"
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1F, 0x1F}, // "▂"
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x1F, 0x1F, 0x1F}, // "▃"
		{0x00, 0x00, 0x00, 0x00, 0x1F, 0x1F, 0x1F, 0x1F}, // "▄"
		{0x00, 0x00, 0x00, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F}, // "▅"
		{0x00, 0x00, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F}, // "▆"
		{0x00, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F}, // "▇"
		{0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F, 0x1F}, // "█"
	}

	loadCustomCharacters(lcd, barChars)

	lcd.BacklightOn()
	lcd.Clear()

	lastMidiEventsEmitted := *midiEventCounter

	var graph []uint
	var graphPointer int
	for i := 0; i < 20; i++ {
		graph = append(graph, 0)
	}

	var x, y uint = 0, 1
	var counterMaxValue = x - y
	var lastProcessingDuration time.Duration

root:
	for {
		start := time.Now()

		var devCount = len(devices)
		var eventsPerSecond uint

		if lastMidiEventsEmitted > *midiEventCounter {
			eventsPerSecond = (counterMaxValue - lastMidiEventsEmitted) + *midiEventCounter // handling counter overflow
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

		lcd.SetPosition(0, 0)
		fmt.Fprintf(lcd, "devices: %11d", devCount)
		lcd.SetPosition(1, 0)
		fmt.Fprintf(lcd, "handlers: %10d", handlerCount)
		lcd.SetPosition(2, 0)
		fmt.Fprintf(lcd, "events: %12d", eventsPerSecond)
		lcd.SetPosition(3, 0)

		var maxGraph uint
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
		lastProcessingDuration = time.Now().Sub(start)

		select {
		case <-ctx.Done():
			break root
		case <-time.After((time.Duration(cfg.UpdateRate) * time.Second) - lastProcessingDuration):
			break
		}
	}

	log.Info(fmt.Sprintf("closing display"))

	lcd.Clear()
	if !cfg.HaveExitMessage() {
		heart := []byte{0x00, 0x00, 0x0A, 0x1F, 0x1F, 0x0E, 0x04, 0x00}
		randomChar := []byte{0x06, 0x0C, 0x1B, 0x13, 0x10, 0x00, 0x00, 0x00}

		loadCustomCharacters(lcd, [][]byte{heart, randomChar})

		lcd.SetPosition(0, 0)
		fmt.Fprintf(lcd, "                    ")
		lcd.SetPosition(1, 0)
		fmt.Fprintf(lcd, " thanks for playing ")
		lcd.SetPosition(2, 0)
		fmt.Fprintf(lcd, "    %s with HIDI %s  ", string(1), string(0))
		lcd.SetPosition(3, 0)
		msg := fmt.Sprintf("(score: %d)", *score)
		fmt.Fprintf(lcd, fmt.Sprintf("%*s", -20, fmt.Sprintf("%*s", (20+len(msg))/2, msg)))
	} else {
		for i, msg := range cfg.ExitMessage {
			lcd.SetPosition(i, 0)
			fmt.Fprintf(lcd, msg[:20])
		}
	}

	bus.Close()
	log.Info(fmt.Sprintf("display closed"))
}
