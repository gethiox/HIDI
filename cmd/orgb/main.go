package main

import (
	"fmt"
	"os"
	"time"

	"github.com/realbucksavage/openrgb-go"
)

func ouu(err error) {
	if err != nil {
		fmt.Printf("ou error: %s\n", err)
		os.Exit(1)
	}
}

type HSVColor struct {
	H    uint16
	S, V uint8
}

func (h HSVColor) RGB() (r, g, b uint32) {
	// Direct implementation of the graph in this image:
	// https://en.wikipedia.org/wiki/HSL_and_HSV#/media/File:HSV-RGB-comparison.svg
	max := uint32(h.V) * 255
	min := uint32(h.V) * uint32(255-h.S)

	h.H %= 360
	segment := h.H / 60
	offset := uint32(h.H % 60)
	mid := ((max - min) * offset) / 60

	switch segment {
	case 0:
		return max, min + mid, min
	case 1:
		return max - mid, max, min
	case 2:
		return min, max, min + mid
	case 3:
		return min, max - mid, max
	case 4:
		return min + mid, min, max
	case 5:
		return max, min, max - mid
	}

	return 0, 0, 0
}

func main() {
	c, err := openrgb.Connect("localhost", 6742)
	ouu(err)
	defer c.Close()

	deviceCount, err := c.GetControllerCount()
	ouu(err)

	fmt.Printf("devices: %d\n", deviceCount)

	for i := 0; i < deviceCount; i++ {
		d, err := c.GetDeviceController(i)
		if err != nil {
			continue
		}

		fmt.Printf("name: \"%s\"\n", d.Name)
	}

	dev1, err := c.GetDeviceController(0)
	ouu(err)

	fmt.Printf("dev: %s\n", dev1.String())

	colors1 := make([]openrgb.Color, len(dev1.Colors))

	fmt.Printf("colors: %d\n", len(colors1))

	counter := 0

	for {
		for i, _ := range colors1 {
			co := HSVColor{
				H: uint16(counter+i*43) % 360,
				S: 255,
				V: 255,
			}

			r, g, b := co.RGB()

			colors1[i].Red = uint8(r / 255)
			colors1[i].Green = uint8(g / 255)
			colors1[i].Blue = uint8(b / 255)
		}

		err = c.UpdateLEDs(0, colors1)
		ouu(err)

		counter++
		if counter == 360 {
			counter = 0
		}
		time.Sleep(time.Millisecond * 10)
	}

}
