package winmidi

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
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
	iidConfigManager        = guid("8087b303-0519-c0de-31d1-ee0060001000")
	iidPluginConfig         = guid("8087b303-0519-c0de-31d1-cc0060006000")
)

const classConfigManager = "Windows.Devices.Midi2.ServiceConfig.MidiServiceTransportPluginConfigManager"

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

	slotManagerIsTransportAvailable = 6
	slotManagerCreateVirtualDevice  = 8

	slotEndpointInfoGetName       = 7
	slotEndpointInfoGetInstanceID = 9

	slotConfigManagerSendUpdate   = 6
	slotResponseGetStatus         = 6
	slotResponseGetErrorCode      = 7
	slotResponseGetErrorMessage   = 8
	slotDeviceGetEndpointDeviceID = 6

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
	// stuck is set when a stage did not return in time. Its call into
	// Windows is then still running, and nothing it may be using can be
	// released or reused.
	stuck atomic.Bool
}

// Limits on the calls into Windows. They go to the MIDI service, another
// process, and have been seen never to return. HIDI must not hang with
// them: a hung HIDI can neither be used nor stopped.
var (
	checkTimeout = 15 * time.Second // may have to start the service
	stepTimeout  = 10 * time.Second
	sendTimeout  = 2 * time.Second
	closeTimeout = 3 * time.Second
)

// errStuck is what a call that did not return in time is reported as.
var errStuck = errors.New("Windows did not answer in time. The MIDI service may be stuck: " +
	"restart the service named midisrv (as administrator: Restart-Service midisrv), or restart Windows")

// within runs fn and waits for it at most d. It reports false if fn has
// not returned by then; fn keeps running, since a call into Windows cannot
// be interrupted.
func within(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// serviceStuck is set once a call into Windows did not return. Whatever
// is asked of the service after that would wait behind it, so nothing more
// is asked: ten instruments then fail at once and say why, instead of
// each waiting its turn to find out the same.
var serviceStuck atomic.Bool

func (t *tracer) step(what string, do func() error) error {
	if t.stuck.Load() || serviceStuck.Load() {
		return errStuck
	}
	if t.w != nil {
		fmt.Fprintf(t.w, "  %s ... ", what)
	}
	var err error
	if !within(stepTimeout, func() { err = do() }) {
		t.stuck.Store(true)
		serviceStuck.Store(true)
		if t.w != nil {
			fmt.Fprintf(t.w, "NO ANSWER after %v\n", stepTimeout)
		}
		return fmt.Errorf("%s: %w", what, errStuck)
	}
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
	var err error
	if !within(checkTimeout, func() { st, err = check1() }) {
		var build Status
		_, _, build.Build = windows.RtlGetNtVersionNumbers()
		build.Build &= 0xFFFF
		build.Found = "unknown"
		return build, errStuck
	}
	return st, err
}

func check1() (Status, error) {
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
	trace        *tracer
}

// ErrClosed is returned by [Endpoint.Send] after the endpoint was closed.
var ErrClosed = errors.New("MIDI endpoint is closed")

// Open creates a virtual MIDI device with the given name.
func Open(name string) (*Endpoint, error) { return open(name, &tracer{}) }

// OpenTraced is Open, reporting every stage to w. It is for the diagnostic
// command, which exists to find the stage that fails on a given machine.
func OpenTraced(name string, w io.Writer) (*Endpoint, error) { return open(name, &tracer{w: w}) }

// service is held by whatever creates or removes a device. Windows MIDI
// Services did not answer at all when ten devices were created at once;
// one after another is what its own samples do, and costs nothing that
// matters here.
var service sync.Mutex

// Removal chooses how a device is given up. It exists to find out, with
// the diagnostic command, what Windows MIDI Services takes well: on the
// machine this was first tried on, disconnecting never returned.
type Removal string

const (
	// RemoveBySession closes the session, which ends its connections.
	RemoveBySession Removal = "session"
	// RemoveByDisconnect disconnects first, as Microsoft's sample does.
	RemoveByDisconnect Removal = "disconnect"
	// RemoveByRelease only lets go of the objects.
	RemoveByRelease Removal = "release"
)

// HowToRemove is the way devices are given up.
var HowToRemove = RemoveBySession

func open(name string, t *tracer) (_ *Endpoint, err error) {
	service.Lock()
	defer service.Unlock()
	if serviceStuck.Load() {
		return nil, errStuck
	}
	if _, err := Check(); err != nil {
		return nil, err
	}
	e := &Endpoint{trace: t}
	defer func() {
		if err != nil {
			e.closeLocked()
		}
	}()

	hName, err := newHString(name)
	if err != nil {
		return nil, err
	}
	// The identifier should differ between devices. Two input devices of
	// the same model have the same name, and so far get the same
	// identifier; what Windows does then is not known yet.
	hInstance, err := newHString(instanceID(name))
	if err != nil {
		return nil, err
	}
	hDescription, _ := newHString("An instrument made from an input device by HIDI")
	hManufacturer, _ := newHString("HIDI")

	var info, config, blocks, block, group object
	defer func() {
		if t.stuck.Load() {
			return // a call that never returned may still be using them
		}
		for _, h := range []hstring{hName, hInstance, hDescription, hManufacturer} {
			h.free()
		}
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
		var available uint8
		if hr := manager.call(slotManagerIsTransportAvailable, uintptr(unsafe.Pointer(&available))); hr.failed() {
			return fmt.Errorf("asking for the virtual device transport: %w", hr)
		}
		if available == 0 {
			return errors.New("the MIDI service on this machine has no virtual device transport")
		}
		hr := manager.call(slotManagerCreateVirtualDevice, uintptr(config.ptr), uintptr(unsafe.Pointer(&e.device.ptr)))
		if err := check(hr, e.device); err != nil {
			// The API does not say why. Asking the service the same thing
			// directly returns its answer.
			return fmt.Errorf("%w; %s; the device was described as %s", err, serviceAnswer(config), describeInfo(info))
		}
		return nil
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

// instanceID makes the identifier a device declares from its name. It
// keeps to letters, digits and dashes, as the identifiers in Microsoft's
// samples do.
func instanceID(name string) string {
	id := []byte("HIDI-")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			id = append(id, byte(r))
		default:
			id = append(id, '-')
		}
	}
	return string(id)
}

// describeInfo reads back the name and identifier of a device description,
// which shows whether they arrived as they were given.
func describeInfo(info object) string {
	var name, id hstring
	hr1 := info.call(slotEndpointInfoGetName, uintptr(unsafe.Pointer(&name)))
	hr2 := info.call(slotEndpointInfoGetInstanceID, uintptr(unsafe.Pointer(&id)))
	defer name.free()
	defer id.free()
	if hr1.failed() || hr2.failed() {
		return fmt.Sprintf("unreadable (%v, %v)", hr1, hr2)
	}
	return fmt.Sprintf("name %q, identifier %q", name.String(), id.String())
}

// serviceAnswer sends the device's settings to the MIDI service the way
// creating a device does, and returns what the service says about them.
func serviceAnswer(config object) string {
	manager, _, err := factory(classConfigManager, iidConfigManager)
	if err != nil {
		return "the service could not be asked why: " + err.Error()
	}
	defer manager.release()
	plugin, err := config.query(iidPluginConfig)
	if err != nil {
		return "the service could not be asked why: " + err.Error()
	}
	defer plugin.release()
	var response object
	if err := check(manager.call(slotConfigManagerSendUpdate, uintptr(plugin.ptr), uintptr(unsafe.Pointer(&response.ptr))), response); err != nil {
		return "the service gave no answer: " + err.Error()
	}
	defer response.release()
	var status int32
	var code uint32
	var text hstring
	response.call(slotResponseGetStatus, uintptr(unsafe.Pointer(&status)))
	response.call(slotResponseGetErrorCode, uintptr(unsafe.Pointer(&code)))
	response.call(slotResponseGetErrorMessage, uintptr(unsafe.Pointer(&text)))
	defer text.free()
	return fmt.Sprintf("the MIDI service answered with status %d (%s), error code 0x%08X, message %q",
		status, responseStatus(status), code, text.String())
}

// responseStatus names a MidiServiceConfigResponseStatus.
func responseStatus(status int32) string {
	switch status {
	case 0:
		return "success"
	case 404:
		return "target not found"
	case 600:
		return "configuration empty"
	case 601:
		return "error processing the configuration"
	case 605:
		return "error processing the response"
	case 808:
		return "error from the service"
	case 2600:
		return "not implemented"
	}
	return "unknown"
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
	if e.trace.stuck.Load() {
		return errStuck
	}
	// A timestamp of zero means now.
	var hr hresult
	words := e.words
	if !within(sendTimeout, func() {
		hr = e.raw.call(slotConnectionRawSendMessage, 0, uintptr(len(words)), uintptr(unsafe.Pointer(&words[0])))
	}) {
		e.trace.stuck.Store(true)
		e.words = nil // the call that did not return still reads the old ones
		return fmt.Errorf("sending to Windows MIDI Services: %w", errStuck)
	}
	if hr.failed() {
		return fmt.Errorf("sending to Windows MIDI Services: %w", hr)
	}
	return nil
}

// Close removes the device. Closing an endpoint twice is not an error. If
// Windows does not finish within a few seconds, Close returns an error and
// leaves the rest to the end of the program.
func (e *Endpoint) Close() error {
	service.Lock()
	defer service.Unlock()
	return e.closeLocked()
}

// closeLocked is Close for a caller that holds service.
func (e *Endpoint) closeLocked() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	if e.trace.stuck.Load() || serviceStuck.Load() {
		// An earlier call never returned; another one would not either.
		return errors.New("the MIDI device was not removed, because Windows stopped answering; it goes away when the program ends")
	}
	if !within(closeTimeout, e.teardown) {
		e.trace.stuck.Store(true)
		serviceStuck.Store(true)
		return errors.New("Windows did not finish removing the MIDI device; it goes away when the program ends")
	}
	return nil
}

// teardown gives the device up in the way [HowToRemove] says. Each stage
// is announced before it runs, like the stages of opening.
func (e *Endpoint) teardown() {
	e.trace.step("releasing the fast way to send", func() error {
		e.raw.release()
		return nil
	})
	if HowToRemove == RemoveByDisconnect && !e.session.isNil() && !e.connection.isNil() && e.connectionID != (windows.GUID{}) {
		e.trace.step("disconnecting from the device", func() error {
			if hr := e.session.call(slotSessionDisconnectEndpointConnection, guidArgs(&e.connectionID)...); hr.failed() {
				return hr
			}
			return nil
		})
	}
	e.trace.step("releasing the connection and the device", func() error {
		e.connection.release()
		e.device.release()
		return nil
	})
	if HowToRemove == RemoveByRelease {
		e.trace.step("letting go of the session", func() error {
			e.session.release()
			return nil
		})
		return
	}
	if !e.session.isNil() {
		e.trace.step("closing the MIDI session", func() error {
			closable, err := e.session.query(iidIClosable)
			if err != nil {
				return err
			}
			hr := closable.call(slotClosableClose)
			closable.release()
			e.session.release()
			if hr.failed() {
				return hr
			}
			return nil
		})
	}
}
