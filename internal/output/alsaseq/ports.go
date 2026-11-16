//go:build linux

package alsaseq

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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
func Ports() ([]output.Port, error) {
	c, err := openClient("HIDI", unix.O_WRONLY)
	if err != nil {
		return nil, err
	}
	defer c.close()

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
			if !acceptsConnections(port.Capability) {
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
