package display

import (
	"fmt"
	"sync"

	device "github.com/d2r2/go-hd44780"
	"github.com/d2r2/go-i2c"
	shittyLogger "github.com/d2r2/go-logger"
	"github.com/gethiox/HIDI/internal/pkg/logger"
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

var conversionMap = map[rune]byte{
	'▁': 0,
	'▂': 1,
	'▃': 2,
	'▄': 3,
	'▅': 4,
	'▆': 5,
	'▇': 6,
	'█': 7,
	'❤': 0,
	'░': 1,
}

func replaceCharsForDisplay(s string) string {
	var ns string
	for _, r := range s {
		n, ok := conversionMap[r]
		if ok {
			ns += string(n)
		} else {
			ns += string(r)
		}
	}
	return ns
}

type DisplayData struct {
	Lines   [4]string
	LastMsg bool // inform LCD about loading exit message to load differrent custom character set
}

func HandleDisplay(wg *sync.WaitGroup, cfg ScreenConfig, dd <-chan DisplayData) {
	defer wg.Done()
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

	for data := range dd {
		if !data.LastMsg {
			for i, s := range data.Lines {
				fixed := replaceCharsForDisplay(s)
				lcd.SetPosition(i, 0)
				lcd.Write([]byte(fixed))
			}
		} else {
			loadCustomCharacters(lcd, [][]byte{
				{0x00, 0x00, 0x0A, 0x1F, 0x1F, 0x0E, 0x04, 0x00},
				{0x06, 0x0C, 0x1B, 0x13, 0x10, 0x00, 0x00, 0x00},
			})
			lcd.Clear()
			for i, s := range data.Lines {
				fixed := replaceCharsForDisplay(s)
				lcd.SetPosition(i, 0)
				lcd.Write([]byte(fixed))
			}
		}

	}

	bus.Close()
	log.Info(fmt.Sprintf("display closed"))
}
