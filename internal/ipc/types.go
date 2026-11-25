package ipc

import (
	"sync"
	"time"
)

// Snapshot is the state of a running HIDI at one moment.
type Snapshot struct {
	// Version is the version of the running HIDI.
	Version string `json:"version"`
	// Active says whether HIDI is turned on.
	Active bool `json:"active"`
	// Learning says that learning mode is on: axes stand still until
	// they are moved far.
	Learning bool `json:"learning,omitempty"`
	// Instruments are the running instruments, ordered by name.
	Instruments []Instrument `json:"instruments"`
	// Devices are the devices that are instruments or could be: those
	// connected, and those HIDI has seen before.
	Devices []Device `json:"devices,omitempty"`
}

// Device is a keyboard, a game controller or another device that a
// profile selects.
type Device struct {
	Name string `json:"name"`
	// Type is the kind of device, as in [Instrument].
	Type string `json:"type"`
	// Connected says that the device is there now.
	Connected bool `json:"connected"`
	// Allowed says that the settings let the device be an instrument.
	Allowed bool `json:"allowed"`
}

// Trail is how busy an instrument was, step by step: the events of each
// step alone, as a rate per second, the newest step last.
type Trail struct {
	// Seq counts the steps; it grows by one with each that is added.
	Seq uint64 `json:"seq"`
	// StepMS is the time one value covers, in milliseconds, and AgeMS
	// how long before this snapshot the newest value was added.
	StepMS int     `json:"step_ms"`
	AgeMS  float64 `json:"age_ms"`
	// Input is what came in from the device, and MIDI what went out.
	Input []int `json:"input"`
	MIDI  []int `json:"midi"`
}

// Key is what a key of the device does in the selected layout.
type Key struct {
	// Name is the key's name, such as "KEY_A".
	Name string `json:"name"`
	// Role is "note", "command", "passthrough", "controller", "bend" or
	// "program".
	Role string `json:"role"`
	// Note, for a note, is its number before the instrument's octave and
	// transposition are added.
	Note int `json:"note,omitempty"`
}

// Controller is a controller, or the pitch bend, that an instrument
// sends by the position of a control, and the value it last sent.
type Controller struct {
	// Bend says that it is the pitch bend; otherwise Number is the
	// number of the controller, 0 to 127.
	Bend   bool `json:"bend,omitempty"`
	Number int  `json:"number"`
	// Channel is the MIDI channel it is sent on, 1 to 16.
	Channel int `json:"channel"`
	// Value is what was last sent, if Known: 0 to 127 for a controller,
	// -8192 to 8191 for the pitch bend, 0 being no bend.
	Value int  `json:"value"`
	Known bool `json:"known,omitempty"`
	// Source names the control that drives it, as profiles do, with the
	// part of the device where it is not the main one.
	Source string `json:"source,omitempty"`
}

// KeyLight is the colour of the light under a key.
type KeyLight struct {
	Name string `json:"name"`
	// Colour is written as "#rrggbb".
	Colour string `json:"colour"`
}

// Instrument is one running instrument.
type Instrument struct {
	// Name is the name of the input device, which is also the name of the
	// instrument's MIDI device without its prefix.
	Name string `json:"name"`
	// Type is the kind of device: "keyboard", "gamepad" and so on.
	Type string `json:"type"`
	// Hardware is the device's identifier as bus:vendor:product:version.
	Hardware string `json:"hardware"`
	// Location says where the device is plugged in.
	Location string `json:"location,omitempty"`
	// Profile is the name of the profile in use.
	Profile string `json:"profile"`
	// ProfileFile is the file the profile is read from; empty for a
	// profile that is inside HIDI.
	ProfileFile string `json:"profile_file,omitempty"`
	// ProfileKind is "factory" for a profile HIDI comes with and "user"
	// for one of the user's own.
	ProfileKind string `json:"profile_kind,omitempty"`

	Octave    int `json:"octave"`
	Transpose int `json:"transpose"`
	// Channel is the MIDI channel, 1 to 16.
	Channel  int    `json:"channel"`
	Velocity int    `json:"velocity"`
	Layout   string `json:"layout"`
	// Chord is the remembered chord as its intervals above the note
	// played, such as "+4 +7"; empty when there is none.
	Chord string `json:"chord,omitempty"`
	// Toggle names the key, or the keys pressed together, that turn
	// HIDI off and on from this device; none if it has no way to.
	Toggle []string `json:"toggle,omitempty"`
	// Typing says that the device is a keyboard that keeps typing while
	// it plays, although keyboards are kept from that: nothing on it
	// turns HIDI off, so HIDI does not take it.
	Typing bool `json:"typing,omitempty"`
	// Learning says that the learn command is held: axes get through
	// only when moved far.
	Learning bool `json:"learning,omitempty"`
	// Sounding is the number of notes that are on.
	Sounding int `json:"sounding"`
	// Notes are the numbers of the notes that are on, lowest first; note
	// 60 is middle C.
	Notes []int `json:"notes"`
	// Controllers is what the instrument sends by the position of its
	// controls in the layout that is selected: the controllers and the
	// pitch bend, in the order of the controls that drive them.
	Controllers []Controller `json:"controllers,omitempty"`
	// InputRate is how many events the device reported in the last
	// second, and MIDIRate how many MIDI messages the instrument sent.
	InputRate int `json:"input_rate"`
	MIDIRate  int `json:"midi_rate"`
	// Trail is how busy the instrument was over the last seconds.
	Trail *Trail `json:"trail,omitempty"`
	// Keys is what the bound keys do in the selected layout.
	Keys []Key `json:"keys,omitempty"`
	// Lit says that HIDI controls the lights of the keyboard. Lights are
	// then the colours of the keys that have one of their own, and
	// LightRest is the colour of every other key.
	Lit       bool       `json:"lit,omitempty"`
	Lights    []KeyLight `json:"lights,omitempty"`
	LightRest string     `json:"light_rest,omitempty"`
	// Simulated says that HIDI does not control the lights and that
	// Lights and LightRest are what they would show; a "simulate" request
	// asked for it.
	Simulated bool `json:"simulated,omitempty"`
	// Complete says that Buttons and Axes list every control the device
	// has; the place of a control in its list is then its number.
	Complete bool `json:"complete,omitempty"`
	// Buttons and Axes are the controls of the device, all of them or
	// those it has reported so far, and where each stands: its raw
	// input, before the profile.
	Buttons []Button `json:"buttons"`
	Axes    []Axis   `json:"axes"`
}

// Button is a key or button of an input device, as the device reports it.
type Button struct {
	// Name is the control's name, such as "KEY_A" or "BTN_SOUTH".
	Name string `json:"name"`
	// Part names the part of the device the control belongs to, for a
	// device with several, such as "Touchpad"; empty for the main part.
	Part string `json:"part,omitempty"`
	Down bool   `json:"down"`
}

// Axis is an axis of an input device, as the device reports it.
type Axis struct {
	// Name is the control's name, such as "ABS_X".
	Name string `json:"name"`
	Part string `json:"part,omitempty"`
	// Value is the position, from 0 to 1.
	Value float64 `json:"value"`
	// Hat says that the device calls the axis one of a hat: it rests in
	// the middle or at an end.
	Hat bool `json:"hat,omitempty"`
}

// LogLine is one line of the running HIDI's log.
type LogLine struct {
	// Seq numbers the lines in the order they were logged.
	Seq   int       `json:"seq"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	// Component is the part of HIDI the line comes from.
	Component string `json:"component,omitempty"`
	// Text is the whole line as HIDI writes it, with ANSI colour codes.
	Text string `json:"text"`
}

// request is what a client sends.
type request struct {
	Op string `json:"op"`
	// Logs, for "watch", asks for the log: this many of the lines already
	// logged, then every new one. Zero asks for no log.
	Logs int `json:"logs,omitempty"`
	// EveryMS, for "watch", is the least time between two snapshots, in
	// milliseconds. Zero leaves it to HIDI.
	EveryMS int `json:"every_ms,omitempty"`
	// File and Text, for "preview", are a profile file and the text HIDI
	// is to use in its place until the file changes on disk; an empty
	// Text ends that.
	File string `json:"file,omitempty"`
	Text string `json:"text,omitempty"`
	// Name, Type and Location, for "simulate", name an instrument as a
	// snapshot does, and On says whether the lights of its keyboard are
	// to be shown though HIDI does not control them.
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Location string `json:"location,omitempty"`
	On       bool   `json:"on,omitempty"`
}

// previewHead is how a preview request begins, byte for byte: such a
// request may be longer than any other, and the server tells by this.
const previewHead = `{"op":"preview"`

// settingsHead begins a request that carries settings, which may be long
// as well.
const settingsHead = `{"op":"settings"`

// response is one answer. Exactly one field is set.
type response struct {
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	// Settings, for "current_settings", are the settings HIDI goes by.
	Settings string   `json:"settings,omitempty"`
	Log      *LogLine `json:"log,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Host is what the server needs from the running HIDI.
type Host struct {
	// Snapshot returns the current state.
	Snapshot func() Snapshot
	// SetActive turns HIDI on or off.
	SetActive func(active bool)
	// SetLearning turns learning mode on or off. Nil for a host that
	// has none.
	SetLearning func(on bool)
	// Simulate turns the showing of a keyboard's lights on or off for
	// an instrument, named as a snapshot names it. Nil for a host that
	// cannot.
	Simulate func(name, kind, location string, on bool)
	// TrySettings makes HIDI go by the settings in text, written as the
	// settings file is, without anything being saved: the window hands
	// over what the user changes there. Settings returns the settings
	// HIDI goes by, written the same way. Nil for a host that has none.
	TrySettings func(text string) error
	Settings    func() string
	// Changes tells when a snapshot would be different from the last.
	Changes *Notifier
	// LogLines returns the kept log lines numbered from to to-1, and
	// LogTotal the number of the next line. Both may be nil for a host
	// without a log.
	LogLines func(from, to int) []LogLine
	LogTotal func() int
	// Preview makes HIDI use text in place of a profile file, without
	// the file being written, until the file changes on disk or Preview
	// is called with an empty text. Nil for a host that cannot.
	Preview func(file, text string) error
}

// Notifier tells any number of listeners that something changed, without
// saying what. Signals that arrive faster than a listener looks merge into
// one. The zero value is ready to use.
type Notifier struct {
	mu        sync.Mutex
	listeners map[chan struct{}]struct{}
}

// Notify signals every listener. It never blocks.
func (n *Notifier) Notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.listeners {
		select {
		case ch <- struct{}{}:
		default: // a signal is waiting already
		}
	}
}

// Listen returns a channel that receives after each change, and a function
// that ends the listening.
func (n *Notifier) Listen() (changes <-chan struct{}, stop func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	if n.listeners == nil {
		n.listeners = map[chan struct{}]struct{}{}
	}
	n.listeners[ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		delete(n.listeners, ch)
		n.mu.Unlock()
	}
}
