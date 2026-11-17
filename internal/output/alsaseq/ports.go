//go:build linux

package alsaseq

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/gethiox/HIDI/internal/output"
)

// systemClient is the sequencer's own client, with its timer and
// announcement ports. They are not MIDI destinations.
const systemClient = 0

// Ports lists the sequencer ports an endpoint can be connected to: those
// that accept MIDI from other clients. The ID of a port is its address in
// the form "client:port", as aconnect prints it.
func Ports() ([]output.Port, error) { return listPorts(acceptsConnections) }

// Sources lists the sequencer ports an [Input] can be connected to: those
// that other clients may read from, which includes HIDI's instruments.
func Sources() ([]output.Port, error) { return listPorts(offersConnections) }

// listPorts returns the ports of all other clients whose capabilities pass
// the filter.
//
// One client is kept for listing for as long as the program runs. HIDI
// lists once a second while it keeps instruments connected to ports, and
// a client that came and went each time had the kernel announce both to
// every program that follows such news.
func listPorts(wanted func(capability uint32) bool) ([]output.Port, error) {
	lister.mu.Lock()
	defer lister.mu.Unlock()
	if lister.client == nil {
		c, err := openClient("HIDI", unix.O_WRONLY)
		if err != nil {
			return nil, err
		}
		lister.client = c
	}
	ports, err := lister.client.ports(wanted)
	if err != nil {
		// Whatever is wrong with the client, the next listing starts anew.
		lister.client.close()
		lister.client = nil
	}
	return ports, err
}

// lister is the client that lists ports.
var lister struct {
	mu     sync.Mutex
	client *client
}

func (c *client) ports(wanted func(capability uint32) bool) ([]output.Port, error) {
	var ports []output.Port
	client := clientInfo{Client: -1}
	for {
		// The kernel fills in the client with the next higher number.
		if err := c.ioctl(ioctlNextClient, unsafe.Pointer(&client)); err != nil {
			if errors.Is(err, unix.ENOENT) {
				return ports, nil
			}
			return nil, fmt.Errorf("listing sequencer clients: %w", err)
		}
		if client.Client == systemClient || client.Client == int32(c.id) {
			continue
		}
		clientName := unix.ByteSliceToString(client.Name[:])

		// Port 255 is before the first: the kernel adds one, which wraps.
		port := portInfo{Addr: addr{Client: uint8(client.Client), Port: 255}}
		for c.ioctl(ioctlNextPort, unsafe.Pointer(&port)) == nil {
			if !wanted(port.Capability) {
				continue
			}
			ports = append(ports, output.Port{
				ID:   formatAddr(port.Addr),
				Name: portName(clientName, unix.ByteSliceToString(port.Name[:])),
			})
		}
	}
}

// acceptsConnections reports whether another client may connect to a port
// with these capabilities to send to it.
func acceptsConnections(capability uint32) bool {
	const need = portCapWrite | portCapSubsWrite
	return capability&need == need && capability&portCapNoExport == 0
}

// offersConnections reports whether another client may connect to a port
// with these capabilities to receive from it.
func offersConnections(capability uint32) bool {
	const need = portCapRead | portCapSubsRead
	return capability&need == need && capability&portCapNoExport == 0
}

// portName joins the names of a client and its port. Many devices give
// both the same name, or start the port's name with the client's.
func portName(client, port string) string {
	if port == "" || port == client {
		return client
	}
	if strings.HasPrefix(port, client) {
		return port
	}
	return client + ": " + port
}

func formatAddr(a addr) string {
	return strconv.Itoa(int(a.Client)) + ":" + strconv.Itoa(int(a.Port))
}

func parseAddr(id string) (addr, error) {
	client, port, ok := strings.Cut(id, ":")
	c, err1 := strconv.ParseUint(client, 10, 8)
	p, err2 := strconv.ParseUint(port, 10, 8)
	if !ok || err1 != nil || err2 != nil {
		return addr{}, fmt.Errorf("%q is not a sequencer port; want client:port, such as 24:0", id)
	}
	return addr{Client: uint8(c), Port: uint8(p)}, nil
}

// Connect makes the endpoint deliver to an existing sequencer port as well
// as to the programs that connect to its own. The connection ends when
// either side disappears. Connect implements [output.Connector].
func (e *Endpoint) Connect(portID string) (already bool, err error) {
	dest, err := parseAddr(portID)
	if err != nil {
		return false, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client == nil {
		return false, ErrClosed
	}
	err = e.client.subscribe(e.port, dest)
	if errors.Is(err, unix.EBUSY) {
		return true, nil // the kernel's answer for an existing connection
	}
	return false, err
}
