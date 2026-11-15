//go:build linux

package alsaseq

import (
	"errors"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/midi"
)

// requireSequencer skips the test on a machine without the ALSA sequencer.
// CI sets HIDI_REQUIRE_ALSA so that a missing sequencer fails there instead
// of silently skipping the only tests that exercise the kernel.
func requireSequencer(t *testing.T) {
	t.Helper()
	fd, err := unix.Open(devicePath, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err == nil {
		unix.Close(fd)
		return
	}
	if os.Getenv("HIDI_REQUIRE_ALSA") != "" {
		t.Fatalf("HIDI_REQUIRE_ALSA is set but the sequencer cannot be opened: %v", err)
	}
	t.Skipf("the ALSA sequencer is not available: %v", err)
}

// receiver is a second sequencer client that listens to an endpoint, the way
// a synthesizer or a DAW would.
type receiver struct {
	t      *testing.T
	client *client
}

func newReceiver(t *testing.T, from addr) *receiver {
	t.Helper()
	c, err := openClient("HIDI test receiver", unix.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.close() })
	port, err := c.createPort("in", portCapWrite|portCapSubsWrite, portTypeMIDIGeneric|portTypeApplication)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.subscribe(from, port); err != nil {
		t.Fatal(err)
	}
	return &receiver{t, c}
}

// read returns the next n events, failing the test if they do not arrive.
func (r *receiver) read(n int) []event {
	r.t.Helper()
	var events []event
	buf := make([]byte, 64*eventSize)
	deadline := time.Now().Add(2 * time.Second)
	for len(events) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			r.t.Fatalf("received %d of %d events before the timeout", len(events), n)
		}
		fds := []unix.PollFd{{Fd: int32(r.client.fd), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, int(remaining.Milliseconds())+1); err != nil && !errors.Is(err, unix.EINTR) {
			r.t.Fatalf("poll: %v", err)
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		got, err := unix.Read(r.client.fd, buf)
		if err != nil {
			r.t.Fatalf("read: %v", err)
		}
		for off := 0; off+eventSize <= got; off += eventSize {
			events = append(events, *(*event)(unsafe.Pointer(&buf[off])))
		}
	}
	return events
}

// clientExists asks the sequencer whether a client id is in use.
func (r *receiver) clientExists(id uint8) bool {
	info := clientInfo{Client: int32(id)}
	return r.client.ioctl(ioctlGetClientInfo, unsafe.Pointer(&info)) == nil
}

// clientName returns the name the sequencer reports for a client.
func (r *receiver) clientName(id uint8) string {
	r.t.Helper()
	info := clientInfo{Client: int32(id)}
	if err := r.client.ioctl(ioctlGetClientInfo, unsafe.Pointer(&info)); err != nil {
		r.t.Fatalf("reading client info: %v", err)
	}
	return unix.ByteSliceToString(info.Name[:])
}

// An endpoint must deliver every kind of message, unchanged and in order, to
// a program connected to it.
func TestEndpointDelivers(t *testing.T) {
	requireSequencer(t)

	ep, err := Open("HIDI test instrument")
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()
	rx := newReceiver(t, ep.port)

	if got := rx.clientName(ep.port.Client); got != "HIDI test instrument" {
		t.Errorf("the device is named %q, want %q", got, "HIDI test instrument")
	}

	msgs := make([]midi.Message, len(messages))
	for i, tt := range messages {
		msgs[i] = tt.msg
	}
	if err := ep.Send(msgs); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for i, ev := range rx.read(len(messages)) {
		if got, want := decodeEvent(ev), messages[i].want; got != want {
			t.Errorf("%s: received %+v, want %+v", messages[i].name, got, want)
		}
		if ev.Source != ep.port {
			t.Errorf("%s: received from %+v, want %+v", messages[i].name, ev.Source, ep.port)
		}
	}
}

// Sending with nothing connected, and sending nothing, must both succeed.
func TestEndpointWithoutListeners(t *testing.T) {
	requireSequencer(t)
	ep, err := Open("HIDI test instrument")
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()
	if err := ep.Send([]midi.Message{midi.NoteOn(0, 60, midi.From7(100))}); err != nil {
		t.Errorf("Send with no listeners: %v", err)
	}
	if err := ep.Send(nil); err != nil {
		t.Errorf("Send of nothing: %v", err)
	}
}

// Closing the endpoint must make the device disappear.
func TestEndpointClose(t *testing.T) {
	requireSequencer(t)
	ep, err := Open("HIDI test instrument")
	if err != nil {
		t.Fatal(err)
	}
	rx := newReceiver(t, ep.port)
	id := ep.port.Client
	if !rx.clientExists(id) {
		t.Fatal("the device is not visible while the endpoint is open")
	}

	if err := ep.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if rx.clientExists(id) {
		t.Error("the device is still visible after Close")
	}
	if err := ep.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := ep.Send([]midi.Message{midi.NoteOn(0, 60, 1)}); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close: err = %v, want ErrClosed", err)
	}
}

// Several endpoints must coexist as separate devices.
func TestEndpointsAreIndependent(t *testing.T) {
	requireSequencer(t)
	a, err := Open("HIDI test A")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open("HIDI test B")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.port.Client == b.port.Client {
		t.Fatal("two endpoints share a sequencer client")
	}

	rxA, rxB := newReceiver(t, a.port), newReceiver(t, b.port)
	if err := a.Send([]midi.Message{midi.NoteOn(0, 60, midi.From7(1))}); err != nil {
		t.Fatal(err)
	}
	if err := b.Send([]midi.Message{midi.NoteOn(0, 72, midi.From7(1))}); err != nil {
		t.Fatal(err)
	}
	if got := decodeEvent(rxA.read(1)[0]).note; got != 60 {
		t.Errorf("listener of A received note %d, want 60", got)
	}
	if got := decodeEvent(rxB.read(1)[0]).note; got != 72 {
		t.Errorf("listener of B received note %d, want 72", got)
	}
}
