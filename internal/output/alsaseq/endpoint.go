//go:build linux

package alsaseq

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/midi"
)

// Endpoint is a virtual MIDI device: a sequencer client with one port that
// other programs can read from. It implements [output.Endpoint].
type Endpoint struct {
	mu     sync.Mutex
	client *client // nil once closed
	port   addr
	buf    []event

	// For an endpoint that also receives: wake is a pipe that Close
	// writes to, to end a Receive that waits; receiving holds the lock on
	// what Receive uses.
	duplex    bool
	wake      [2]int
	receiving sync.Mutex
	inbuf     []byte
}

// Open creates a virtual MIDI device with the given name. The device is
// visible to other programs until the endpoint is closed or the process
// exits.
//
// The endpoint only sends. It does not offer a port to write to, because a
// client that accepts events must also read them; [OpenDuplex] makes one
// that does.
func Open(name string) (*Endpoint, error) {
	c, err := openClient(name, unix.O_WRONLY)
	if err != nil {
		return nil, err
	}
	port, err := c.createPort(name,
		portCapRead|portCapSubsRead,
		portTypeMIDIGeneric|portTypeSoftware|portTypeApplication)
	if err != nil {
		c.close()
		return nil, err
	}
	return &Endpoint{client: c, port: port}, nil
}

// OpenDuplex creates a virtual MIDI device that other programs can read
// from and also send to. What they send is read with [Endpoint.Receive],
// and has to be: the sequencer keeps what is not read, up to a limit.
func OpenDuplex(name string) (*Endpoint, error) {
	c, err := openClient(name, unix.O_RDWR)
	if err != nil {
		return nil, err
	}
	port, err := c.createPort(name,
		portCapRead|portCapSubsRead|portCapWrite|portCapSubsWrite,
		portTypeMIDIGeneric|portTypeSoftware|portTypeApplication)
	if err != nil {
		c.close()
		return nil, err
	}
	e := &Endpoint{client: c, port: port, duplex: true, inbuf: make([]byte, 64*eventSize)}
	if err := unix.Pipe2(e.wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		c.close()
		return nil, fmt.Errorf("making the pipe that ends a waiting read: %w", err)
	}
	return e, nil
}

// ErrSendOnly is returned by [Endpoint.Receive] for an endpoint that was
// not opened with [OpenDuplex].
var ErrSendOnly = errors.New("MIDI endpoint does not receive")

// Receive waits for MIDI that another program sends to the device and
// appends the messages to out. It may return without any, when what
// arrived was not a MIDI message HIDI knows. After [Endpoint.Close] it
// returns [ErrClosed]. Receive must not be called from several goroutines
// at once.
func (e *Endpoint) Receive(out []midi.Message) ([]midi.Message, error) {
	if !e.duplex {
		return out, ErrSendOnly
	}
	e.receiving.Lock()
	defer e.receiving.Unlock()
	e.mu.Lock()
	c := e.client
	e.mu.Unlock()
	if c == nil {
		return out, ErrClosed
	}
	fds := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}, {Fd: int32(e.wake[0]), Events: unix.POLLIN}}
	for {
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return out, fmt.Errorf("waiting for the ALSA sequencer: %w", err)
		}
		if fds[1].Revents != 0 {
			return out, ErrClosed
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			if fds[0].Revents != 0 {
				return out, ErrClosed // the sequencer hung up
			}
			continue
		}
		n, err := unix.Read(c.fd, e.inbuf)
		// ENOSPC says that more arrived than the kernel kept for this
		// client; it has thrown the lot away and goes on from here.
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.ENOSPC) {
			continue
		}
		if err != nil {
			return out, fmt.Errorf("reading from the ALSA sequencer: %w", err)
		}
		return decodeAll(e.inbuf[:n], out), nil
	}
}

// ErrClosed is returned by [Endpoint.Send] after the endpoint was closed.
var ErrClosed = errors.New("MIDI endpoint is closed")

// Send delivers the messages, in order, to every program connected to the
// device. With nothing connected the messages are discarded.
func (e *Endpoint) Send(msgs []midi.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client == nil {
		return ErrClosed
	}
	e.buf = e.buf[:0]
	for _, m := range msgs {
		if ev, ok := encode(m, e.port); ok {
			e.buf = append(e.buf, ev)
		}
	}
	return e.client.write(e.buf)
}

// Close removes the device. Closing an endpoint twice is not an error.
func (e *Endpoint) Close() error {
	e.mu.Lock()
	c := e.client
	e.client = nil
	e.mu.Unlock()
	if c == nil {
		return nil
	}
	if e.duplex {
		// End a Receive that waits, and let it leave before what it
		// uses is closed.
		unix.Write(e.wake[1], []byte{0})
		e.receiving.Lock()
		unix.Close(e.wake[0])
		unix.Close(e.wake[1])
		e.receiving.Unlock()
	}
	return c.close()
}

// encode converts a message to a sequencer event sent from the given port to
// all of its subscribers, for immediate delivery. It reports false for a
// message that has no sequencer equivalent.
func encode(m midi.Message, source addr) (event, bool) {
	// The MIDI 1.0 encoding supplies the scaled-down values, including the
	// rule that a note on never carries velocity zero.
	var raw [3]byte
	b := m.AppendBytes(raw[:0])
	if len(b) < 2 {
		return event{}, false
	}
	channel, d1 := b[0]&0x0F, b[1]
	var d2 uint8
	if len(b) == 3 {
		d2 = b[2]
	}

	ev := event{
		Queue:  queueDirect,
		Source: source,
		Dest:   addr{Client: addressSubscribers, Port: addressUnknown},
	}
	// note fills struct snd_seq_ev_note; ctrl fills struct snd_seq_ev_ctrl.
	note := func(typ uint8) {
		ev.Type = typ
		ev.Data[0], ev.Data[1], ev.Data[2] = channel, d1, d2
	}
	ctrl := func(typ uint8, param uint32, value int32) {
		ev.Type = typ
		ev.Data[0] = channel
		native.PutUint32(ev.Data[4:], param)
		native.PutUint32(ev.Data[8:], uint32(value))
	}

	switch m.Kind {
	case midi.KindNoteOn:
		note(eventNoteOn)
	case midi.KindNoteOff:
		note(eventNoteOff)
	case midi.KindPolyPressure:
		note(eventKeyPress)
	case midi.KindControlChange:
		ctrl(eventController, uint32(d1), int32(d2))
	case midi.KindProgramChange:
		ctrl(eventPgmChange, 0, int32(d1))
	case midi.KindChannelPressure:
		ctrl(eventChanPress, 0, int32(d1))
	case midi.KindPitchBend:
		// The sequencer centers pitch bend on zero.
		ctrl(eventPitchBend, 0, int32(d2)<<7|int32(d1)-8192)
	default:
		return event{}, false
	}
	return ev, true
}
