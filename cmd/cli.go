package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strconv"
	"strings"
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
	}

	if v, err := g.SetView(ViewOverview, maxX-69, 0, maxX-1, 10); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[overview]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
	}

	x := maxX - 69 - 22

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
	DeviceType   string `json:"device_type"`
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

func color(r, g, b uint8) aurora.Color {
	return aurora.Color(16 + 36*r + 6*g + b)
}

// r, g, b 0<=v<=6
func getColor(r, g, b uint8, v interface{}) aurora.Value {
	n := 16 + 36*r + 6*g + b
	return aurora.Index(n, v)
}

func randomColor(v interface{}) aurora.Value {
	return getColor(uint8(rand.Intn(7)), uint8(rand.Intn(7)), uint8(rand.Intn(7)), v)
}

func terminator(r rune) bool {
	if r >= 0x40 && r <= 0x7e {
		return true
	}
	return false
}

// returns random color for string, will return the same color for the same string
func colorForString(s string) aurora.Value {
	h := fnv.New32a()
	h.Write([]byte(s))
	sum := h.Sum32()

	r, g, b := uint8(sum), uint8(sum>>8), uint8(sum>>16)

	if r+g+b < 64 {
		b = 64
	}

	return aurora.Index(16+36*r+6*g+b, s)
	// return color(uint8(sum), uint8(sum>>8), uint8(sum>>16))
}

// rawStringLen returns a len of string ignoring included escape sequences
func rawStringLen(s string) int {
	var sequence bool
	var escLens []int
	var escLen int

	for i, r := range s {
		if !sequence {
			if r == '\033' {
				if i >= len(s)-1 { // esc seems to be last character
					continue
				}
				if s[i+1] == '[' {
					sequence = true
					escLen += 1
					continue
				}

			}
		} else {
			if r == '[' && s[i-1] == '\033' {
				escLen += 1
				continue
			}
			if terminator(r) {
				sequence = false
				escLen += 1
				escLens = append(escLens, escLen)
				escLen = 0
			} else {
				escLen += 1
			}
		}
	}
	var sum int
	for _, x := range escLens {
		sum += x
	}
	return len(s) - sum
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

	timestamp := fmt.Sprintf("[%s]", aurora.Blue(tf).String())

	fields := ""
	if msg.Config != "" {
		fields += fmt.Sprintf(" [config=%s]", colorForString(msg.Config).String())
	}
	if msg.HandlerName != "" {
		fields += fmt.Sprintf(" [handler=%s]", colorForString(msg.HandlerName).String())
	}
	if msg.HandlerEvent != "" {
		fields += fmt.Sprintf(" [%s]", colorForString(msg.HandlerEvent).String())
	}
	if msg.DeviceType != "" {
		fields += fmt.Sprintf(" [type=%s]", colorForString(msg.DeviceType).String())
	}
	if msg.Device != "" {
		fields += fmt.Sprintf(" [dev=%s]", colorForString(msg.Device).String())
	}
	if f.logLevel >= logger.DebugLvl {
		fields += fmt.Sprintf(" (%s)", colorForString(msg.Caller).String())
	}

	fieldsLen := rawStringLen(fields)
	timeLen := rawStringLen(timestamp)
	msgLen := len(msg.Msg)

	var m string
	freeSpace := x - (timeLen + 1 + msgLen + 1 + fieldsLen)
	if freeSpace < 0 {
		limit := (x - (fieldsLen + 1 + timeLen + 1)) - 3
		if limit < 20 {
			m = msg.Msg
			fields = aurora.Gray(12, "(fields hidden)").String()
			freeSpace = x - (timeLen + 1 + msgLen + 1 + rawStringLen(fields))
			if freeSpace < 0 {
				freeSpace = 0
			}
		} else {
			m = msg.Msg[:limit] + "(…)"
			freeSpace = 0
		}
	} else {
		m = msg.Msg
	}

	separators := strings.Repeat(" ", freeSpace)

	mm := fmt.Sprintf("%s %s%s %s", timestamp, m, separators, fields)
	f.view.Write([]byte{'\n'})
	f.view.Write([]byte(mm))
}

func (f *Feeder) OverWrite(data []byte) {
	f.view.Clear()
	f.view.Write([]byte{'\n'})
	f.view.Write(data)
}
