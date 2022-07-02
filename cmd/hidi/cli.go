package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/awesome-gocui/gocui"
	"github.com/gethiox/HIDI/internal/pkg/logger"
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
	g, err := gocui.NewGui(gocui.Output256, true)
	if err != nil {
		return nil, err
	}

	g.SetManagerFunc(Layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		return nil, err
	}

	return g, nil
}

func Layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	if v, err := g.SetView(ViewOverview, 0, 0, maxX-1, 9, 0); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[Devices]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
	}

	if v, err := g.SetView(ViewLogs, 0, 9, maxX-1, maxY-1, gocui.TOP); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[Logs]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
	}

	if v, err := g.SetView(ViewLCD, maxX-22, 0, maxX-1, 5, 0); err != nil {
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
	au       aurora.Aurora
	logLevel int
}

func NewFeeder(gui *gocui.Gui, viewName string, logLevel int, au aurora.Aurora) (Feeder, error) {
	v, err := gui.View(viewName)
	if err != nil {
		return Feeder{}, err
	}

	return Feeder{view: v, logLevel: logLevel, au: au}, nil
}

func gray(v uint8) aurora.Color {
	if v > 23 {
		v = 23
	}
	return aurora.Color(232+v) << 16
}

func color(r, g, b uint8) aurora.Color {
	return aurora.Color(16+36*r+6*g+b) << 16
}

// r, g, b 0<=v<=6
func getColor(au aurora.Aurora, r, g, b uint8, v interface{}) aurora.Value {
	n := 16 + 36*r + 6*g + b
	return au.Index(n, v)
}

func randomColor(au aurora.Aurora, v interface{}) aurora.Value {
	return getColor(au, uint8(rand.Intn(7)), uint8(rand.Intn(7)), uint8(rand.Intn(7)), v)
}

func terminator(r rune) bool {
	if r >= 0x40 && r <= 0x7e {
		return true
	}
	return false
}

// returns random color for string, will return the same color for the same string
func colorForString(au aurora.Aurora, s string) aurora.Value {
	h := fnv.New32a()
	h.Write([]byte(s))
	sum := h.Sum32()

	r, g, b := uint8(sum)&0b00000111, uint8(sum>>8)&0b00000111, uint8(sum>>16)&0b00000111
	if r > 5 {
		r = 5
	}
	if g > 5 {
		g = 5
	}
	if b > 5 {
		b = 5
	}

	// avoid dark colors
	if r+g+b < 3 {
		r += 1
		g += 1
		b += 1
	}

	return au.Index(16+36*r+6*g+b, s)
	// return color(uint8(sum), uint8(sum>>8), uint8(sum>>16))
}

func secondProgress(t time.Time) float64 {
	return float64(t.Nanosecond()) / 999999999.0
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

func prepareString(msg Entry, au aurora.Aurora, width, logLevel int) string {
	if msg.Level > logLevel {
		return ""
	}

	var msgColor aurora.Color

	switch msg.Level {
	case logger.ErrorLvl:
		msgColor = color(5, 1, 1)
	case logger.WarningLvl:
		msgColor = color(5, 5, 1)
	case logger.InfoLvl:
		msgColor = gray(18)
	case logger.ActionLvl:
		msgColor = gray(18)
	case logger.KeysLvl:
		msgColor = gray(15)
	case logger.KeysNotAssignedLvl:
		msgColor = gray(13)
	case logger.AnalogLvl:
		msgColor = gray(11)
	case logger.DebugLvl:
		msgColor = gray(9)
	}

	t := time.Time(msg.Ts)

	tf := t.Format("15:04:05.000")

	var base uint8 = 16
	base += uint8(secondProgress(t) * 8)

	timestamp := fmt.Sprintf(
		"[%s]",
		au.Reset(tf).Colorize(color(1, 1, 5)).String(),
	)

	// TODO: some less retarded solution
	fields := ""
	if msg.Config != "" {
		fields += fmt.Sprintf(" [config=%s]", colorForString(au, msg.Config).String())
	}
	if msg.HandlerName != "" {
		fields += fmt.Sprintf(" [handler=%s]", colorForString(au, msg.HandlerName).String())
	}
	if msg.HandlerEvent != "" {
		fields += fmt.Sprintf(" [%s]", colorForString(au, msg.HandlerEvent).String())
	}
	if msg.DeviceType != "" {
		fields += fmt.Sprintf(" [type=%s]", colorForString(au, msg.DeviceType).String())
	}
	if msg.Device != "" {
		fields += fmt.Sprintf(" [dev=%s]", colorForString(au, msg.Device).String())
	}
	if logLevel >= logger.DebugLvl {
		x := strings.Split(msg.Caller, ":")
		fields += fmt.Sprintf(" (%s:%s)", colorForString(au, x[0]).String(), x[1])
	}

	if fields != "" {
		fields = fields[1:] // removing one space at the beginning
	}

	fieldsLen := rawStringLen(fields)
	timeLen := rawStringLen(timestamp)
	msgLen := len(msg.Msg)

	if width > -1 {
		var m string
		freeSpace := width - (timeLen + 1 + msgLen + 1 + fieldsLen)
		if freeSpace < 0 {
			limit := (width - (fieldsLen + 1 + timeLen + 1)) - 3
			if limit < 20 {
				m = au.Reset(msg.Msg).Colorize(msgColor).String()
				fields = au.Gray(12, "(fields hidden)").String()
				freeSpace = width - (timeLen + 1 + msgLen + 1 + rawStringLen(fields))
				if freeSpace < 0 {
					freeSpace = 0
				}
			} else {
				m = au.Reset(msg.Msg[:limit] + "(…)").Colorize(msgColor).String()
				freeSpace = 0
			}
		} else {
			m = au.Reset(msg.Msg).Colorize(msgColor).String()
		}

		separators := strings.Repeat(" ", freeSpace)

		return fmt.Sprintf("%s %s%s %s", timestamp, m, separators, fields)
	} else {
		m := au.Reset(msg.Msg).Colorize(msgColor).String()
		return fmt.Sprintf("%s %s %s", timestamp, m, fields)
	}

}

func (f *Feeder) Write(data []byte) {
	msg, err := unpack(data)
	if err != nil {
		f.view.Write(data)
		f.view.Write([]byte{'\n'})
		return
	}

	x, _ := f.view.Size()

	s := prepareString(msg, f.au, x, f.logLevel)
	if s != "" {
		f.view.Write([]byte(s))
		f.view.Write([]byte{'\n'})
	}
}

func (f *Feeder) OverWrite(data []byte) {
	f.view.Clear()
	f.view.Write(data)
	f.view.Write([]byte{'\n'})
}
