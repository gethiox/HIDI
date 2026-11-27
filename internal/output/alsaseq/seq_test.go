//go:build linux

package alsaseq

import (
	"testing"
	"unsafe"

	"github.com/gethiox/HIDI/internal/midi"
	"github.com/gethiox/HIDI/internal/output"
)

var _ output.Endpoint = (*Endpoint)(nil)

// The Go structures must have exactly the layout of the kernel's. The
// expected numbers come from compiling sound/asequencer.h.
func TestStructLayout(t *testing.T) {
	sizes := []struct {
		name      string
		got, want uintptr
	}{
		{"snd_seq_addr", unsafe.Sizeof(addr{}), 2},
		{"snd_seq_event", unsafe.Sizeof(event{}), 28},
		{"snd_seq_client_info", unsafe.Sizeof(clientInfo{}), 188},
		{"snd_seq_port_info", unsafe.Sizeof(portInfo{}), 96 + unsafe.Sizeof(uintptr(0)) + 64}, // 168, or 164 with pointers of 4 bytes
		{"snd_seq_port_subscribe", unsafe.Sizeof(portSubscribe{}), 80},
	}
	for _, s := range sizes {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}

	offsets := []struct {
		name      string
		got, want uintptr
	}{
		{"snd_seq_event.source", unsafe.Offsetof(event{}.Source), 12},
		{"snd_seq_event.dest", unsafe.Offsetof(event{}.Dest), 14},
		{"snd_seq_event.data", unsafe.Offsetof(event{}.Data), 16},
		{"snd_seq_client_info.name", unsafe.Offsetof(clientInfo{}.Name), 8},
		{"snd_seq_port_info.name", unsafe.Offsetof(portInfo{}.Name), 2},
		{"snd_seq_port_info.capability", unsafe.Offsetof(portInfo{}.Capability), 68},
		{"snd_seq_port_info.type", unsafe.Offsetof(portInfo{}.Type), 72},
		{"snd_seq_port_subscribe.dest", unsafe.Offsetof(portSubscribe{}.Dest), 2},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Errorf("offsetof(%s) = %d, want %d", o.name, o.got, o.want)
		}
	}

	// The requests about ports, as the kernel's headers give them for
	// pointers of 8 bytes and of 4.
	create, next := uintptr(0xc0a85320), uintptr(0xc0a85352)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		create, next = 0xc0a45320, 0xc0a45352
	}
	if ioctlCreatePort != create || ioctlNextPort != next {
		t.Errorf("CREATE_PORT %#x, QUERY_NEXT_PORT %#x; want %#x, %#x", uintptr(ioctlCreatePort), uintptr(ioctlNextPort), create, next)
	}

	// The ioctl numbers encode the argument sizes in bits 16-29.
	requests := []struct {
		name    string
		request uintptr
		size    uintptr
	}{
		{"CLIENT_ID", ioctlClientID, 4},
		{"GET_CLIENT_INFO", ioctlGetClientInfo, unsafe.Sizeof(clientInfo{})},
		{"SET_CLIENT_INFO", ioctlSetClientInfo, unsafe.Sizeof(clientInfo{})},
		{"CREATE_PORT", ioctlCreatePort, unsafe.Sizeof(portInfo{})},
		{"SUBSCRIBE_PORT", ioctlSubscribePort, unsafe.Sizeof(portSubscribe{})},
		{"UNSUBSCRIBE_PORT", ioctlUnsubscribe, unsafe.Sizeof(portSubscribe{})},
		{"QUERY_NEXT_CLIENT", ioctlNextClient, unsafe.Sizeof(clientInfo{})},
		{"QUERY_NEXT_PORT", ioctlNextPort, unsafe.Sizeof(portInfo{})},
	}
	for _, r := range requests {
		if got := r.request >> 16 & 0x3FFF; got != r.size {
			t.Errorf("ioctl %s encodes size %d, but its argument is %d bytes", r.name, got, r.size)
		}
	}
}

func TestSetName(t *testing.T) {
	var field [64]byte
	setName(&field, "HIDI")
	if string(field[:5]) != "HIDI\x00" {
		t.Errorf("field = %q", field[:8])
	}

	long := make([]byte, 100)
	for i := range long {
		long[i] = 'x'
	}
	setName(&field, string(long))
	if field[62] != 'x' || field[63] != 0 {
		t.Errorf("a long name must be truncated and stay NUL-terminated; tail = %q", field[60:])
	}

	setName(&field, "ab")
	if field[2] != 0 || field[10] != 0 {
		t.Error("setName must clear what a previous name left behind")
	}
}

// decoded is the content of an event in a form that is easy to compare.
type decoded struct {
	typ, channel uint8
	note, vel    uint8  // note events
	param        uint32 // controller events
	value        int32
}

func decodeEvent(ev event) decoded {
	d := decoded{typ: ev.Type, channel: ev.Data[0]}
	switch ev.Type {
	case eventNoteOn, eventNoteOff, eventKeyPress:
		d.note, d.vel = ev.Data[1], ev.Data[2]
	default:
		d.param = native.Uint32(ev.Data[4:])
		d.value = int32(native.Uint32(ev.Data[8:]))
	}
	return d
}

// messages lists every kind of message with the event it must become.
var messages = []struct {
	name string
	msg  midi.Message
	want decoded
}{
	{"note on", midi.NoteOn(1, 60, midi.From7(100)), decoded{typ: eventNoteOn, channel: 1, note: 60, vel: 100}},
	{"note on, lowest velocity", midi.NoteOn(0, 60, 0), decoded{typ: eventNoteOn, note: 60, vel: 1}},
	{"note off", midi.NoteOff(15, 127, midi.From7(64)), decoded{typ: eventNoteOff, channel: 15, note: 127, vel: 64}},
	{"poly pressure", midi.PolyPressure(2, 61, midi.From7(9)), decoded{typ: eventKeyPress, channel: 2, note: 61, vel: 9}},
	{"control change", midi.ControlChange(3, 74, midi.From7(127)), decoded{typ: eventController, channel: 3, param: 74, value: 127}},
	{"program change", midi.ProgramChange(4, 42), decoded{typ: eventPgmChange, channel: 4, value: 42}},
	{"channel pressure", midi.ChannelPressure(5, midi.From7(88)), decoded{typ: eventChanPress, channel: 5, value: 88}},
	{"pitch bend center", midi.PitchBend(6, midi.ValueCenter), decoded{typ: eventPitchBend, channel: 6, value: 0}},
	{"pitch bend up", midi.PitchBend(6, midi.ValueMax), decoded{typ: eventPitchBend, channel: 6, value: 8191}},
	{"pitch bend down", midi.PitchBend(6, 0), decoded{typ: eventPitchBend, channel: 6, value: -8192}},
}

func TestEncode(t *testing.T) {
	source := addr{Client: 130, Port: 2}
	for _, tt := range messages {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := encode(tt.msg, source)
			if !ok {
				t.Fatal("encode reported the message as unsupported")
			}
			if got := decodeEvent(ev); got != tt.want {
				t.Errorf("event = %+v, want %+v", got, tt.want)
			}
			if ev.Source != source || ev.Queue != queueDirect ||
				ev.Dest != (addr{Client: addressSubscribers, Port: addressUnknown}) || ev.Flags != 0 {
				t.Errorf("addressing = %+v, want direct delivery from %+v to subscribers", ev, source)
			}
		})
	}

	if _, ok := encode(midi.Message{}, source); ok {
		t.Error("encode accepted an invalid message")
	}
}

func TestPortNames(t *testing.T) {
	for _, tt := range []struct{ client, port, want string }{
		{"FluidSynth", "FluidSynth", "FluidSynth"},
		{"FluidSynth", "", "FluidSynth"},
		{"USB MIDI Interface", "USB MIDI Interface MIDI 1", "USB MIDI Interface MIDI 1"},
		{"Midi Through", "Port-0", "Midi Through: Port-0"},
	} {
		if got := portName(tt.client, tt.port); got != tt.want {
			t.Errorf("portName(%q, %q) = %q, want %q", tt.client, tt.port, got, tt.want)
		}
	}
}

func TestPortAddresses(t *testing.T) {
	a, err := parseAddr("128:3")
	if err != nil || a != (addr{Client: 128, Port: 3}) || formatAddr(a) != "128:3" {
		t.Errorf("parseAddr(128:3) = %+v, %v", a, err)
	}
	for _, bad := range []string{"", "128", "128:", ":0", "256:0", "1:256", "a:b", "1:2:3", "-1:0"} {
		if _, err := parseAddr(bad); err == nil {
			t.Errorf("parseAddr(%q) succeeded", bad)
		}
	}
}

func TestAcceptsConnections(t *testing.T) {
	for capability, want := range map[uint32]bool{
		portCapWrite | portCapSubsWrite:               true,
		portCapWrite | portCapSubsWrite | portCapRead: true,
		portCapWrite:                  false, // not open to others
		portCapRead | portCapSubsRead: false, // an output
		portCapWrite | portCapSubsWrite | portCapNoExport: false,
	} {
		if acceptsConnections(capability) != want {
			t.Errorf("acceptsConnections(%#x) = %v, want %v", capability, !want, want)
		}
	}
}

// What encode writes, decode must read back as the same MIDI.
func TestDecodeReversesEncode(t *testing.T) {
	source := addr{Client: 130, Port: 2}
	for _, tt := range messages {
		ev, ok := encode(tt.msg, source)
		if !ok {
			t.Fatalf("%s: not encoded", tt.name)
		}
		got, ok := decode(ev)
		if !ok {
			t.Errorf("%s: not decoded", tt.name)
			continue
		}
		want := tt.msg.AppendBytes(nil)
		if b := got.AppendBytes(nil); string(b) != string(want) {
			t.Errorf("%s: decoded to % x, want % x", tt.name, b, want)
		}
	}
}

func TestDecodeSpecialCases(t *testing.T) {
	source := addr{Client: 130}
	// A note on with velocity zero means note off.
	ev, _ := encode(midi.NoteOn(2, 60, midi.From7(100)), source)
	ev.Data[2] = 0
	if got, ok := decode(ev); !ok || got.Kind != midi.KindNoteOff || got.Channel != 2 {
		t.Errorf("note on with velocity 0 decoded to %v, %v", got, ok)
	}
	// The sequencer's own events and unknown types are not MIDI.
	ev, _ = encode(midi.NoteOn(0, 60, midi.From7(100)), addr{Client: systemClient, Port: 1})
	if _, ok := decode(ev); ok {
		t.Error("an event of the sequencer itself was decoded")
	}
	if _, ok := decode(event{Type: 66, Source: source}); ok {
		t.Error("a port announcement was decoded")
	}
}

func TestOffersConnections(t *testing.T) {
	for capability, want := range map[uint32]bool{
		portCapRead | portCapSubsRead:                   true,
		portCapRead:                                     false,
		portCapWrite | portCapSubsWrite:                 false,
		portCapRead | portCapSubsRead | portCapNoExport: false,
	} {
		if offersConnections(capability) != want {
			t.Errorf("offersConnections(%#x) = %v, want %v", capability, !want, want)
		}
	}
}

// A system exclusive message arrives as an event followed by its bytes,
// which must not be read as events.
func TestDecodeAllSkipsData(t *testing.T) {
	note, _ := encode(midi.NoteOn(0, 60, midi.From7(100)), addr{Client: 130, Port: 0})

	// 56 bytes of data that would read as two note-on events.
	sysex := event{Type: 130, Flags: lengthVariable, Source: addr{Client: 130}}
	native.PutUint32(sysex.Data[0:], 56)
	var buf []byte
	add := func(ev event) {
		buf = append(buf, unsafe.Slice((*byte)(unsafe.Pointer(&ev)), eventSize)...)
	}
	add(sysex)
	add(note) // the data: two events' worth
	add(note)
	add(note) // and a real note after it

	got := decodeAll(buf, nil)
	if len(got) != 1 || got[0].String() != "NoteOn ch=1 note=C3 velocity=100" {
		t.Errorf("decodeAll = %v, want the one note after the data", got)
	}
	// Data that is cut off ends the reading without a fault.
	if got := decodeAll(buf[:2*eventSize], nil); len(got) != 0 {
		t.Errorf("decodeAll of a cut-off message = %v", got)
	}
}
