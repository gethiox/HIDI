package output

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Router keeps endpoints connected to the existing ports the user chose,
// such as the output of a hardware MIDI interface. A port that appears
// later, or disappears and comes back, is connected when it is there, so a
// MIDI interface can be plugged in at any time.
//
// The router looks at the list of ports at intervals instead of waiting
// for announcements. Connecting what is connected already is harmless, so
// it needs to remember nothing to be correct.
type Router struct {
	list  func() ([]Port, error)
	match func(Port) bool
	log   *slog.Logger

	mu        sync.Mutex // guards what follows, and match
	none      bool       // no port is chosen; see Choose
	endpoints map[*routed]struct{}
	listErr   string // the last failure to list ports that was logged
}

// NewRouter returns a router that connects every endpoint it is given to
// every port that list returns and match accepts.
func NewRouter(list func() ([]Port, error), match func(Port) bool, log *slog.Logger) *Router {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Router{list: list, match: match, log: log, endpoints: map[*routed]struct{}{}}
}

// SetMatch changes which ports the endpoints are connected to, and brings
// the connections in line at once: new ones are made, and those to ports
// that are no longer chosen are ended where the endpoint can end them.
func (r *Router) SetMatch(match func(Port) bool) {
	r.mu.Lock()
	r.match = match
	r.mu.Unlock()
	r.Sync()
}

// Choose is SetMatch for the ports a user named; see [MatchPorts]. With
// none named, an endpoint that cannot be connected to ports is no cause
// for complaint.
func (r *Router) Choose(patterns []string) {
	r.mu.Lock()
	r.none = len(patterns) == 0
	r.mu.Unlock()
	r.SetMatch(MatchPorts(patterns))
}

// MatchPorts returns a match function for the ports a user named. A
// pattern selects a port if it is the port's ID, or if the port's name
// contains it, ignoring case.
func MatchPorts(patterns []string) func(Port) bool {
	return func(p Port) bool {
		name := strings.ToLower(p.Name)
		for _, pattern := range patterns {
			if pattern == p.ID || strings.Contains(name, strings.ToLower(pattern)) {
				return true
			}
		}
		return false
	}
}

// routed is an endpoint the router looks after.
type routed struct {
	Connector
	router *Router
	name   string
	// connected and failed hold what was logged, by port ID, so that it
	// is said once and not at every look.
	connected map[string]string // port ID to port name
	failed    map[string]bool
}

// Close stops the router from looking after the endpoint, and closes it.
func (e *routed) Close() error {
	e.router.mu.Lock()
	delete(e.router.endpoints, e)
	e.router.mu.Unlock()
	return e.Connector.Close()
}

// Add puts an endpoint under the router's care, connects it to the ports
// that are there now, and returns the endpoint to use in its place.
// Closing that one ends the care. An endpoint that cannot be connected to
// ports is returned as it is.
func (r *Router) Add(name string, e Endpoint) Endpoint {
	c, ok := e.(Connector)
	if !ok {
		r.mu.Lock()
		none := r.none
		r.mu.Unlock()
		if !none {
			r.log.Warn("this kind of MIDI device cannot be connected to existing ports", "instrument", name)
		}
		return e
	}
	added := &routed{Connector: c, router: r, name: name, connected: map[string]string{}, failed: map[string]bool{}}
	r.mu.Lock()
	r.endpoints[added] = struct{}{}
	r.mu.Unlock()
	r.Sync()
	if receiver, ok := e.(Receiver); ok {
		// Keep what the endpoint can do besides sending.
		return routedReceiver{added, receiver}
	}
	return added
}

// routedReceiver is a routed endpoint that also receives.
type routedReceiver struct {
	*routed
	Receiver
}

// Run calls [Router.Sync] at every interval until ctx is cancelled.
func (r *Router) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sync()
		}
	}
}

// Sync connects every endpoint to every chosen port that exists now.
func (r *Router) Sync() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.endpoints) == 0 {
		return
	}
	ports, err := r.list()
	if err != nil {
		if r.listErr != err.Error() {
			r.listErr = err.Error()
			r.log.Warn("MIDI ports cannot be listed", "error", err)
		}
		return
	}
	r.listErr = ""

	chosen, there := map[string]string{}, map[string]bool{}
	for _, p := range ports {
		there[p.ID] = true
		if r.match(p) {
			chosen[p.ID] = p.Name
		}
	}
	for e := range r.endpoints {
		for id, name := range e.connected {
			if _, still := chosen[id]; still {
				continue
			}
			delete(e.connected, id)
			if !there[id] {
				r.log.Info("MIDI port is gone", "instrument", e.name, "port", name)
				continue
			}
			// The port is there and no longer chosen.
			if d, ok := e.Connector.(Disconnector); ok {
				if err := d.Disconnect(id); err != nil {
					r.log.Warn("disconnecting from the MIDI port failed", "instrument", e.name, "port", name, "error", err)
					continue
				}
			}
			r.log.Info("disconnected from MIDI port", "instrument", e.name, "port", name)
		}
		for id := range e.failed {
			if _, there := chosen[id]; !there {
				delete(e.failed, id)
			}
		}
		for id, name := range chosen {
			_, err := e.Connect(id)
			switch {
			case err != nil:
				if !e.failed[id] {
					e.failed[id] = true
					r.log.Warn("connecting to the MIDI port failed", "instrument", e.name, "port", name, "error", err)
				}
			case e.connected[id] != name:
				// Also after a connection someone removed by hand:
				// HIDI puts it back and says so once.
				delete(e.failed, id)
				e.connected[id] = name
				r.log.Info("connected to MIDI port", "instrument", e.name, "port", name, "id", id)
			}
		}
	}
}
