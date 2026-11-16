//go:build linux

package alsaseq

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// devicePath is the character device of the ALSA sequencer.
const devicePath = "/dev/snd/seq"

// ioctl request numbers from sound/asequencer.h. They encode the size of
// their argument, which is the same on every architecture Linux supports.
const (
	ioctlClientID      = 0x80045301
	ioctlGetClientInfo = 0xc0bc5310
	ioctlSetClientInfo = 0x40bc5311
	ioctlCreatePort    = 0xc0a85320
	ioctlSubscribePort = 0x40505330
	ioctlNextClient    = 0xc0bc5351
	ioctlNextPort      = 0xc0a85352
)

// Port capabilities and types.
const (
	portCapRead      = 1 << 0 // the port can be read from
	portCapWrite     = 1 << 1 // the port can be written to
	portCapSubsRead  = 1 << 5 // other clients may subscribe to read from it
	portCapSubsWrite = 1 << 6 // other clients may subscribe to write to it
	portCapNoExport  = 1 << 7 // only its owner may connect it

	portTypeMIDIGeneric = 1 << 1
	portTypeSoftware    = 1 << 17
	portTypeApplication = 1 << 20
)

// Event types.
const (
	eventNoteOn     = 6
	eventNoteOff    = 7
	eventKeyPress   = 8
	eventController = 10
	eventPgmChange  = 11
	eventChanPress  = 12
	eventPitchBend  = 13
)

// Special addresses and queues.
const (
	queueDirect        = 253 // deliver immediately, without a queue
	addressSubscribers = 254 // deliver to every subscriber of the source port
	addressUnknown     = 253
)

// addr is struct snd_seq_addr: a port of a client.
type addr struct {
	Client, Port uint8
}

// event is struct snd_seq_event with a fixed-length payload.
type event struct {
	Type   uint8
	Flags  uint8
	Tag    uint8
	Queue  uint8
	Time   [8]byte
	Source addr
	Dest   addr
	// Data holds struct snd_seq_ev_note or struct snd_seq_ev_ctrl.
	Data [12]byte
}

const eventSize = int(unsafe.Sizeof(event{}))

// clientInfo is struct snd_seq_client_info. Only the leading fields are
// used; the rest is carried through unchanged.
type clientInfo struct {
	Client int32
	Type   int32
	Name   [64]byte
	_      [116]byte
}

// portInfo is struct snd_seq_port_info. Only the leading fields are used.
type portInfo struct {
	Addr       addr
	Name       [64]byte
	Capability uint32
	Type       uint32
	_          [92]byte
}

// portSubscribe is struct snd_seq_port_subscribe.
type portSubscribe struct {
	Sender addr
	Dest   addr
	_      [76]byte
}

// client is an open connection to the sequencer.
type client struct {
	fd int
	id uint8
}

// openClient connects to the sequencer and names the client. flags is
// unix.O_WRONLY for a client that only sends, which spares the kernel from
// keeping an input queue for it, or unix.O_RDWR.
func openClient(name string, flags int) (*client, error) {
	fd, err := unix.Open(devicePath, flags|unix.O_CLOEXEC, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENODEV):
			return nil, fmt.Errorf("the ALSA sequencer is not available (%s: %w); load the snd-seq kernel module", devicePath, err)
		case errors.Is(err, unix.EACCES):
			return nil, fmt.Errorf("no permission to use the ALSA sequencer (%s: %w); add your user to the audio group", devicePath, err)
		}
		return nil, fmt.Errorf("opening %s: %w", devicePath, err)
	}
	c := &client{fd: fd}

	var id int32
	if err := c.ioctl(ioctlClientID, unsafe.Pointer(&id)); err != nil {
		c.close()
		return nil, fmt.Errorf("reading the sequencer client id: %w", err)
	}
	c.id = uint8(id)

	info := clientInfo{Client: id}
	if err := c.ioctl(ioctlGetClientInfo, unsafe.Pointer(&info)); err != nil {
		c.close()
		return nil, fmt.Errorf("reading the sequencer client info: %w", err)
	}
	setName(&info.Name, name)
	if err := c.ioctl(ioctlSetClientInfo, unsafe.Pointer(&info)); err != nil {
		c.close()
		return nil, fmt.Errorf("naming the sequencer client: %w", err)
	}
	return c, nil
}

func (c *client) ioctl(request uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(c.fd), request, uintptr(arg))
		switch errno {
		case 0:
			return nil
		case unix.EINTR:
			continue
		}
		return errno
	}
}

// createPort adds a port to the client and returns its address.
func (c *client) createPort(name string, capability, typ uint32) (addr, error) {
	info := portInfo{Addr: addr{Client: c.id}, Capability: capability, Type: typ}
	setName(&info.Name, name)
	if err := c.ioctl(ioctlCreatePort, unsafe.Pointer(&info)); err != nil {
		return addr{}, fmt.Errorf("creating sequencer port %q: %w", name, err)
	}
	return info.Addr, nil
}

// subscribe connects a sender port to a destination port, so that events the
// sender addresses to its subscribers reach the destination. The client must
// own one of the two ports.
func (c *client) subscribe(sender, dest addr) error {
	sub := portSubscribe{Sender: sender, Dest: dest}
	if err := c.ioctl(ioctlSubscribePort, unsafe.Pointer(&sub)); err != nil {
		return fmt.Errorf("connecting sequencer port %d:%d to %d:%d: %w",
			sender.Client, sender.Port, dest.Client, dest.Port, err)
	}
	return nil
}

// write sends events. The kernel accepts any number of whole events per
// write and may accept fewer than offered.
func (c *client) write(events []event) error {
	if len(events) == 0 {
		return nil
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&events[0])), len(events)*eventSize)
	for len(buf) > 0 {
		n, err := unix.Write(c.fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("writing to the ALSA sequencer: %w", err)
		}
		buf = buf[n:]
	}
	return nil
}

func (c *client) close() error {
	return unix.Close(c.fd)
}

// setName copies name into a fixed-size, NUL-terminated C string field,
// truncating it if necessary.
func setName(field *[64]byte, name string) {
	*field = [64]byte{}
	copy(field[:len(field)-1], name)
}

// The sequencer uses the byte order of the machine.
var native = binary.NativeEndian
