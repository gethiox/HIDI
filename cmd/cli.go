package main

import (
	"fmt"

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
		v.Wrap = true
		v.Frame = true

		// for i := 0; i < 200; i++ {
		// 	fmt.Fprintf(v, fmt.Sprintf("\nHello world! %s %d", aurora.Red("asdf"), i))
		// }
		//
		// for i := uint8(16); i <= 231; i++ {
		// 	fmt.Fprintf(v, fmt.Sprintf("\n%d, %s, %s", i, aurora.Index(i, "pew-pew"), aurora.BgIndex(i, "pew-pew")))
		// }
	}

	if v, err := g.SetView(ViewOverview, maxX/2, 0, maxX-1, 10); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "[overview]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
		fmt.Fprintln(v, "Hello world!")
	}

	x := 40

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

type Feeder struct {
	view *gocui.View
}

func NewFeeder(gui *gocui.Gui, viewName string) (Feeder, error) {
	v, err := gui.View(viewName)
	if err != nil {
		return Feeder{}, err
	}

	return Feeder{view: v}, nil
}

func (f *Feeder) Write(data []byte) {
	f.view.Write([]byte{'\n'})
	f.view.Write(data)
}

func (f *Feeder) OverWrite(data []byte) {
	f.view.Clear()
	f.view.Write([]byte{'\n'})
	f.view.Write(data)
}
