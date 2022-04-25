package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gethiox/HIDI/internal/pkg/logger"
	"github.com/jroimartin/gocui"
	"github.com/logrusorgru/aurora"
)

func a() {
	aurora.Red("asdf")
}

const (
	ViewLogs     = "logs"
	ViewOverview = "overview"
	ViewLCD      = "lcd"
)

func GetCli() (*gocui.Gui, error) {
	g, err := gocui.NewGui(gocui.Output256)
	if err != nil {
		return nil, err
	}

	g.SetManagerFunc(Layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlQ, gocui.ModNone, quit); err != nil {
		return nil, err
	}

	return g, nil
}

func Layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	if v, err := g.SetView(ViewLogs, 0, 0, maxX-1, maxY-1); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[Logs]"
		v.Autoscroll = true
		v.Wrap = false
		v.Frame = true

		// for i := 0; i < 200; i++ {
		// 	fmt.Fprintf(v, fmt.Sprintf("\nHello world! %s %d", aurora.Red("asdf"), i))
		// }
		//
		// for i := uint8(16); i <= 231; i++ {
		// 	fmt.Fprintf(v, fmt.Sprintf("\n%d, %s, %s", i, aurora.Index(i, "pew-pew"), aurora.BgIndex(i, "pew-pew")))
		// }
	}

	if v, err := g.SetView(ViewOverview, maxX-68, 0, maxX-1, 10); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[overview]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
	}

	x := maxX - 68 - 22

	if v, err := g.SetView(ViewLCD, x, 0, x+21, 5); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[lcd 20x4]"
		v.Autoscroll = false
		v.Wrap = true
		v.Frame = true
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

type TimeNanosecond time.Time

// Implement Marshaler and Unmarshaler interface
func (j *TimeNanosecond) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return err
	}
	*j = TimeNanosecond(time.Unix(0, v))
	return nil
}

func (j TimeNanosecond) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(j))
}

type Entry struct {
	Ts     TimeNanosecond `json:"ts"`
	Caller string         `json:"caller"`
	Msg    string         `json:"msg"`
	Level  int            `json:"level"`

	Device       string `json:"device_name"`
	HandlerEvent string `json:"handler_event"`
	HandlerName  string `json:"handler_name"`
	Config       string `json:"config"`
}

func unpack(data []byte) (Entry, error) {
	var v Entry
	err := json.Unmarshal(data, &v)
	return v, err
}

type Feeder struct {
	view     *gocui.View
	logLevel int
}

func NewFeeder(gui *gocui.Gui, viewName string, logLevel int) (Feeder, error) {
	v, err := gui.View(viewName)
	if err != nil {
		return Feeder{}, err
	}

	return Feeder{view: v, logLevel: logLevel}, nil
}

func (f *Feeder) Write(data []byte) {
	x, _ := f.view.Size()
	msg, err := unpack(data)
	if err != nil {
		f.view.Write([]byte{'\n'})
		f.view.Write(data)
		return
	}

	if msg.Level > f.logLevel {
		return
	}

	tf := time.Time(msg.Ts).Format("15:04:05.000")
	ml := fmt.Sprintf("[%s] %s", tf, msg.Msg)
	mr := ""
	if msg.Config != "" {
		mr += fmt.Sprintf(" [%s]", msg.Config)
	}
	if msg.HandlerName != "" {
		mr += fmt.Sprintf(" [handler=%s]", msg.HandlerName)
	}
	if msg.HandlerEvent != "" {
		mr += fmt.Sprintf(" [%s]", msg.HandlerEvent)
	}
	if msg.Device != "" {
		mr += fmt.Sprintf(" [dev=%s]", msg.Device)
	}
	if f.logLevel >= logger.DebugLvl {
		mr += fmt.Sprintf(" (%s)", msg.Caller)
	}

	mrPad := fmt.Sprintf("%%%ds", x-len(ml)-1)
	mrPadded := fmt.Sprintf(mrPad, mr)
	m := fmt.Sprintf("%s %s", ml, mrPadded)
	f.view.Write([]byte{'\n'})
	f.view.Write([]byte(m))
}

func (f *Feeder) OverWrite(data []byte) {
	f.view.Clear()
	f.view.Write([]byte{'\n'})
	f.view.Write(data)
}
