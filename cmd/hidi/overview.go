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

		x, y := view.Size()

		for _, d := range ptrs {
			dname := d.InputDevice.Name
			dtype := d.InputDevice.DeviceType.String()
			typeSep := 8 - len(dtype)
			if typeSep < 0 {
				typeSep = 0
			}

			header := fmt.Sprintf(
				"%s: %s, handlers: %2d",
				strings.Repeat(" ", typeSep)+colorForString(au, dtype).String(),
				colorForString(au, dname).String(),
				len(d.InputDevice.Handlers),
			)

			headerFreeSpace := x - rawStringLen(header)
			if headerFreeSpace < 0 {
				headerFreeSpace = 0
			}

			state := d.State()

			description := fmt.Sprintf(
				"└ octave: %d, semitone: %d, channel: %d, active keys: %d, mapping: %s",
				state.Octave,
				state.Semitone,
				state.Channel,
				state.Notes,
				colorForString(au, state.Mapping).String(),
			)
			descriptionFreeSpace := x - rawStringLen(description)
			if descriptionFreeSpace < 0 {
				descriptionFreeSpace = 0
			}

			viewData = append(viewData, fmt.Sprintf("%s%s", header, strings.Repeat(" ", headerFreeSpace)))
			viewData = append(viewData, fmt.Sprintf("%s%s", description, strings.Repeat(" ", descriptionFreeSpace)))
		}

		view.Rewind()
		for i := 0; i < y; i++ {
			if i > len(viewData)-1 {
				data := strings.Repeat(" ", x)
				view.Write([]byte(data))
				view.Write([]byte{'\n'})
				continue
			}

			view.Write([]byte(viewData[i]))
			view.Write([]byte{'\n'})
		}
		time.Sleep(time.Millisecond * 500)
	}
}

func logView(g *gocui.Gui, color bool, logLevel, bufSize int) {
	feeder, err := NewFeeder(g, ViewLogs, logLevel, aurora.NewAurora(color))
	if err != nil {
		panic(err)
	}

	buf := newLogBuffer(bufSize)

	var closed bool
	var newMessage = make(chan bool, 1)
	var sizeChange = make(chan bool, 1)

	go func() {
		var lastX, lastY int
		for {
			if closed {
				close(sizeChange)
				return
			}
			x, y := feeder.view.Size()
			if x != lastX || y != lastY {
				newMessage <- true // fix this retardation
				lastX = x
				lastY = y
			}
			time.Sleep(time.Millisecond * 100)
		}

	}()

	go func() {
		for msg := range logger.Messages {
			buf.WriteMessage(msg)
			select {
			case newMessage <- true:
			case <-time.After(time.Millisecond * 1):
				continue
			}

		}
		close(newMessage)
		closed = true
	}()

	for {
		select {
		case <-sizeChange:
		case <-newMessage:
		}
		if closed {
			break
		}
		feeder.view.Rewind()
		_, y := feeder.view.Size()
		lastMessages := buf.ReadLastMessages(y)
		for _, msg := range lastMessages {
			feeder.Write(msg)
		}
	}
}

func lcdView(g *gocui.Gui, dd <-chan display.DisplayData) {
	view, err := g.View(ViewLCD)
	if err != nil {
		panic(err)
	}

	for data := range dd {
		view.Rewind()
		for _, s := range data.Lines {
			view.Write([]byte(s))
			view.Write([]byte{'\n'})
		}
	}
}
