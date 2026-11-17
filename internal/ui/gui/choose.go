package gui

import (
	"maps"
	"slices"
	"strings"

	"github.com/gethiox/HIDI/internal/ipc"
	"github.com/gethiox/HIDI/internal/output"
)

// maxToggleKeys is the most keys a toggle combination is taken to have.
const maxToggleKeys = 4

// capture takes a key combination from what the user presses: the keys
// that were down together on one keyboard, from the first one pressed
// until all are let go again. One keyboard, because the combination is
// later looked for on each device by itself: Ctrl on one keyboard and a
// key on another would be a combination nobody can press.
type capture struct {
	device string   // the keyboard the first key was pressed on
	keys   []string // in the order they went down
}

// step takes the keys that are down now, by keyboard, and reports whether
// the combination is complete: at least one key was pressed and none is
// down any more.
func (c *capture) step(down map[string][]string) bool {
	if c.device == "" {
		// The first keyboard with a key down is the one; by name, so that
		// two that begin at once give the same answer every time.
		for _, device := range slices.Sorted(maps.Keys(down)) {
			if len(down[device]) > 0 {
				c.device = device
				break
			}
		}
	}
	now := down[c.device]
	for _, key := range now {
		if !slices.Contains(c.keys, key) && len(c.keys) < maxToggleKeys {
			c.keys = append(c.keys, key)
		}
	}
	return len(c.keys) > 0 && len(now) == 0
}

// keysDown returns the keys that are down on each keyboard among the
// instruments, by name. The keyboards are told apart by their name and
// where they are plugged in.
func keysDown(state *ipc.Snapshot) map[string][]string {
	down := map[string][]string{}
	if state == nil {
		return down
	}
	for _, in := range state.Instruments {
		if in.Type != "keyboard" {
			continue
		}
		device := in.Name + "\x00" + in.Location
		for _, b := range in.Buttons {
			if b.Down && b.Part == "" && strings.HasPrefix(b.Name, "KEY_") {
				down[device] = append(down[device], b.Name)
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
// with the entries that name it exactly taken out if it was in. An entry
// may be a part of a name that selects several ports; such an entry is
// left alone, since taking it out would drop the others with it, and
// returned as still, for telling the user.
func togglePort(connect []string, port output.Port) (out []string, still string) {
	if !portChosen(connect, port) {
		return append(slices.Clone(connect), port.Name), ""
	}
	for _, pattern := range connect {
		switch {
		case pattern == port.ID || strings.EqualFold(pattern, port.Name):
		default:
			out = append(out, pattern)
			if output.MatchPorts([]string{pattern})(port) {
				still = pattern
			}
		}
	}
	return out, still
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
