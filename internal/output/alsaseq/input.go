//go:build linux

package alsaseq

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/midi"
)

// Input receives MIDI from other sequencer ports: a sequencer client with
// one port that other clients can send to.
type Input struct {
	// file is the sequencer device, opened so that Close interrupts Read.
	file *os.File
	port addr

	mu     sync.Mutex
	client *client // nil once closed
	buf    []byte
}

// OpenInput creates a MIDI input with the given name. It receives nothing
// until it is connected to a source, by [Input.Connect] or by another
// program.
func OpenInput(name string) (*Input, error) {
	c, err := openClient(name, unix.O_RDWR|unix.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	port, err := c.createPort(name,
		portCapWrite|portCapSubsWrite,
		portTypeMIDIGeneric|portTypeSoftware|portTypeApplication)
	if err != nil {
		c.close()
		return nil, err
	}
	return &Input{
		file:   os.NewFile(uintptr(c.fd), devicePath),
		port:   port,
		client: c,
		buf:    make([]byte, 64*eventSize),
	}, nil
}

// Connect makes the input receive what a source port sends. The ID is one
// that [Sources] returned. It reports whether the two were connected
// already, which is not an error.
func (in *Input) Connect(sourceID string) (already bool, err error) {
	source, err := parseAddr(sourceID)
	if err != nil {
		return false, err
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.client == nil {
		return false, ErrClosed
	}
	err = in.client.subscribe(source, in.port)
	if errors.Is(err, unix.EBUSY) {
		return true, nil
	}
	return false, err
}

// Read waits for MIDI and appends the messages received to out. It may
// return without any, when what arrived was not a MIDI message HIDI knows.
// After [Input.Close] it returns [ErrClosed]. Read must not be called from
// several goroutines at once.
func (in *Input) Read(out []midi.Message) ([]midi.Message, error) {
	n, err := in.file.Read(in.buf)
	if err != nil {
		if errors.Is(err, os.ErrClosed) {
			return out, ErrClosed
		}
		return out, fmt.Errorf("reading from the ALSA sequencer: %w", err)
	}
	for off := 0; off+eventSize <= n; off += eventSize {
		ev := (*event)(unsafe.Pointer(&in.buf[off]))
		if m, ok := decode(*ev); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// Close removes the input and interrupts a Read in progress. Closing twice
// is not an error.
func (in *Input) Close() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.client == nil {
		return nil
	}
	in.client = nil
	return in.file.Close()
}

// decode converts a sequencer event to a message. It reports false for
// events that are not channel messages, such as the sequencer's own
// announcement that a port was connected.
func decode(ev event) (midi.Message, bool) {
	if ev.Source.Client == systemClient {
		return midi.Message{}, false
	}
	channel := ev.Data[0] & 0x0F
	note, velocity := midi.Note(ev.Data[1]&0x7F), ev.Data[2]&0x7F
	param := uint8(native.Uint32(ev.Data[4:]) & 0x7F)
	value := int32(native.Uint32(ev.Data[8:]))
	value7 := midi.From7(uint8(value & 0x7F))

	switch ev.Type {
	case eventNoteOn:
		if velocity == 0 {
			// The MIDI convention for a note off in running status.
			return midi.NoteOff(channel, note, midi.From7(64)), true
		}
		return midi.NoteOn(channel, note, midi.From7(velocity)), true
	case eventNoteOff:
		return midi.NoteOff(channel, note, midi.From7(velocity)), true
	case eventKeyPress:
		return midi.PolyPressure(channel, note, midi.From7(velocity)), true
	case eventController:
		return midi.ControlChange(channel, param, value7), true
	case eventPgmChange:
		return midi.ProgramChange(channel, uint8(value&0x7F)), true
	case eventChanPress:
		return midi.ChannelPressure(channel, value7), true
	case eventPitchBend:
		// The sequencer centers pitch bend on zero.
		return midi.PitchBend(channel, midi.From14(uint16(min(max(value+8192, 0), 16383)))), true
	}
	return midi.Message{}, false
}
