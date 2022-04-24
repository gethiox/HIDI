package cli

import (
	"fmt"

	"github.com/jroimartin/gocui"
)

const (
	ViewLogs     = "logs"
	ViewOverview = "overview"
	ViewLCD      = "lcd"
)

func GetCli() (*gocui.Gui, error) {
	g, err := gocui.NewGui(gocui.Output256)
	if err != nil {
		return nil, err
		// log.Panicln(err)
	}

	g.SetManagerFunc(Layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlQ, gocui.ModNone, quit); err != nil {
		// return err
		// log.Panicln(err)
	}

	// if err := g.SetKeybinding("", gocui.KeyPgup, gocui.ModNone, pgup); err != nil {
	// 	// return err
	// 	// log.Panicln(err)
	// }
	// if err := g.SetKeybinding("", gocui.KeyPgdn, gocui.ModNone, pgdn); err != nil {
	// 	// return err
	// 	// log.Panicln(err)
	// }

	return g, nil
}

func Layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	if v, err := g.SetView(ViewLogs, 0, 0, maxX-1, maxY-1); err != nil {
		if err != gocui.ErrUnknownView {
			return fmt.Errorf("some error 1: %v", err)
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
			return fmt.Errorf("some error 2: %v", err)
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
			return fmt.Errorf("some error 3: %v", err)
		}
		v.Title = "[lcd]"
		v.Autoscroll = false
		v.Wrap = false
		v.Frame = true
		fmt.Fprintln(v, "Hello world1!      >")
		fmt.Fprintln(v, "Hello world2!      >")
		fmt.Fprintln(v, "Hello world3!      >")
		fmt.Fprintln(v, "Hello world4!      >")
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
