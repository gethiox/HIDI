// Package openrgb talks to an OpenRGB server, the program that controls
// the lights of keyboards and other hardware, through its network
// protocol. It knows as much of the protocol as lighting single keys
// takes: listing the controllers with their lights, and setting the
// colour of every light of one.
//
// The protocol is described in OpenRGB's Documentation/OpenRGBSDK.md. The
// client asks for version 1 of it, which every server since OpenRGB 0.5
// speaks and which has all that is needed here.
package openrgb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// DefaultAddress is where an OpenRGB server listens unless told otherwise.
const DefaultAddress = "127.0.0.1:6742"

// TypeKeyboard is the type of a controller that is a keyboard.
const TypeKeyboard = 5

// Packet identifiers.
const (
	packetControllerCount = 0
	packetControllerData  = 1
	packetProtocolVersion = 40
	packetSetClientName   = 50
	packetUpdateLEDs      = 1050
	packetSetCustomMode   = 1100
)

const (
	// version is the protocol version asked for.
	version = 1
	// timeout limits every exchange with the server.
	timeout = 3 * time.Second
	// maxPacket bounds what the server may send in one packet.
	maxPacket = 4 << 20
)

// RGB is a colour.
type RGB struct{ R, G, B uint8 }

// Controller is a piece of hardware whose lights OpenRGB controls.
type Controller struct {
	// Index identifies the controller to the server.
	Index int
	// Type is the kind of hardware; [TypeKeyboard] for a keyboard.
	Type int
	Name string
	// Location says how OpenRGB reaches the hardware, such as
	// "HID: /dev/hidraw3".
	Location string
	// LEDs are the names of the lights, in the order their colours are
	// sent; a keyboard names them by key, such as "Key: A".
	LEDs []string
}

// Client is a connection to an OpenRGB server. It is safe for concurrent
// use.
type Client struct {
	mu   sync.Mutex
	conn net.Conn
	// version is the protocol version in use: the lower of the server's
	// and this client's.
	version uint32
}

// Connect opens a connection to the server at address and introduces
// HIDI by name.
func Connect(address, name string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn}
	// A server that does not know the question does not answer it: that
	// is version 0.
	reply, err := c.ask(0, packetProtocolVersion, binary.LittleEndian.AppendUint32(nil, version), time.Second)
	switch {
	case err == nil && len(reply) >= 4:
		c.version = min(binary.LittleEndian.Uint32(reply), version)
	case errors.Is(err, errNoAnswer):
		c.version = 0
	default:
		conn.Close()
		return nil, fmt.Errorf("asking the OpenRGB server for its version: %w", err)
	}
	if err := c.send(0, packetSetClientName, append([]byte(name), 0)); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

var errNoAnswer = errors.New("the OpenRGB server did not answer")

func (c *Client) send(device uint32, id uint32, data []byte) error {
	packet := make([]byte, 0, 16+len(data))
	packet = append(packet, "ORGB"...)
	packet = binary.LittleEndian.AppendUint32(packet, device)
	packet = binary.LittleEndian.AppendUint32(packet, id)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(data)))
	packet = append(packet, data...)
	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	_, err := c.conn.Write(packet)
	return err
}

// ask sends a packet and returns the data of the answer, which has the
// same identifier. Other packets the server sends in between, such as the
// news that its list of devices changed, are passed over.
func (c *Client) ask(device, id uint32, data []byte, wait time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.send(device, id, data); err != nil {
		return nil, err
	}
	c.conn.SetReadDeadline(time.Now().Add(wait))
	for {
		var header [16]byte
		if _, err := io.ReadFull(c.conn, header[:]); err != nil {
			var timedOut net.Error
			if errors.As(err, &timedOut) && timedOut.Timeout() {
				return nil, errNoAnswer
			}
			return nil, err
		}
		if string(header[:4]) != "ORGB" {
			return nil, errors.New("what answered is not an OpenRGB server")
		}
		size := binary.LittleEndian.Uint32(header[12:])
		if size > maxPacket {
			return nil, fmt.Errorf("the OpenRGB server sent a packet of %d bytes", size)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(c.conn, body); err != nil {
			return nil, err
		}
		if binary.LittleEndian.Uint32(header[8:]) == id {
			return body, nil
		}
	}
}

// Controllers lists the hardware the server controls.
func (c *Client) Controllers() ([]Controller, error) {
	reply, err := c.ask(0, packetControllerCount, nil, timeout)
	if err != nil {
		return nil, err
	}
	if len(reply) < 4 {
		return nil, errors.New("the OpenRGB server sent a short answer")
	}
	count := binary.LittleEndian.Uint32(reply)
	if count > 1024 {
		return nil, fmt.Errorf("the OpenRGB server claims %d controllers", count)
	}
	var request []byte
	if c.version >= 1 {
		request = binary.LittleEndian.AppendUint32(nil, c.version)
	}
	controllers := make([]Controller, 0, count)
	for i := range count {
		reply, err := c.ask(i, packetControllerData, request, timeout)
		if err != nil {
			return nil, err
		}
		ctl, err := parseController(reply, c.version)
		if err != nil {
			return nil, fmt.Errorf("controller %d: %w", i, err)
		}
		ctl.Index = int(i)
		controllers = append(controllers, ctl)
	}
	return controllers, nil
}

// SetCustomMode puts a controller into the mode in which each light takes
// the colour it is sent.
func (c *Client) SetCustomMode(index int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.send(uint32(index), packetSetCustomMode, nil)
}

// SetLEDs sets the colour of every light of a controller, in the order of
// [Controller.LEDs].
func (c *Client) SetLEDs(index int, colours []RGB) error {
	size := 4 + 2 + 4*len(colours)
	data := make([]byte, 0, size)
	data = binary.LittleEndian.AppendUint32(data, uint32(size))
	data = binary.LittleEndian.AppendUint16(data, uint16(len(colours)))
	for _, colour := range colours {
		data = append(data, colour.R, colour.G, colour.B, 0)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.send(uint32(index), packetUpdateLEDs, data)
}

// Sync returns once the server has taken everything sent to a controller
// so far. It asks for the controller's description, a question the server
// answers in the order it gets to it: newer servers take an update of the
// lights only when the keyboard is done with the one before, so the answer
// also tells that the keyboard keeps up. The protocol has no lighter
// question that waits its turn.
func (c *Client) Sync(index int) error {
	var request []byte
	if c.version >= 1 {
		request = binary.LittleEndian.AppendUint32(nil, c.version)
	}
	_, err := c.ask(uint32(index), packetControllerData, request, timeout)
	return err
}

// reader walks through the description of a controller.
type reader struct {
	data []byte
	err  error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.data) {
		r.err = errors.New("the description of the controller ends too early")
		return nil
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out
}

func (r *reader) u16() int {
	if b := r.take(2); b != nil {
		return int(binary.LittleEndian.Uint16(b))
	}
	return 0
}

func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *reader) skip(n int) { r.take(n) }

// text reads a string: its length, then its bytes with a zero at the end.
func (r *reader) text() string {
	b := r.take(r.u16())
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}

// parseController reads the description of a controller as the server
// sends it for the given protocol version, 0 or 1.
func parseController(data []byte, version uint32) (Controller, error) {
	r := &reader{data: data}
	r.u32() // the size of the whole description
	ctl := Controller{Type: int(int32(r.u32())), Name: r.text()}
	if version >= 1 {
		r.text() // vendor
	}
	r.text() // description
	r.text() // version
	r.text() // serial
	ctl.Location = r.text()

	modes := r.u16()
	r.u32() // the active mode
	for range modes {
		r.text()            // name
		r.skip(9 * 4)       // value, flags, speeds, colour counts, direction, colour mode
		r.skip(4 * r.u16()) // colours
	}
	for range r.u16() { // zones
		r.text()        // name
		r.skip(4 * 4)   // type, least, most and present number of lights
		r.skip(r.u16()) // the map of a zone that is a grid
	}
	leds := r.u16()
	for range leds {
		ctl.LEDs = append(ctl.LEDs, r.text())
		r.u32() // value
	}
	if r.err != nil {
		return Controller{}, r.err
	}
	return ctl, nil
}
