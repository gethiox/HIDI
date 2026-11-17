package gui

import (
	"slices"
	"strings"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/output"
)

// maxToggleKeys is the most keys a toggle combination is taken to have.
const maxToggleKeys = 4

// capture takes a key combination from what the user presses: the keys
// that were down together, from the first one pressed until all are let
// go again.
type capture struct {
	keys []string // in the order they went down
}

// step takes the keys that are down now and reports whether the
// combination is complete: at least one key was pressed and none is down
// any more.
func (c *capture) step(down []string) bool {
	for _, key := range down {
		if !slices.Contains(c.keys, key) && len(c.keys) < maxToggleKeys {
			c.keys = append(c.keys, key)
		}
	}
	return len(c.keys) > 0 && len(down) == 0
}

// keysDown returns the keys that are down on the keyboards among the
// instruments, by name.
func keysDown(state *ipc.Snapshot) []string {
	if state == nil {
		return nil
	}
	var down []string
	for _, in := range state.Instruments {
		if in.Type != "keyboard" {
			continue
		}
		for _, b := range in.Buttons {
			if b.Down && b.Part == "" && strings.HasPrefix(b.Name, "KEY_") && !slices.Contains(down, b.Name) {
				down = append(down, b.Name)
			}
		}
	}
	return down
}

// portChosen reports whether the settings name a port.
func portChosen(connect []string, port output.Port) bool {
	return output.MatchPorts(connect)(port)
}

// togglePort returns the list of ports to send to with one port added, or
// with everything that names it taken out if it was in.
func togglePort(connect []string, port output.Port) []string {
	if !portChosen(connect, port) {
		return append(slices.Clone(connect), port.Name)
	}
	var out []string
	for _, pattern := range connect {
		if !output.MatchPorts([]string{pattern})(port) {
			out = append(out, pattern)
		}
	}
	return out
}

// absentPorts returns the entries of the settings that name no port that
// exists now.
func absentPorts(connect []string, ports []output.Port) []string {
	var out []string
	for _, pattern := range connect {
		match := output.MatchPorts([]string{pattern})
		if !slices.ContainsFunc(ports, match) {
			out = append(out, pattern)
		}
	}
	return out
}
