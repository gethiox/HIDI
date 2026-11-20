package openrgb

import (
	"encoding/binary"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
)

// builder writes a controller description the way the server does.
type builder struct{ b []byte }

func (b *builder) u16(v int)    { b.b = binary.LittleEndian.AppendUint16(b.b, uint16(v)) }
func (b *builder) u32(v uint32) { b.b = binary.LittleEndian.AppendUint32(b.b, v) }
func (b *builder) text(s string) {
	b.u16(len(s) + 1)
	b.b = append(append(b.b, s...), 0)
}

// describe builds the description of a keyboard for a protocol version.
func describe(version uint32, name, location string, leds []string) []byte {
	b := &builder{}
	b.u32(0) // size, filled in below
	b.u32(TypeKeyboard)
	b.text(name)
	if version >= 1 {
		b.text("Vendor")
	}
	b.text("A keyboard")
	b.text("1.0")
	b.text("SERIAL")
	b.text(location)
	b.u16(2) // modes
	b.u32(0)
	for _, mode := range []struct {
		name    string
		colours int
	}{{"Direct", 0}, {"Wave", 2}} {
		b.text(mode.name)
		for range 9 {
			b.u32(7)
		}
		b.u16(mode.colours)
		for range mode.colours {
			b.u32(0x00ff00ff)
		}
	}
	b.u16(2) // zones: one a grid, one a strip
	b.text("Keys")
	for range 4 {
		b.u32(1)
	}
	b.u16(8 + 4*6) // a grid of two by three
	b.u32(2)
	b.u32(3)
	for range 6 {
		b.u32(0xffffffff)
	}
	b.text("Strip")
	for range 4 {
		b.u32(1)
	}
	b.u16(0)
	b.u16(len(leds))
	for _, led := range leds {
		b.text(led)
		b.u32(0)
	}
	b.u16(len(leds))
	for range leds {
		b.u32(0)
	}
	binary.LittleEndian.PutUint32(b.b, uint32(len(b.b)))
	return b.b
}

// server is an OpenRGB server as far as the client talks to it.
type server struct {
	t        *testing.T
	listener net.Listener
	version  uint32 // 0 for a server that does not know the question
	leds     []string

	mu      sync.Mutex
	name    string
	custom  []uint32
	colours map[uint32][]RGB
}

func startServer(t *testing.T, version uint32) *server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no network here: %v", err)
	}
	s := &server{t: t, listener: l, version: version, leds: []string{"Key: A", "Key: Escape", "Logo"}, colours: map[uint32][]RGB{}}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *server) reply(conn net.Conn, device, id uint32, data []byte) {
	packet := append([]byte("ORGB"), make([]byte, 12)...)
	binary.LittleEndian.PutUint32(packet[4:], device)
	binary.LittleEndian.PutUint32(packet[8:], id)
	binary.LittleEndian.PutUint32(packet[12:], uint32(len(data)))
	conn.Write(append(packet, data...))
}

func (s *server) serve(conn net.Conn) {
	defer conn.Close()
	for {
		var header [16]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		device := binary.LittleEndian.Uint32(header[4:])
		id := binary.LittleEndian.Uint32(header[8:])
		data := make([]byte, binary.LittleEndian.Uint32(header[12:]))
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}
		switch id {
		case packetProtocolVersion:
			if s.version > 0 {
				s.reply(conn, 0, id, binary.LittleEndian.AppendUint32(nil, s.version))
			}
		case packetSetClientName:
			s.mu.Lock()
			s.name = string(data[:len(data)-1])
			s.mu.Unlock()
		case packetControllerCount:
			// News in between must not be taken for the answer.
			s.reply(conn, 0, 100, nil)
			s.reply(conn, 0, id, binary.LittleEndian.AppendUint32(nil, 2))
		case packetControllerData:
			asked := uint32(0)
			if len(data) >= 4 {
				asked = binary.LittleEndian.Uint32(data)
			}
			if asked > s.version {
				s.t.Errorf("the client asked for version %d of a server that has %d", asked, s.version)
			}
			name := []string{"First keyboard", "Second keyboard"}[device]
			s.reply(conn, device, id, describe(asked, name, "HID: /dev/hidraw3", s.leds))
		case packetSetCustomMode:
			s.mu.Lock()
			s.custom = append(s.custom, device)
			s.mu.Unlock()
		case packetUpdateLEDs:
			n := int(binary.LittleEndian.Uint16(data[4:]))
			colours := make([]RGB, n)
			for i := range colours {
				colours[i] = RGB{data[6+4*i], data[7+4*i], data[8+4*i]}
			}
			s.mu.Lock()
			s.colours[device] = colours
			s.mu.Unlock()
		}
	}
}

func TestClient(t *testing.T) {
	for _, serverVersion := range []uint32{0, 1, 4} {
		s := startServer(t, serverVersion)
		c, err := Connect(s.listener.Addr().String(), "HIDI")
		if err != nil {
			t.Fatalf("server version %d: Connect: %v", serverVersion, err)
		}
		if want := min(serverVersion, 1); c.version != want {
			t.Errorf("server version %d: the client uses version %d, want %d", serverVersion, c.version, want)
		}
		controllers, err := c.Controllers()
		if err != nil {
			t.Fatalf("server version %d: Controllers: %v", serverVersion, err)
		}
		if len(controllers) != 2 {
			t.Fatalf("server version %d: %d controllers", serverVersion, len(controllers))
		}
		second := controllers[1]
		if second.Index != 1 || second.Type != TypeKeyboard || second.Name != "Second keyboard" ||
			second.Location != "HID: /dev/hidraw3" || !slices.Equal(second.LEDs, s.leds) {
			t.Errorf("server version %d: second controller: %+v", serverVersion, second)
		}

		want := []RGB{{1, 2, 3}, {255, 0, 0}, {0, 0, 0}}
		if err := c.SetCustomMode(1); err != nil {
			t.Fatal(err)
		}
		if err := c.SetLEDs(1, want); err != nil {
			t.Fatal(err)
		}
		// After Sync nothing is left to wait for: the server answers in
		// order, so it has taken what was sent before.
		if err := c.Sync(1); err != nil {
			t.Fatalf("server version %d: Sync: %v", serverVersion, err)
		}
		s.mu.Lock()
		arrived := slices.Equal(s.colours[1], want) && slices.Equal(s.custom, []uint32{1}) && s.name == "HIDI"
		s.mu.Unlock()
		if !arrived {
			t.Errorf("server version %d: Sync returned before the colours had arrived", serverVersion)
		}
		c.Close()
	}
}

func TestConnectFails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no network here: %v", err)
	}
	address := l.Addr().String()
	l.Close()
	if _, err := Connect(address, "HIDI"); err == nil {
		t.Error("connecting to nothing worked")
	}
}

func TestParseControllerRejectsShortData(t *testing.T) {
	whole := describe(1, "Keyboard", "HID: /dev/hidraw0", []string{"Key: A"})
	if _, err := parseController(whole, 1); err != nil {
		t.Fatalf("the whole description: %v", err)
	}
	// Whatever is cut off, the answer is an error and not a crash. The
	// colours after the names of the lights are not read, so a cut there
	// is not noticed.
	for cut := 0; cut < len(whole)-6; cut++ {
		if _, err := parseController(whole[:cut], 1); err == nil {
			t.Fatalf("a description cut to %d of %d bytes was accepted", cut, len(whole))
		}
	}
}
