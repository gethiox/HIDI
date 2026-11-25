package main

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/instrument"
	"github.com/gethiox/HIDI/internal/light"
	"github.com/gethiox/HIDI/internal/output"
	"github.com/gethiox/HIDI/internal/settings"
)

// TestLiveSettings: what the window hands over is in use and stays in
// use; the file is taken over when it says something new about HIDI, and
// left alone when it was only written for what the window shows.
func TestLiveSettings(t *testing.T) {
	file := settings.Default()
	o := runOptions{given: map[string]bool{}, toggle: file.Input.Toggle, grabKeyboards: file.Input.GrabKeyboards, toggleTap: file.Input.ToggleTap}
	manager := instrument.NewManager(instrument.Options{Toggle: o.toggle})
	hub := light.NewHub(light.Options{Disabled: true})
	var address atomic.Pointer[string]
	var listed atomic.Int32
	router := output.NewRouter(func() ([]output.Port, error) { listed.Add(1); return nil, nil }, output.MatchPorts(nil), nil)
	changes := 0
	live := newLiveSettings(o, file, manager, hub, &address, router, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func([]control.Code) {}, func() { changes++ })

	// The window turns the lights on and names another server.
	tried := file
	tried.Lights.OpenRGB, tried.Lights.Address = true, "10.0.0.9:6742"
	tried.Input.LearnThreshold = 35
	live.try(tried)
	if got := address.Load(); got == nil || *got != "10.0.0.9:6742" {
		t.Fatalf("the address in use: %v", got)
	}
	back, err := settings.Parse([]byte(live.text()))
	if err != nil || !back.Lights.OpenRGB || back.Input.LearnThreshold != 35 {
		t.Fatalf("the settings in use: %+v, %v", back.Lights, err)
	}

	// The file is written again as it was, by a save of something that
	// was in use already for one. Nothing changed in it, so what is
	// being tried stays.
	written := file
	live.fileChanged(written)
	back, _ = settings.Parse([]byte(live.text()))
	if !back.Lights.OpenRGB || back.Lights.Address != "10.0.0.9:6742" || back.Input.LearnThreshold != 35 {
		t.Fatalf("a file written without a change undid what was tried: %+v", back.Lights)
	}

	// The file is changed by hand: that is taken over, all of it.
	edited := written
	edited.Input.LearnThreshold = 10
	live.fileChanged(edited)
	back, _ = settings.Parse([]byte(live.text()))
	if back.Input.LearnThreshold != 10 || back.Lights.OpenRGB {
		t.Errorf("after the file was edited: threshold %d, lights %v", back.Input.LearnThreshold, back.Lights.OpenRGB)
	}

	// Saving what is in use changes nothing: the file then says what
	// HIDI goes by already.
	before := changes
	live.try(edited)
	live.fileChanged(edited)
	if changes != before {
		t.Errorf("saving what was in use made %d changes", changes-before)
	}

	// The ports every instrument also sends to follow at once.
	ports := edited
	ports.Output.Connect = []string{"USB MIDI"}
	live.try(ports)
	back, _ = settings.Parse([]byte(live.text()))
	if len(back.Output.Connect) != 1 {
		t.Errorf("the ports in use: %v", back.Output.Connect)
	}

	// Colours are followed, and told to whoever shows them.
	before = changes
	coloured := ports
	coloured.Lights.Colours.White = light.RGB{R: 9, G: 9, B: 9}
	live.try(coloured)
	if changes == before {
		t.Error("changed colours were not announced")
	}
}
