package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/awesome-gocui/gocui"
	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/logrusorgru/aurora"
)

type DevicePtrs []*midi.Device

func (d DevicePtrs) Len() int {
	return len(d)
}

func (d DevicePtrs) Less(i, j int) bool {
	return d[i].InputDevice.DeviceID() < d[j].InputDevice.DeviceID()
}

func (d DevicePtrs) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

func overviewView(g *gocui.Gui, colors bool, devices map[*midi.Device]*midi.Device) {
	view, err := g.View(ViewOverview)
	if err != nil {
		panic(err)
	}

	au := aurora.NewAurora(colors)

	for {
		var viewData []string

		var ptrs DevicePtrs

		for _, d := range devices {
			ptrs = append(ptrs, d)
		}

		sort.Sort(ptrs)

		for _, d := range ptrs {
			dname := d.InputDevice.Name
			dtype := d.InputDevice.DeviceType.String()
			typeSep := 8 - len(dtype)
			if typeSep < 0 {
				typeSep = 0
			}

			s := fmt.Sprintf(
				"%s: %s, handlers: %2d",
				strings.Repeat(" ", typeSep)+colorForString(au, dtype).String(),
				colorForString(au, dname).String(),
				len(d.InputDevice.Handlers),
			)
			viewData = append(viewData, s)
			viewData = append(viewData, "└ "+d.Status())
		}

		view.Rewind()
		for _, d := range viewData {
			view.Write([]byte(d))
			view.Write([]byte{'\n'})
		}
		time.Sleep(time.Millisecond * 500)
	}
}

func logView(g *gocui.Gui, color bool, logLevel int) {
	feeder, err := NewFeeder(g, ViewLogs, logLevel, aurora.NewAurora(color))
	if err != nil {
		panic(err)
	}

	// filling up all first lines
	_, y := feeder.view.Size()
	for i := 0; i < y; i++ {
		feeder.view.Write([]byte("\n"))
	}

	for msg := range logger.Messages {
		feeder.Write(msg)
	}
}

func lcdView(g *gocui.Gui, dd <-chan display.DisplayData) {
	view, err := g.View(ViewLCD)
	if err != nil {
		panic(err)
	}

	for data := range dd {
		view.Clear()
		for _, s := range data.Lines {
			view.Write([]byte(s))
		}
	}
}
