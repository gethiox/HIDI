package tray

import (
	"fmt"
	"slices"

	"github.com/gethiox/HIDI/internal/ipc"
)

// Look is what the icon tells at a glance.
type Look uint8

const (
	// LookStopped: HIDI is not running.
	LookStopped Look = iota
	// LookOff: HIDI is running and turned off; the devices type.
	LookOff
	// LookOn: HIDI is running and turned on; the devices play.
	LookOn
	// LookLearning: the other half of the blinking of an icon whose
	// HIDI is on and in learning mode: it alternates with LookOn.
	LookLearning
)

// View is everything the icon shows.
type View struct {
	Look Look
	// Title is the text shown when the pointer rests on the icon.
	Title string
	// Lines are shown at the top of the menu and cannot be chosen: the
	// state in words, and the instruments.
	Lines []string
	// Toggle is the label of the menu entry that turns HIDI on or off. It
	// is empty while HIDI is not running.
	Toggle string
	// Learn is the label of the menu entry that turns learning mode on
	// or off; empty while HIDI is not running. Learning says that the
	// mode is on, which the icon shows by blinking while HIDI is on.
	Learn    string
	Learning bool
	// Open is the label of the menu entry that opens HIDI's window. It is
	// empty where the icon has no window to open; where it has one, a
	// click on the icon opens it instead of turning HIDI on or off.
	Open string
	// Quit is the label of the last menu entry.
	Quit string
}

// maxInstruments is how many instruments the menu lists by name.
const maxInstruments = 8

// ViewOf describes the state of HIDI; nil stands for a HIDI that is not
// running.
func ViewOf(s *ipc.Snapshot) View {
	if s == nil {
		return View{Look: LookStopped, Title: "HIDI is not running", Lines: []string{"HIDI is not running"}}
	}
	v := View{Look: LookOff, Title: "HIDI is off", Toggle: "Turn HIDI on", Learn: "Turn learning mode on"}
	if s.Active {
		v.Look, v.Title, v.Toggle = LookOn, "HIDI is on", "Turn HIDI off"
	}
	if s.Learning {
		v.Learning, v.Learn = true, "Turn learning mode off"
		v.Title += ", learning"
	}
	switch n := len(s.Instruments); n {
	case 0:
		v.Lines = []string{v.Title, "No instruments"}
	case 1:
		v.Title += ", 1 instrument"
		v.Lines = []string{v.Title}
	default:
		v.Title += fmt.Sprintf(", %d instruments", n)
		v.Lines = []string{v.Title}
	}
	for i, in := range s.Instruments {
		if i == maxInstruments {
			v.Lines = append(v.Lines, fmt.Sprintf("and %d more", len(s.Instruments)-i))
			break
		}
		v.Lines = append(v.Lines, fmt.Sprintf("%s (channel %d)", in.Name, in.Channel))
	}
	return v
}

// Action is something the user did with the icon.
type Action uint8

const (
	// ActionToggle: turn HIDI on if it is off, and off if it is on.
	ActionToggle Action = iota + 1
	// ActionQuit: the last menu entry. For an icon that runs beside HIDI
	// it removes the icon; for the icon of the application it ends HIDI.
	ActionQuit
	// ActionOpen: open HIDI's window.
	ActionOpen
	// ActionLearn: turn learning mode on if it is off, and off if it is
	// on.
	ActionLearn
)

// The labels of the menu entries beside the one that toggles.
const (
	// QuitLabel removes an icon that runs beside HIDI.
	QuitLabel = "Remove this icon"
	// QuitAllLabel ends HIDI, for the icon that is part of it.
	QuitAllLabel = "Quit HIDI"
	// OpenLabel opens the window.
	OpenLabel = "Open the window"
)

// equal reports whether two views show the same.
func (v View) equal(o View) bool {
	return v.Look == o.Look && v.Title == o.Title && v.Toggle == o.Toggle && v.Learn == o.Learn &&
		v.Open == o.Open && v.Quit == o.Quit && slices.Equal(v.Lines, o.Lines)
}

// Shown reports whether the icon of a host is on screen now, as far as
// the host can tell: a desktop may have no place for icons at all. A host
// that cannot tell is taken at its word.
func Shown(h Host) bool {
	p, ok := h.(interface{ Present() bool })
	return !ok || p.Present()
}

// click is what a click on the icon does.
func (v View) click() Action {
	if v.Open != "" {
		return ActionOpen
	}
	return ActionToggle
}

// quit is the label of the last menu entry.
func (v View) quit() string {
	if v.Quit != "" {
		return v.Quit
	}
	return QuitLabel
}

// Host is the notification area of one desktop system.
type Host interface {
	// Show makes the icon look like the view.
	Show(View) error
	// Actions returns the channel of what the user does.
	Actions() <-chan Action
	// Close removes the icon.
	Close() error
}
