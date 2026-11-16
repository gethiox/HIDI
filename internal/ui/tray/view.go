package tray

import (
	"fmt"

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
}

// maxInstruments is how many instruments the menu lists by name.
const maxInstruments = 8

// ViewOf describes the state of HIDI; nil stands for a HIDI that is not
// running.
func ViewOf(s *ipc.Snapshot) View {
	if s == nil {
		return View{Look: LookStopped, Title: "HIDI is not running", Lines: []string{"HIDI is not running"}}
	}
	v := View{Look: LookOff, Title: "HIDI is off", Toggle: "Turn HIDI on"}
	if s.Active {
		v.Look, v.Title, v.Toggle = LookOn, "HIDI is on", "Turn HIDI off"
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
	// ActionQuit: remove the icon. HIDI keeps running.
	ActionQuit
)

// QuitLabel is the menu entry that removes the icon.
const QuitLabel = "Remove this icon"

// Host is the notification area of one desktop system.
type Host interface {
	// Show makes the icon look like the view.
	Show(View) error
	// Actions returns the channel of what the user does.
	Actions() <-chan Action
	// Close removes the icon.
	Close() error
}
