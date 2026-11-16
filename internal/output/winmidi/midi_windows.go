package winmidi

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/gethiox/HIDI/internal/midi"
)

// Class names of Windows.Devices.Midi2.
const (
	classMidiApi         = "Windows.Devices.Midi2.MidiApi"
	classSession         = "Windows.Devices.Midi2.MidiSession"
	classGroup           = "Windows.Devices.Midi2.MidiGroup"
	classEndpointInfo    = "Windows.Devices.Midi2.Enumeration.MidiDeclaredEndpointInfo"
	classFunctionBlock   = "Windows.Devices.Midi2.Enumeration.MidiFunctionBlock"
	classVirtualConfig   = "Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDeviceCreationConfig"
	classVirtualManager  = "Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDeviceManager"
	minimumWindowsBuild  = 26200 // Windows 11 25H2
	sendSucceeded        = 0x80000000
	pluginAddSucceeded   = 0
	blockDirectionOutput = 2 // the block is a source of messages
	blockHintSender      = 2
	blockIsMidi10        = 1 // a MIDI 1.0 port, without the 31.25 kbit/s limit
)

// Interface identifiers, from src/in-box/Inc/midi_sdk_idl_defs.h of
// github.com/microsoft/MIDI.
var (
	iidApiStatics           = guid("8087b303-0519-c0de-31d1-ee0010000000")
	iidApiStatics2          = guid("8087b303-0519-c0de-31d1-ee0010000002")
	iidSessionStatics       = guid("8087b303-0519-c0de-31d1-ee0010009000")
	iidGroupFactory         = guid("8087b303-0519-c0de-31d1-ff0010002000")
	iidEndpointInfoFactory  = guid("8087b303-0519-c0de-31d1-ff0040012000")
	iidFunctionBlock        = guid("8087b303-0519-c0de-31d1-dd004000f000")
	iidVirtualConfigFactory = guid("8087b303-0519-c0de-31d1-ff00F0403000")
	iidVirtualManager       = guid("8087b303-0519-c0de-31d1-ee00F0404000")
	iidProcessingPlugin     = guid("8087b303-0519-c0de-31d1-cc001000F040")
	iidConnectionSource     = guid("8087b303-0519-c0de-31d1-cc001000F030")
	iidConnectionRaw        = guid("8087b303-0519-31d1-31d1-000000000020")
)

// Slots of the methods used, in each interface's table. The first six of a
// WinRT interface are inherited.
const (
	slotApiEnsureServiceAvailable = 6
	slotApiIsSystemProvided       = 6 // of IMidiApiStatics2

	slotSessionCreate                       = 6
	slotSessionCreateEndpointConnection     = 10
	slotSessionDisconnectEndpointConnection = 12
	slotClosableClose                       = 6

	slotGroupCreateInstance        = 6
	slotEndpointInfoCreateInstance = 6
	slotConfigCreateInstance       = 6
	slotConfigGetFunctionBlocks    = 21
	slotVectorAppend               = 13

	slotBlockPutNumber     = 8
	slotBlockPutName       = 10
	slotBlockPutIsActive   = 12
	slotBlockPutDirection  = 14
	slotBlockPutUIHint     = 16
	slotBlockPutMidi10     = 18
	slotBlockPutFirstGroup = 20
	slotBlockPutGroupCount = 22

	slotManagerCreateVirtualDevice = 8
	slotDeviceGetEndpointDeviceID  = 6

	slotConnectionOpen           = 8
	slotConnectionAddPlugin      = 10
	slotConnectionSourceGetID    = 12
	slotConnectionRawSendMessage = 5 // not a WinRT interface: starts at 3
)

// step runs one stage of setting a device up, reporting it to trace if
// there is one. The stage is announced before it runs, so that a crash in
// it still shows where it happened.
type tracer struct {
	w io.Writer
}

func (t tracer) step(what string, do func() error) error {
	if t.w != nil {
		fmt.Fprintf(t.w, "  %s ... ", what)
	}
	err := do()
	if t.w != nil {
		if err != nil {
			fmt.Fprintf(t.w, "FAILED: %v\n", err)
		} else {
			fmt.Fprintln(t.w, "ok")
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// Status describes the MIDI support of this Windows.
type Status struct {
	// Build is the build number of Windows.
	Build uint32
	// Found says where the API was found.
	Found string
	// SystemProvided reports that the API is the copy that is part of
	// Windows, as opposed to a preview copy beside the program.
	SystemProvided bool
	// ServiceAvailable reports that the MIDI service is running, or could
	// be started.
	ServiceAvailable bool
}

// Check finds out whether virtual MIDI devices can be created. It returns
// what it learned together with an error that says what is missing.
func Check() (Status, error) {
	var st Status
	_, _, st.Build = windows.RtlGetNtVersionNumbers()
	st.Build &= 0xFFFF
	st.Found = sourceNone.String()
	if st.Build < minimumWindowsBuild {
		return st, fmt.Errorf("Windows MIDI Services needs Windows 11 25H2 (build %d) or later; this is build %d", minimumWindowsBuild, st.Build)
	}
	api, from, err := factory(classMidiApi, iidApiStatics)
	if err != nil {
		return st, err
	}
	defer api.release()
	st.Found = from.String()

	if api2, err := api.query(iidApiStatics2); err == nil {
		var system uint8
		if hr := api2.call(slotApiIsSystemProvided, uintptr(unsafe.Pointer(&system))); !hr.failed() {
			st.SystemProvided = system != 0
		}
		api2.release()
	}
	var available uint8
	if hr := api.call(slotApiEnsureServiceAvailable, uintptr(unsafe.Pointer(&available))); hr.failed() {
		return st, fmt.Errorf("asking for the MIDI service: %w", hr)
	}
	st.ServiceAvailable = available != 0
	if !st.ServiceAvailable {
		return st, errors.New("the Windows MIDI service is not available; it is disabled, or Windows is set to use the old MIDI stack")
	}
	return st, nil
}

// Endpoint is a virtual MIDI device. It implements output.Endpoint.
type Endpoint struct {
	mu           sync.Mutex
	closed       bool
	session      object
	connection   object // IMidiEndpointConnection
	raw          object // IMidiEndpointConnectionRaw, for sending
	device       object // IMidiVirtualDevice
	connectionID windows.GUID
	words        []uint32
}

// ErrClosed is returned by [Endpoint.Send] after the endpoint was closed.
var ErrClosed = errors.New("MIDI endpoint is closed")

// Open creates a virtual MIDI device with the given name.
func Open(name string) (*Endpoint, error) { return open(name, tracer{}) }

// OpenTraced is Open, reporting every stage to w. It is for the diagnostic
// command, which exists to find the stage that fails on a given machine.
func OpenTraced(name string, w io.Writer) (*Endpoint, error) { return open(name, tracer{w}) }

func open(name string, t tracer) (_ *Endpoint, err error) {
	if _, err := Check(); err != nil {
		return nil, err
	}
	e := &Endpoint{}
	defer func() {
		if err != nil {
			e.Close()
		}
	}()

	hName, err := newHString(name)
	if err != nil {
		return nil, err
	}
	defer hName.free()
	// The identifier must differ between devices; the name does, since
	// HIDI numbers devices that share one.
	hInstance, err := newHString("HIDI-" + name)
	if err != nil {
		return nil, err
	}
	defer hInstance.free()
	hDescription, _ := newHString("An instrument made from an input device by HIDI")
	defer hDescription.free()
	hManufacturer, _ := newHString("HIDI")
	defer hManufacturer.free()

	var info, config, blocks, block, group object
	defer func() {
		for _, o := range []object{info, config, blocks, block, group} {
			o.release()
		}
	}()

	err = t.step("describing the device", func() error {
		f, _, err := factory(classEndpointInfo, iidEndpointInfoFactory)
		if err != nil {
			return err
		}
		defer f.release()
		// name, product instance id, MIDI 1.0, MIDI 2.0, receiving and
		// sending jitter-reduction timestamps, static function blocks,
		// number of function blocks, UMP version 1.1.
		hr := f.call(slotEndpointInfoCreateInstance, uintptr(hName), uintptr(hInstance),
			boolArg(true), boolArg(false), boolArg(false), boolArg(false), boolArg(true),
			1, 1, 1, uintptr(unsafe.Pointer(&info.ptr)))
		return check(hr, info)
	})
	if err != nil {
		return nil, err
	}

	err = t.step("preparing the device's settings", func() error {
		f, _, err := factory(classVirtualConfig, iidVirtualConfigFactory)
		if err != nil {
			return err
		}
		defer f.release()
		hr := f.call(slotConfigCreateInstance, uintptr(hName), uintptr(hDescription), uintptr(hManufacturer),
			uintptr(info.ptr), uintptr(unsafe.Pointer(&config.ptr)))
		return check(hr, config)
	})
	if err != nil {
		return nil, err
	}

	// A device declares at least one function block: here one output on
	// the first group, which is what a MIDI 1.0 instrument is.
	err = t.step("declaring the device's output", func() error {
		f, _, err := factory(classGroup, iidGroupFactory)
		if err != nil {
			return err
		}
		defer f.release()
		if err := check(f.call(slotGroupCreateInstance, 0, uintptr(unsafe.Pointer(&group.ptr))), group); err != nil {
			return fmt.Errorf("group: %w", err)
		}
		if block, err = activate(classFunctionBlock, iidFunctionBlock); err != nil {
			return err
		}
		for _, set := range []struct {
			slot  int
			value uintptr
		}{
			{slotBlockPutNumber, 0},
			{slotBlockPutName, uintptr(hName)},
			{slotBlockPutIsActive, boolArg(true)},
			{slotBlockPutDirection, blockDirectionOutput},
			{slotBlockPutUIHint, blockHintSender},
			{slotBlockPutMidi10, blockIsMidi10},
			{slotBlockPutFirstGroup, uintptr(group.ptr)},
			{slotBlockPutGroupCount, 1},
		} {
			if hr := block.call(set.slot, set.value); hr.failed() {
				return fmt.Errorf("function block setting in slot %d: %w", set.slot, hr)
			}
		}
		if err := check(config.call(slotConfigGetFunctionBlocks, uintptr(unsafe.Pointer(&blocks.ptr))), blocks); err != nil {
			return fmt.Errorf("list of function blocks: %w", err)
		}
		if hr := blocks.call(slotVectorAppend, uintptr(block.ptr)); hr.failed() {
			return fmt.Errorf("adding the function block: %w", hr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = t.step("creating the device", func() error {
		manager, _, err := factory(classVirtualManager, iidVirtualManager)
		if err != nil {
			return err
		}
		defer manager.release()
		hr := manager.call(slotManagerCreateVirtualDevice, uintptr(config.ptr), uintptr(unsafe.Pointer(&e.device.ptr)))
		return check(hr, e.device)
	})
	if err != nil {
		return nil, err
	}

	err = t.step("opening a MIDI session", func() error {
		statics, _, err := factory(classSession, iidSessionStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		return check(statics.call(slotSessionCreate, uintptr(hName), uintptr(unsafe.Pointer(&e.session.ptr))), e.session)
	})
	if err != nil {
		return nil, err
	}

	err = t.step("connecting to the device", func() error {
		var id hstring
		if hr := e.device.call(slotDeviceGetEndpointDeviceID, uintptr(unsafe.Pointer(&id))); hr.failed() {
			return fmt.Errorf("the device's identifier: %w", hr)
		}
		defer id.free()
		hr := e.session.call(slotSessionCreateEndpointConnection, uintptr(id), uintptr(unsafe.Pointer(&e.connection.ptr)))
		if err := check(hr, e.connection); err != nil {
			return err
		}
		if src, err := e.connection.query(iidConnectionSource); err == nil {
			src.call(slotConnectionSourceGetID, uintptr(unsafe.Pointer(&e.connectionID)))
			src.release()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The device object is what answers other programs' questions about
	// the device; it does so as a plugin of the connection.
	err = t.step("attaching the device to the connection", func() error {
		plugin, err := e.device.query(iidProcessingPlugin)
		if err != nil {
			return err
		}
		defer plugin.release()
		var result int32
		if hr := e.connection.call(slotConnectionAddPlugin, uintptr(plugin.ptr), uintptr(unsafe.Pointer(&result))); hr.failed() {
			return hr
		}
		if result != pluginAddSucceeded {
			return fmt.Errorf("refused with result %d", result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Opening is what makes the device appear to other programs.
	err = t.step("opening the connection", func() error {
		var opened uint8
		if hr := e.connection.call(slotConnectionOpen, uintptr(unsafe.Pointer(&opened))); hr.failed() {
			return hr
		}
		if opened == 0 {
			return errors.New("the service refused")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = t.step("getting the fast way to send", func() error {
		e.raw, err = e.connection.query(iidConnectionRaw)
		return err
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// check turns the result of a call that returns an object into an error.
// The API reports most failures by returning nothing.
func check(hr hresult, out object) error {
	if hr.failed() {
		return hr
	}
	if out.isNil() {
		return errors.New("nothing was returned")
	}
	return nil
}

// Send delivers the messages, in order, to every program connected to the
// device.
func (e *Endpoint) Send(msgs []midi.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.words = e.words[:0]
	for _, m := range msgs {
		// A message that cannot be encoded comes back as zero.
		if word := m.UMP(0); word != 0 {
			e.words = append(e.words, word)
		}
	}
	if len(e.words) == 0 {
		return nil
	}
	// A timestamp of zero means now.
	hr := e.raw.call(slotConnectionRawSendMessage, 0, uintptr(len(e.words)), uintptr(unsafe.Pointer(&e.words[0])))
	if hr.failed() {
		return fmt.Errorf("sending to Windows MIDI Services: %w", hr)
	}
	return nil
}

// Close removes the device. Closing an endpoint twice is not an error.
func (e *Endpoint) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	e.raw.release()
	if !e.session.isNil() && !e.connection.isNil() && e.connectionID != (windows.GUID{}) {
		e.session.call(slotSessionDisconnectEndpointConnection, guidArgs(&e.connectionID)...)
	}
	e.connection.release()
	e.device.release()
	if !e.session.isNil() {
		if closable, err := e.session.query(iidIClosable); err == nil {
			closable.call(slotClosableClose)
			closable.release()
		}
		e.session.release()
	}
	return nil
}
