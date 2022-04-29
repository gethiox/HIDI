package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/display"
	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/gethiox/HIDI/internal/pkg/midi"
	"github.com/jroimartin/gocui"
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
		viewData = append(viewData, "Devices:")

		var ptrs DevicePtrs

		for _, d := range devices {
			ptrs = append(ptrs, d)
		}

		sort.Sort(ptrs)

		for _, d := range ptrs {
			dname := d.InputDevice.Name
			dtype := d.InputDevice.DeviceType.String()
			nameSep := 30 - len(dname)
			typeSep := 8 - len(dtype)
			s := fmt.Sprintf(
				"Name: %s, Type: %s, handlers: %2d",
				strings.Repeat(" ", nameSep)+colorForString(au, dname).String(),
				strings.Repeat(" ", typeSep)+colorForString(au, dtype).String(),
				len(d.InputDevice.Handlers),
			)
			viewData = append(viewData, s)
			viewData = append(viewData, "└ "+d.Status())
		}

		view.Clear()
		for _, d := range viewData {
			view.Write([]byte{'\n'})
			view.Write([]byte(d))
		}
		time.Sleep(time.Millisecond * 500)
	}
}

func logView(g *gocui.Gui, color bool, logLevel int) {
	feeder, err := NewFeeder(g, ViewLogs, logLevel, aurora.NewAurora(color))
	if err != nil {
		panic(err)
	}

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
