package light

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gethiox/HIDI/internal/control"
	"github.com/gethiox/HIDI/internal/light/openrgb"
)

// Server is an OpenRGB server as the hub uses it; *openrgb.Client is one.
type Server interface {
	Controllers() ([]openrgb.Controller, error)
	SetCustomMode(index int) error
	SetLEDs(index int, colours []openrgb.RGB) error
	// Sync returns once the server has taken everything sent to a
	// controller so far.
	Sync(index int) error
	Close() error
}

// Options configure a [Hub].
type Options struct {
	// Connect reaches the server. It is called again, after a while,
	// whenever there is no connection.
	Connect func() (Server, error)
	// Matches says whether the hardware at an OpenRGB location is a given
	// input device. Nil means [Matches].
	Matches func(location string, info control.DeviceInfo) bool
	// Colours are the colours to use. The zero value means
	// [DefaultColours].
	Colours Colours
	// Logger receives what the hub reports. Nil discards it.
	Logger *slog.Logger
	// Disabled makes the hub start turned off: it reaches for no server
	// until [Hub.SetEnabled] turns it on.
	Disabled bool
	// Changed, if set, is called when what a lamp shows has changed. It
	// must return quickly.
	Changed func()
	// Interval is how often the hub looks after what is not a change of
	// the lights: the server, keyboards that show up, and the repeats of
	// what was sent last. Zero means 25 ms. A changed scene is sent at
	// once.
	Interval time.Duration
	// Repeats are the times after a change at which what was sent is
	// sent once more. Nil means [DefaultRepeats]; empty, never.
	Repeats []time.Duration
	// Retry is how long the hub waits before it looks for the server, or
	// for a keyboard's lights, again. Zero means three seconds.
	Retry time.Duration
}

// Hub lights the keyboards of the instruments through one OpenRGB server.
// The server may start later than HIDI, or go away and come back.
//
// How fast the lights follow is up to the keyboard: writing all its keys
// takes some tens of milliseconds on most. The hub sends a changed scene
// at once and then waits until the server has taken it before it sends
// the next, which is then the newest one: nothing queues up behind a slow
// keyboard, and no change is shown later than one write of the keyboard
// after it happened.
type Hub struct {
	opts Options
	log  *slog.Logger
	// colours are the colours in use; [Hub.SetColours] replaces them.
	colours atomic.Pointer[Colours]
	// wake tells the hub's goroutine that a lamp has something new.
	wake chan struct{}
	// disabled says that the hub is turned off, and renew that the
	// connection is to be made anew, to another server for one.
	disabled, renew atomic.Bool

	mu    sync.Mutex
	lamps []*Lamp
}

// DefaultRepeats are the times after a change at which the same colours
// are sent once more. Released versions of OpenRGB lose an update that
// arrives while the keyboard is still being written with the one before:
// they note that there is something to write, and forget the note when
// the write in progress ends. A key would then stay lit until something
// else changed. The repeats make what was sent last arrive in the end.
var DefaultRepeats = []time.Duration{40 * time.Millisecond, 120 * time.Millisecond, 350 * time.Millisecond, time.Second}

// Lamp is the lights of one instrument's keyboard.
type Lamp struct {
	hub  *Hub
	info control.DeviceInfo

	mu     sync.Mutex
	scene  Scene
	dirty  bool
	closed bool
	// lit says that the keyboard shows what the hub sends, painted is
	// the colour of every key that has one of its own, and rest that of
	// the other keys. A painted map is never changed once it is here.
	lit     bool
	painted map[control.Code]RGB
	rest    RGB

	// The fields below belong to the hub's goroutine.
	bound    bool
	index    int
	name     string
	keys     []ledKey      // for each light, its key
	sent     []openrgb.RGB // what the lights were last set to
	sentAt   time.Time     // when they last changed
	repeated int           // how many of the repeats were sent since
	missed   bool          // no lights were found for it, and that was said
}

// Painted returns what the keyboard shows: the colour of every key that
// has one of its own and the colour of all other keys. lit is false while
// the hub does not control the keyboard's lights; the colours mean nothing
// then. The map must not be changed.
func (l *Lamp) Painted() (keys map[control.Code]RGB, rest RGB, lit bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.painted, l.rest, l.lit && !l.closed
}

type ledKey struct {
	code  control.Code
	isKey bool
}

// NewHub returns a hub. [Hub.Run] makes it work.
func NewHub(opts Options) *Hub {
	if opts.Matches == nil {
		opts.Matches = Matches
	}
	if opts.Colours == (Colours{}) {
		opts.Colours = DefaultColours()
	}
	if opts.Repeats == nil {
		opts.Repeats = DefaultRepeats
	}
	if opts.Changed == nil {
		opts.Changed = func() {}
	}
	if opts.Interval == 0 {
		opts.Interval = 25 * time.Millisecond
	}
	if opts.Retry == 0 {
		opts.Retry = 3 * time.Second
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &Hub{opts: opts, log: log, wake: make(chan struct{}, 1)}
	h.colours.Store(&opts.Colours)
	h.disabled.Store(opts.Disabled)
	return h
}

// SetEnabled turns the hub on or off while it runs. Turned off, it lights
// every keyboard evenly, as while HIDI is off, and lets go of the server;
// turned on, it reaches for the server at once.
func (h *Hub) SetEnabled(on bool) {
	if h.disabled.Swap(!on) != !on {
		h.poke()
	}
}

// Reconnect makes the hub let go of its server and reach for one anew:
// after the address it is told to connect to has changed.
func (h *Hub) Reconnect() {
	h.renew.Store(true)
	h.poke()
}

// SetColours replaces the colours. The keyboards follow at once.
func (h *Hub) SetColours(c Colours) {
	h.colours.Store(&c)
	h.mu.Lock()
	for _, l := range h.lamps {
		l.mu.Lock()
		l.dirty = true
		l.mu.Unlock()
	}
	h.mu.Unlock()
	h.poke()
}

func (h *Hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default: // the hub is about to look anyway
	}
}

// Attach returns the lamp for an instrument's device. It lights up when
// the server has lights for that device. The caller closes it.
func (h *Hub) Attach(info control.DeviceInfo) *Lamp {
	l := &Lamp{hub: h, info: info}
	h.mu.Lock()
	h.lamps = append(h.lamps, l)
	h.mu.Unlock()
	return l
}

// Show makes the lamp show a scene. It returns at once; the lights follow
// as fast as the keyboard takes them.
func (l *Lamp) Show(s Scene) {
	l.mu.Lock()
	if !l.closed {
		l.scene, l.dirty = s, true
	}
	l.mu.Unlock()
	l.hub.poke()
}

// Close ends the lamp. The hub lights its keys evenly, as while HIDI is
// off, the next time it looks.
func (l *Lamp) Close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}

// Run keeps the lights in step with the lamps until ctx is cancelled. On
// the way out it lights every keyboard evenly, as while HIDI is off.
func (h *Hub) Run(ctx context.Context) {
	var (
		server    Server
		nextTry   time.Time
		nextBind  time.Time
		announced bool // that the server cannot be reached
	)
	drop := func(err error) {
		h.log.Warn("the connection to OpenRGB is lost; the lights stay as they are", "error", err)
		server.Close()
		server = nil
		nextTry = time.Now().Add(h.opts.Retry)
		open, _ := h.current()
		for _, l := range open {
			l.bound, l.missed = false, false
			h.unlit(l)
		}
	}
	ticker := time.NewTicker(h.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if server != nil {
				// Every lamp, also those that are closed already: the
				// instruments end at the same moment and close theirs,
				// and which comes first must not decide whether a
				// keyboard is left in the colours of an instrument.
				h.mu.Lock()
				all := slices.Clone(h.lamps)
				h.mu.Unlock()
				for _, l := range all {
					h.leave(server, l)
				}
				server.Close()
			}
			return
		case <-ticker.C:
		case <-h.wake:
		}
		lamps, closed := h.current()
		if server != nil {
			for _, l := range closed {
				h.leave(server, l)
			}
		}
		if off := h.disabled.Load(); off || h.renew.Swap(false) {
			if server != nil {
				// The keyboards are left evenly lit, and their lamps wait
				// for the next server.
				for _, l := range lamps {
					h.rest(server, l)
				}
				server.Close()
				server = nil
				h.log.Info("the connection to OpenRGB is closed")
			}
			nextTry, announced = time.Time{}, false
			if off {
				continue
			}
		}
		if len(lamps) == 0 {
			continue
		}
		now := time.Now()
		if server == nil {
			if now.Before(nextTry) {
				continue
			}
			nextTry = now.Add(h.opts.Retry)
			s, err := h.opts.Connect()
			if err != nil {
				if !announced {
					announced = true
					h.log.Info("no OpenRGB server yet; the lights follow when one runs", "error", err)
				}
				continue
			}
			server, announced = s, false
			h.log.Info("connected to OpenRGB")
		}
		// Lamps without lights look for them: at once after connecting,
		// then every so often, since a keyboard may show up later.
		fresh := slices.ContainsFunc(lamps, func(l *Lamp) bool { return !l.bound && !l.missed })
		waiting := slices.ContainsFunc(lamps, func(l *Lamp) bool { return !l.bound })
		if fresh || waiting && !now.Before(nextBind) {
			nextBind = now.Add(h.opts.Retry)
			if err := h.bind(server, lamps); err != nil {
				drop(err)
				continue
			}
		}
		for _, l := range lamps {
			if l.bound {
				if err := h.send(server, l, time.Now()); err != nil {
					drop(err)
					break
				}
			}
		}
	}
}

// current returns the lamps that are open. The closed ones are forgotten
// and returned too, once, to be left evenly lit.
func (h *Hub) current() (open, closed []*Lamp) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lamps = slices.DeleteFunc(h.lamps, func(l *Lamp) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			closed = append(closed, l)
		}
		return l.closed
	})
	return slices.Clone(h.lamps), closed
}

// leave lights the keys of a lamp evenly, as while HIDI is off, and ends
// the lamp: whatever its instrument still asks to show comes too late.
func (h *Hub) leave(server Server, l *Lamp) {
	l.mu.Lock()
	l.closed = true
	l.scene.Active, l.scene.Pressed, l.dirty = false, nil, true
	l.mu.Unlock()
	if l.bound {
		h.send(server, l, time.Now())
	}
	h.unlit(l)
}

// rest lights the keys of an open lamp evenly and lets go of its
// keyboard; the lamp keeps its scene and lights up again with the next
// server.
func (h *Hub) rest(server Server, l *Lamp) {
	if l.bound {
		l.mu.Lock()
		scene := l.scene
		l.scene.Active, l.scene.Pressed, l.dirty = false, nil, true
		l.mu.Unlock()
		h.send(server, l, time.Now())
		l.mu.Lock()
		l.scene, l.dirty = scene, true
		l.mu.Unlock()
	}
	l.bound, l.missed, l.sent = false, false, nil
	h.unlit(l)
}

// unlit records that a lamp's keyboard no longer shows what the hub
// sends.
func (h *Hub) unlit(l *Lamp) {
	l.mu.Lock()
	was := l.lit
	l.lit, l.painted = false, nil
	l.mu.Unlock()
	if was {
		h.opts.Changed()
	}
}

// bind gives every lamp without lights the keyboard of the server that is
// its device, if there is one.
func (h *Hub) bind(server Server, lamps []*Lamp) error {
	controllers, err := server.Controllers()
	if err != nil {
		return err
	}
	taken := map[int]bool{}
	for _, l := range lamps {
		if l.bound {
			taken[l.index] = true
		}
	}
	for _, l := range lamps {
		if l.bound {
			continue
		}
		for _, c := range controllers {
			if taken[c.Index] || c.Type != openrgb.TypeKeyboard || !h.opts.Matches(c.Location, l.info) {
				continue
			}
			if err := server.SetCustomMode(c.Index); err != nil {
				return err
			}
			l.bound, l.index, l.name, l.sent = true, c.Index, c.Name, nil
			l.keys = make([]ledKey, len(c.LEDs))
			keys := 0
			for i, led := range c.LEDs {
				if code, ok := KeyOfLED(led); ok {
					l.keys[i] = ledKey{code, true}
					keys++
				}
			}
			l.mu.Lock()
			l.dirty = true
			l.mu.Unlock()
			taken[c.Index] = true
			h.log.Info("lighting the keys", "device", l.info.Name, "openrgb", c.Name, "lights", len(c.LEDs), "keys", keys)
			break
		}
		if !l.bound && !l.missed {
			l.missed = true
			h.log.Info("OpenRGB has no lights for this device", "device", l.info.Name)
		}
	}
	return nil
}

// send sets the lights of a lamp if its scene changed and the colours are
// not what the lights have already, and repeats what was last sent when
// that is due. It returns when the server has taken what was sent.
func (h *Hub) send(server Server, l *Lamp, now time.Time) error {
	l.mu.Lock()
	scene, dirty := l.scene, l.dirty
	l.dirty = false
	l.mu.Unlock()
	if !dirty {
		if l.sent == nil || l.repeated >= len(h.opts.Repeats) || now.Sub(l.sentAt) < h.opts.Repeats[l.repeated] {
			return nil
		}
		l.repeated++
		if err := server.SetLEDs(l.index, l.sent); err != nil {
			return err
		}
		return server.Sync(l.index)
	}
	c := *h.colours.Load()
	painted := Paint(scene, c)
	rest := c.Unbound
	if !scene.Active {
		rest = c.Idle
	}
	colours := make([]openrgb.RGB, len(l.keys))
	for i, key := range l.keys {
		switch colour, bound := painted[key.code]; {
		case !key.isKey:
			// A light that is not under a key stays dark.
		case bound:
			colours[i] = openrgb.RGB(colour)
		default:
			colours[i] = openrgb.RGB(rest)
		}
	}
	if slices.Equal(colours, l.sent) {
		return nil
	}
	if err := server.SetLEDs(l.index, colours); err != nil {
		return err
	}
	l.sent, l.sentAt, l.repeated = colours, now, 0
	l.mu.Lock()
	l.lit, l.painted, l.rest = true, painted, rest
	l.mu.Unlock()
	h.opts.Changed()
	// Not before the server has taken this: what comes next is then the
	// newest scene, not one of a row that waits for the keyboard.
	return server.Sync(l.index)
}
