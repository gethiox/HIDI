package rawinput

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/gethiox/HIDI/internal/control"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	hid    = windows.NewLazySystemDLL("hid.dll")

	procRegisterClassEx         = user32.NewProc("RegisterClassExW")
	procCreateWindowEx          = user32.NewProc("CreateWindowExW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procDefWindowProc           = user32.NewProc("DefWindowProcW")
	procGetMessage              = user32.NewProc("GetMessageW")
	procDispatchMessage         = user32.NewProc("DispatchMessageW")
	procPostMessage             = user32.NewProc("PostMessageW")
	procPostQuitMessage         = user32.NewProc("PostQuitMessage")
	procRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = user32.NewProc("GetRawInputData")
	procGetRawInputDeviceList   = user32.NewProc("GetRawInputDeviceList")
	procGetRawInputDeviceInfo   = user32.NewProc("GetRawInputDeviceInfoW")
	procSetWindowsHookEx        = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx     = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx          = user32.NewProc("CallNextHookEx")
	procSendInput               = user32.NewProc("SendInput")
	procPostThreadMessage       = user32.NewProc("PostThreadMessageW")
	procGetProductString        = hid.NewProc("HidD_GetProductString")
	cfgmgr                      = windows.NewLazySystemDLL("cfgmgr32.dll")
	procLocateDevNode           = cfgmgr.NewProc("CM_Locate_DevNodeW")
	procGetDevNodeProperty      = cfgmgr.NewProc("CM_Get_DevNode_PropertyW")
	procHidGetCaps              = hid.NewProc("HidP_GetCaps")
	procHidGetValueCaps         = hid.NewProc("HidP_GetValueCaps")
	procHidGetUsages            = hid.NewProc("HidP_GetUsages")
	procHidGetData              = hid.NewProc("HidP_GetData")
	procHidMaxDataListLength    = hid.NewProc("HidP_MaxDataListLength")
	procHidGetUsageValueArray   = hid.NewProc("HidP_GetUsageValueArray")
)

// Constants from winuser.h.
const (
	wmDestroy           = 0x0002
	wmClose             = 0x0010
	wmQuit              = 0x0012
	wmInput             = 0x00FF
	wmInputDeviceChange = 0x00FE
	wmKeyUp             = 0x0101
	wmSysKeyUp          = 0x0105

	deviceArrival = 1
	deviceRemoval = 2

	ridInput          = 0x10000003
	ridiDeviceName    = 0x20000007
	ridiDeviceInfo    = 0x2000000b
	ridiPreparsedData = 0x20000005
	rimTypeKeyboard   = 1
	rimTypeHID        = 2
	ridevInputSink    = 0x00000100 // receive input without being the active window
	ridevDevNotify    = 0x00002000 // be told when devices come and go
	usagePageDesktop  = 0x01
	usageKeyboard     = 0x06
	whKeyboardLL      = 13
	llkhfInjected     = 0x10
	hwndMessage       = ^uintptr(2) // HWND_MESSAGE: a window that only receives messages
	notFoundDeviceMsg = "the device was unplugged"

	// From hidpi.h: the kind of report that carries input, and the answer
	// of a function that worked.
	hidpInput         = 0
	hidpStatusSuccess = 0x00110000
)

// Structures from winuser.h, as laid out on 64-bit Windows.
type (
	wndClassEx struct {
		size       uint32
		style      uint32
		wndProc    uintptr
		clsExtra   int32
		wndExtra   int32
		instance   windows.Handle
		icon       windows.Handle
		cursor     windows.Handle
		background windows.Handle
		menuName   *uint16
		className  *uint16
		iconSm     windows.Handle
	}
	message struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      [2]int32
		private uint32
	}
	rawInputDevice struct {
		usagePage uint16
		usage     uint16
		flags     uint32
		target    uintptr
	}
	rawInputHeader struct {
		typ    uint32
		size   uint32
		device uintptr
		wParam uintptr
	}
	rawKeyboard struct {
		makeCode         uint16
		flags            uint16
		reserved         uint16
		vKey             uint16
		message          uint32
		extraInformation uint32
	}
	rawInputDeviceList struct {
		device uintptr
		typ    uint32
	}
	// rawHID is the start of what follows the header for a device that is
	// neither keyboard nor mouse: count reports of size bytes each.
	rawHID struct {
		size  uint32
		count uint32
	}
	// ridDeviceInfo is RID_DEVICE_INFO with the fields of its HID variant.
	ridDeviceInfo struct {
		size      uint32
		typ       uint32
		vendor    uint32
		product   uint32
		version   uint32
		usagePage uint16
		usage     uint16
		_         [8]byte
	}
	// hidpCaps is HIDP_CAPS, which is made of 16-bit numbers only.
	hidpCaps struct {
		usage, usagePage  uint16
		inputReportLength uint16
		_                 [19]uint16
		linkNodes         uint16
		inputButtonCaps   uint16
		inputValueCaps    uint16
		_                 [7]uint16
	}
	// hidpValueCaps is HIDP_VALUE_CAPS.
	hidpValueCaps struct {
		usagePage      uint16
		reportID       uint8
		isAlias        uint8
		bitField       uint16
		linkCollection uint16
		linkUsage      uint16
		linkUsagePage  uint16
		isRange        uint8
		isStringRange  uint8
		isDesignRange  uint8
		isAbsolute     uint8
		hasNull        uint8
		_              uint8
		bitSize        uint16
		reportCount    uint16
		_              [5]uint16
		unitsExp       uint32
		units          uint32
		logicalMin     int32
		logicalMax     int32
		physicalMin    int32
		physicalMax    int32
		usageMin       uint16 // the usage itself when isRange is 0
		usageMax       uint16
		_              [4]uint16
		dataIndexMin   uint16 // the index itself when isRange is 0
		dataIndexMax   uint16
	}
	// hidpData is HIDP_DATA: one control of a report, named by the index
	// Windows gave it, and its value.
	hidpData struct {
		index uint16
		_     uint16
		value uint32
	}
	kbdLLHook struct {
		vkCode    uint32
		scanCode  uint32
		flags     uint32
		time      uint32
		extraInfo uintptr
	}
)

// ErrClosed and ErrDisconnected are what [Device.Err] returns.
var (
	ErrClosed       = errors.New("the device was closed")
	ErrDisconnected = errors.New(notFoundDeviceMsg)
)

// Watcher finds the keyboards and game controllers of the system, those
// present when it starts and those plugged in afterwards, and reads them.
type Watcher struct {
	opts    Options
	devices chan *Device
	hwnd    uintptr
	done    chan struct{}
	close   sync.Once

	// The fields below belong to the window's thread.
	byHandle map[uintptr]*Device // one entry for each interface
	byGroup  map[string]*Device
	readers  map[uintptr]*padReader // for the interfaces of game controllers
	hook     uintptr
	buf      []byte // what Windows reports is read into this

	// rejected holds the interfaces that were looked at and are of no
	// use, so that their input is not looked into again and again.
	rejected map[uintptr]bool

	// gate decides what the hook swallows. Its maps belong to the hook's
	// thread.
	gate gate
	// The hook runs on a thread of its own: Windows waits for the hook
	// before any program gets a key, and removes a hook that is slow to
	// answer, without a word. The window's thread asks devices for their
	// names, which can take seconds for one that is asleep.
	hookThread uint32
	hookDone   chan struct{}

	// keys guards what both threads use: claims, which decides which
	// keyboard gets a swallowed key, and the down map of every keyboard.
	keys   sync.Mutex
	claims claims[*Device]
}

// the watcher whose hook and window procedure run; Windows calls them
// without an argument to tell watchers apart, and there is only ever one.
var current atomic.Pointer[Watcher]

// Options change what a [Watcher] takes in.
type Options struct {
	// SkipXInput leaves alone the controllers Windows also offers through
	// XInput, for a caller that reads them there.
	SkipXInput bool
}

// Watch starts watching for keyboards and game controllers.
func Watch(opts Options) (*Watcher, error) {
	w := &Watcher{
		opts:     opts,
		devices:  make(chan *Device, 16),
		done:     make(chan struct{}),
		byHandle: map[uintptr]*Device{},
		byGroup:  map[string]*Device{},
		readers:  map[uintptr]*padReader{},
		rejected: map[uintptr]bool{},
		hookDone: make(chan struct{}),
	}
	if !current.CompareAndSwap(nil, w) {
		return nil, errors.New("input devices are already being watched")
	}
	ready := make(chan error, 1)
	go w.run(ready)
	if err := <-ready; err != nil {
		current.Store(nil)
		return nil, err
	}
	return w, nil
}

// Devices returns the channel new devices arrive on. Whoever receives a
// device owns it and must close it. The channel is closed when the watcher
// is closed.
func (w *Watcher) Devices() <-chan *Device { return w.devices }

// Close stops the watcher and ends every device it delivered.
func (w *Watcher) Close() error {
	w.close.Do(func() {
		procPostMessage.Call(w.hwnd, wmClose, 0, 0)
		// The window's thread ends on that message. Should it not, HIDI
		// still has to be able to stop.
		select {
		case <-w.done:
		case <-time.After(3 * time.Second):
		}
	})
	return nil
}

// run is the window's thread: it creates the window, asks for raw input,
// installs the hook and then handles messages until the window is closed.
// Windows delivers all three kinds of call on the thread that set them up.
func (w *Watcher) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)
	defer current.Store(nil)

	fail := func(what string, err error) {
		ready <- fmt.Errorf("%s: %w", what, err)
	}
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		fail("finding the program's module", err)
		return
	}
	className, _ := windows.UTF16PtrFromString("HIDI input")
	class := wndClassEx{wndProc: windows.NewCallback(windowProc), instance: instance, className: className}
	class.size = uint32(unsafe.Sizeof(class))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		fail("registering the window class", err)
		return
	}
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, uintptr(instance), 0)
	if hwnd == 0 {
		fail("creating the message window", err)
		return
	}
	w.hwnd = hwnd

	var requests []rawInputDevice
	for _, usage := range []uint16{usageKeyboard, usageJoystick, usageGamepad, usageMultiAxis} {
		requests = append(requests, rawInputDevice{usagePage: usagePageDesktop, usage: usage, flags: ridevInputSink | ridevDevNotify, target: hwnd})
	}
	if r, _, err := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&requests[0])), uintptr(len(requests)), unsafe.Sizeof(requests[0])); r == 0 {
		procDestroyWindow.Call(hwnd)
		fail("asking Windows for the input of keyboards and game controllers", err)
		return
	}
	hooked := make(chan error, 1)
	go w.runHook(instance, hooked)
	if err := <-hooked; err != nil {
		procDestroyWindow.Call(hwnd)
		fail("installing the keyboard hook", err)
		return
	}
	defer func() {
		procPostThreadMessage.Call(uintptr(w.hookThread), wmQuit, 0, 0)
		select {
		case <-w.hookDone:
		case <-time.After(time.Second):
		}
	}()

	// Asking for notifications makes Windows announce the devices that
	// are there already; listing them as well covers a Windows that does
	// not, and add ignores what it has seen.
	for _, handle := range deviceHandles() {
		w.add(handle)
	}
	ready <- nil

	var msg message
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break // WM_QUIT, or the window is gone
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	for _, d := range w.byGroup {
		d.end(ErrClosed)
	}
	close(w.devices)
}

// runHook is the hook's thread: it installs the hook and handles messages,
// which is how Windows calls the hook, until it is told to quit.
func (w *Watcher) runHook(instance windows.Handle, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.hookDone)

	hook, _, err := procSetWindowsHookEx.Call(whKeyboardLL, windows.NewCallback(keyboardHook), uintptr(instance), 0)
	if hook == 0 {
		ready <- err
		return
	}
	defer procUnhookWindowsHookEx.Call(hook)
	w.hook, w.hookThread = hook, windows.GetCurrentThreadId()
	ready <- nil

	var msg message
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return // WM_QUIT
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// Virtual key codes of the keys the hook names differently from Raw Input.
const (
	vkPause    = 0x13
	vkNumLock  = 0x90
	vkRShift   = 0xA1
	keyRShift  = 54
	keyNumLock = 69
	keyPause   = 119
)

// hookKeyCode is keyCode for what the hook is told. For three keys the
// hook's scan code and prefix differ from what Raw Input reports: right
// Shift and Num Lock come with the prefix, and Pause as Num Lock's code
// without it. Those are told by their virtual key code.
func hookKeyCode(vk uint32, scan, flags uint16) (uint16, bool) {
	switch vk {
	case vkRShift:
		return keyRShift, true
	case vkNumLock:
		return keyNumLock, true
	case vkPause:
		return keyPause, true
	}
	return keyCode(scan, flags)
}

// windowProc receives the window's messages.
func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := current.Load()
	if w != nil {
		switch uint32(msg) {
		case wmInput:
			w.input(lParam)
		case wmInputDeviceChange:
			switch wParam {
			case deviceArrival:
				w.add(lParam)
			case deviceRemoval:
				w.remove(lParam)
			}
			return 0
		case wmClose:
			procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// keyboardHook sees every key before other programs do, without knowing
// its keyboard. Returning 1 swallows the key.
func keyboardHook(code int32, wParam, lParam uintptr) uintptr {
	w := current.Load()
	if code >= 0 && w != nil {
		// lParam is the address of a structure owned by Windows.
		info := *(*kbdLLHook)(*(*unsafe.Pointer)(unsafe.Pointer(&lParam)))
		if info.flags&llkhfInjected == 0 {
			// The hook names the key by its scan code too, which gives
			// the code HIDI knows it by.
			var flags uint16
			if info.flags&llkhfExtended != 0 {
				flags = flagE0
			}
			code, known := hookKeyCode(info.vkCode, uint16(info.scanCode), flags)
			// Two keys can share a virtual key code, Enter and the Enter
			// of the keypad for one, so the gate tells keys apart by
			// their scan code where there is one.
			id := info.scanCode | uint32(flags)<<16
			if info.scanCode == 0 {
				id = info.vkCode | 1<<31
			}
			up := wParam == wmKeyUp || wParam == wmSysKeyUp
			if w.gate.swallowKey(id, code, known, up) {
				// Raw Input will not report this key: Windows drops it
				// there too. So it is taken from here.
				if known {
					w.swallowed(code, !up)
				}
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return r
}

// deviceHandles lists the interfaces of keyboards and of other HID devices
// that are present; add tells the game controllers among the latter.
func deviceHandles() []uintptr {
	var count uint32
	size := unsafe.Sizeof(rawInputDeviceList{})
	procGetRawInputDeviceList.Call(0, uintptr(unsafe.Pointer(&count)), size)
	if count == 0 {
		return nil
	}
	list := make([]rawInputDeviceList, count)
	n, _, _ := procGetRawInputDeviceList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(unsafe.Pointer(&count)), size)
	if int32(n) <= 0 {
		return nil
	}
	var handles []uintptr
	for _, entry := range list[:n] {
		if entry.typ == rimTypeKeyboard || entry.typ == rimTypeHID {
			handles = append(handles, entry.device)
		}
	}
	return handles
}

// devicePath returns the interface path of a raw input device.
func devicePath(handle uintptr) string {
	var n uint32
	procGetRawInputDeviceInfo.Call(handle, ridiDeviceName, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	r, _, _ := procGetRawInputDeviceInfo.Call(handle, ridiDeviceName, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if int32(r) <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// productName asks the device for the name it gives itself. Windows opens
// keyboards for itself alone, but answers this question for a handle that
// was opened without asking to read or write.
func productName(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var buf [127]uint16
	// The answer is one byte; the rest of the register is not defined.
	if r, _, _ := procGetProductString.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), unsafe.Sizeof(buf)); r&0xff == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

// deviceInfo asks Windows what kind of device an interface is.
func deviceInfo(handle uintptr) (ridDeviceInfo, bool) {
	var info ridDeviceInfo
	info.size = uint32(unsafe.Sizeof(info))
	n := info.size
	r, _, _ := procGetRawInputDeviceInfo.Call(handle, ridiDeviceInfo, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&n)))
	return info, int32(r) > 0
}

// devPropKey is DEVPROPKEY: a property of a device in Windows' tree of
// devices.
type devPropKey struct {
	fmtid windows.GUID
	pid   uint32
}

// containerIDKey is DEVPKEY_Device_ContainerId: the identifier that every
// function of one piece of hardware shares. A keyboard that presents its
// keys, its media keys and a few more interfaces is several devices to
// Windows, each with an instance of its own, and one container.
var containerIDKey = devPropKey{
	fmtid: windows.GUID{Data1: 0x8c7ed206, Data2: 0x3f8a, Data3: 0x4827, Data4: [8]byte{0xb3, 0xab, 0xae, 0x9e, 0x1f, 0xae, 0xfc, 0x6c}},
	pid:   2,
}

// noContainer is what Windows reports for hardware that is part of the
// machine and has no container of its own.
var noContainer = windows.GUID{Data4: [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}

// container returns the identifier of the piece of hardware an interface
// belongs to, or an empty string where Windows does not tell.
func container(path string) string {
	id := instanceID(path)
	if id == "" {
		return ""
	}
	name, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return ""
	}
	var node uint32
	if r, _, _ := procLocateDevNode.Call(uintptr(unsafe.Pointer(&node)), uintptr(unsafe.Pointer(name)), 0); r != 0 {
		return ""
	}
	var (
		kind  uint32
		guid  windows.GUID
		size  = uint32(unsafe.Sizeof(guid))
		empty windows.GUID
	)
	r, _, _ := procGetDevNodeProperty.Call(uintptr(node), uintptr(unsafe.Pointer(&containerIDKey)),
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(&size)), 0)
	if r != 0 || guid == empty || guid == noContainer {
		return ""
	}
	return guid.String()
}

// add takes in an interface of a keyboard or a game controller. The
// interfaces of one piece of hardware become one device, which is
// delivered when its first interface appears.
func (w *Watcher) add(handle uintptr) {
	if handle == 0 || w.byHandle[handle] != nil || w.rejected[handle] {
		return
	}
	if !w.take(handle) {
		w.rejected[handle] = true
	}
}

// take is add for an interface that was not looked at yet. It reports
// false for one that is of no use.
func (w *Watcher) take(handle uintptr) bool {
	info, ok := deviceInfo(handle)
	if !ok {
		return false
	}
	var controller bool
	switch {
	case info.typ == rimTypeKeyboard:
	case info.typ == rimTypeHID && isGameController(info.usagePage, info.usage):
		controller = true
	default:
		return false
	}
	path := devicePath(handle)
	if path == "" {
		return false
	}
	id := parsePath(path)
	// The interfaces of one piece of hardware share its container; where
	// Windows names none, what the path says has to do.
	if c := container(path); c != "" {
		id.group = c
	}
	kind, typ, group := "Keyboard", control.DeviceKeyboard, id.group
	if controller {
		if id.xinput && w.opts.SkipXInput {
			return false
		}
		if id.vendor == 0 {
			// The path of a Bluetooth controller is written differently.
			id.vendor, id.product = uint16(info.vendor), uint16(info.product)
		}
		// A keyboard that also presents a game controller is two devices,
		// as it is on Linux.
		kind, typ, group = "Game controller", control.DeviceGamepad, "PAD:"+id.group
		reader, err := newPadReader(handle, layoutOf(id.vendor, id.product, id.xinput), info.usage == usageJoystick)
		if err != nil {
			return false
		}
		w.readers[handle] = reader
	}
	if d := w.byGroup[group]; d != nil {
		w.byHandle[handle] = d
		d.handles++
		return true
	}
	// Some devices pad the name they give themselves with spaces.
	name := strings.TrimSpace(productName(path))
	if name == "" {
		name = kind
		if id.vendor != 0 {
			name = fmt.Sprintf("%s %04x:%04x", kind, id.vendor, id.product)
		}
	}
	d := &Device{
		watcher:    w,
		group:      group,
		handles:    1,
		controller: controller,
		queue:      newQueue(),
		down:       map[uint16]bool{},
		info: control.DeviceInfo{
			Hardware: control.HardwareID{Vendor: id.vendor, Product: id.product},
			Name:     name,
			Location: id.group,
			Type:     typ,
		},
	}
	w.byHandle[handle] = d
	w.byGroup[group] = d
	if !controller {
		w.keys.Lock()
		w.claims.add(d)
		w.keys.Unlock()
	}
	select {
	case w.devices <- d:
	default:
		// Nobody takes devices; this one cannot be used.
		w.forget(d)
		d.end(ErrClosed)
	}
	return true
}

// remove handles an interface that went away. The device ends with its
// last interface.
func (w *Watcher) remove(handle uintptr) {
	delete(w.rejected, handle)
	d := w.byHandle[handle]
	if d == nil {
		return
	}
	delete(w.byHandle, handle)
	delete(w.readers, handle)
	if d.handles--; d.handles > 0 {
		return
	}
	w.forget(d)
	d.end(ErrDisconnected)
}

func (w *Watcher) forget(d *Device) {
	delete(w.byGroup, d.group)
	w.keys.Lock()
	w.claims.remove(d)
	w.keys.Unlock()
	for handle, other := range w.byHandle {
		if other == d {
			delete(w.byHandle, handle)
			delete(w.readers, handle)
		}
	}
}

// input handles one message from Raw Input: a key, or reports of a game
// controller.
func (w *Watcher) input(lParam uintptr) {
	headerSize := unsafe.Sizeof(rawInputHeader{})
	var size uint32
	procGetRawInputData.Call(lParam, ridInput, 0, uintptr(unsafe.Pointer(&size)), headerSize)
	if uintptr(size) < headerSize {
		return
	}
	if int(size) > len(w.buf) {
		w.buf = make([]byte, size)
	}
	r, _, _ := procGetRawInputData.Call(lParam, ridInput, uintptr(unsafe.Pointer(&w.buf[0])), uintptr(unsafe.Pointer(&size)), headerSize)
	if int32(r) <= 0 || uintptr(r) < headerSize {
		return
	}
	data := w.buf[:r]
	header := (*rawInputHeader)(unsafe.Pointer(&data[0]))
	if header.typ != rimTypeKeyboard && header.typ != rimTypeHID {
		return
	}
	d := w.byHandle[header.device]
	if d == nil {
		// Input from a device that was not announced: take it in now.
		// Keys that programs type have no device and stay unknown.
		w.add(header.device)
		if d = w.byHandle[header.device]; d == nil {
			return
		}
	}
	body := data[headerSize:]
	switch header.typ {
	case rimTypeKeyboard:
		if uintptr(len(body)) >= unsafe.Sizeof(rawKeyboard{}) {
			w.key(d, (*rawKeyboard)(unsafe.Pointer(&body[0])))
		}
	case rimTypeHID:
		reader := w.readers[header.device]
		if reader == nil || uintptr(len(body)) < unsafe.Sizeof(rawHID{}) {
			return
		}
		hid := (*rawHID)(unsafe.Pointer(&body[0]))
		reports := body[unsafe.Sizeof(rawHID{}):]
		now := time.Now()
		for i := uint32(0); i < hid.count && hid.size > 0; i++ {
			if uint64(len(reports)) < uint64(hid.size) {
				break
			}
			for _, ev := range reader.read(reports[:hid.size], now) {
				d.queue.put(ev)
			}
			reports = reports[hid.size:]
		}
	}
}

// key handles one key event.
func (w *Watcher) key(d *Device, raw *rawKeyboard) {
	// Windows marks with this virtual key code what is no key by itself:
	// the second half of Pause, which would read as Num Lock, and the
	// shifts a keyboard puts around some keys.
	if raw.vKey == 0xFF {
		return
	}
	code, ok := keyCode(raw.makeCode, raw.flags)
	if !ok {
		return
	}
	w.keys.Lock()
	defer w.keys.Unlock()
	w.claims.saw(d)
	w.put(d, code, raw.flags&flagBreak == 0)
}

// swallowed handles a key the hook kept from other programs. The hook does
// not tell the keyboard; see [claims].
func (w *Watcher) swallowed(code uint16, pressed bool) {
	w.keys.Lock()
	defer w.keys.Unlock()
	d, ok := w.claims.owner(code, pressed, func(d *Device) bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.grabbed || d.passing
	})
	if ok {
		w.put(d, code, pressed)
	}
}

// put delivers a key event of a keyboard. The caller holds w.keys.
func (w *Watcher) put(d *Device, code uint16, pressed bool) {
	if d.down[code] == pressed {
		return // a held key repeating, or a release without a press
	}
	if pressed {
		d.down[code] = true
	} else {
		delete(d.down, code)
	}
	value := 0.0
	if pressed {
		value = 1
	}
	d.queue.put(control.Event{Code: control.Key(code), Value: value, Time: time.Now()})
}

// padReader reads the reports of one interface of a game controller. What
// a report means is described by data Windows prepared from the
// controller's own description; functions of hid.dll read a report with
// it.
type padReader struct {
	preparsed []byte
	pad       *pad
	usages    []uint16
	// byIndex tells the axis of the pad, by the index Windows gave the
	// value. A controller may have several values of one usage, four hats
	// for instance, and only the index tells them apart.
	byIndex map[uint16]int
	items   []hidpData
	// arrays holds the values a controller declares as several of one
	// usage in a row, which Windows reports in one piece.
	arrays []valueArray
}

// valueArray is count values of one usage, the axes from first on.
type valueArray struct {
	page, usage uint16
	bits, count uint16
	first       int
	buf         []byte
}

func newPadReader(handle uintptr, l layout, joystick bool) (*padReader, error) {
	var n uint32
	procGetRawInputDeviceInfo.Call(handle, ridiPreparsedData, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return nil, errors.New("the controller has no description")
	}
	r := &padReader{preparsed: make([]byte, n), pad: newPad(l, joystick), usages: make([]uint16, 256), byIndex: map[uint16]int{}}
	if got, _, _ := procGetRawInputDeviceInfo.Call(handle, ridiPreparsedData, r.data(), uintptr(unsafe.Pointer(&n))); int32(got) <= 0 {
		return nil, errors.New("the controller's description could not be read")
	}
	var caps hidpCaps
	if status, _, _ := procHidGetCaps.Call(r.data(), uintptr(unsafe.Pointer(&caps))); uint32(status) != hidpStatusSuccess {
		return nil, errors.New("the controller's description was not understood")
	}
	if caps.inputValueCaps > 0 {
		values := make([]hidpValueCaps, caps.inputValueCaps)
		count := caps.inputValueCaps
		status, _, _ := procHidGetValueCaps.Call(hidpInput, uintptr(unsafe.Pointer(&values[0])), uintptr(unsafe.Pointer(&count)), r.data())
		if uint32(status) != hidpStatusSuccess {
			return nil, errors.New("the controller's axes could not be listed")
		}
		values = values[:min(int(count), len(values))]
		// Windows lists the values backwards. In the order of their
		// indexes they are as the controller declares them, which is the
		// order Linux numbers a second slider or hat in.
		slices.SortStableFunc(values, func(a, b hidpValueCaps) int { return int(a.dataIndexMin) - int(b.dataIndexMin) })
		for _, v := range values {
			if v.isRange == 0 && v.reportCount > 1 {
				array := valueArray{page: v.usagePage, usage: v.usageMin, bits: v.bitSize, count: v.reportCount, first: len(r.pad.axes)}
				for range v.reportCount {
					if !r.pad.addAxis(v.usagePage, v.usageMin, v.bitSize, v.logicalMin, v.logicalMax) {
						array.count = uint16(len(r.pad.axes) - array.first)
						break
					}
				}
				if array.count > 0 {
					array.buf = make([]byte, (int(v.bitSize)*int(v.reportCount)+7)/8)
					r.arrays = append(r.arrays, array)
				}
				continue
			}
			last := v.usageMin
			if v.isRange != 0 {
				last = v.usageMax
			}
			for usage := v.usageMin; usage >= v.usageMin && usage <= last; usage++ {
				if r.pad.addAxis(v.usagePage, usage, v.bitSize, v.logicalMin, v.logicalMax) {
					r.byIndex[v.dataIndexMin+(usage-v.usageMin)] = len(r.pad.axes) - 1
				}
				if usage == 0xffff {
					break
				}
			}
		}
	}
	if n, _, _ := procHidMaxDataListLength.Call(hidpInput, r.data()); n > 0 {
		r.items = make([]hidpData, n)
	}
	return r, nil
}

func (r *padReader) data() uintptr { return uintptr(unsafe.Pointer(&r.preparsed[0])) }

// read returns the events of one report. A controller may send several
// kinds of report; a question about a control that is not in this one is
// answered with a failure, and that control keeps its state.
func (r *padReader) read(report []byte, at time.Time) []control.Event {
	if len(report) == 0 {
		return nil
	}
	data, size := uintptr(unsafe.Pointer(&report[0])), uintptr(len(report))
	var events []control.Event

	count := uint32(len(r.usages))
	status, _, _ := procHidGetUsages.Call(hidpInput, pageButton, 0, uintptr(unsafe.Pointer(&r.usages[0])), uintptr(unsafe.Pointer(&count)), r.data(), data, size)
	if uint32(status) == hidpStatusSuccess {
		events = r.pad.buttons(r.usages[:min(int(count), len(r.usages))], at)
	}
	for _, a := range r.arrays {
		status, _, _ := procHidGetUsageValueArray.Call(hidpInput, uintptr(a.page), 0, uintptr(a.usage), uintptr(unsafe.Pointer(&a.buf[0])), uintptr(len(a.buf)), r.data(), data, size)
		if uint32(status) != hidpStatusSuccess {
			continue
		}
		for i := range int(a.count) {
			events = append(events, r.pad.value(a.first+i, bitField(a.buf, i*int(a.bits), int(a.bits)), at)...)
		}
	}
	if len(r.items) == 0 {
		return events
	}
	count = uint32(len(r.items))
	status, _, _ = procHidGetData.Call(hidpInput, uintptr(unsafe.Pointer(&r.items[0])), uintptr(unsafe.Pointer(&count)), r.data(), data, size)
	if uint32(status) != hidpStatusSuccess {
		return events
	}
	for _, item := range r.items[:min(int(count), len(r.items))] {
		if i, ok := r.byIndex[item.index]; ok {
			events = append(events, r.pad.value(i, item.value, at)...)
		}
	}
	return events
}

// Device is one keyboard or game controller. It implements input.Device.
type Device struct {
	watcher *Watcher
	info    control.DeviceInfo
	group   string
	queue   *queue
	// controller is set for a game controller.
	controller bool

	// handles belongs to the window's thread; down is guarded by the
	// watcher's keys.
	handles int
	down    map[uint16]bool

	mu      sync.Mutex
	err     error
	grabbed bool
	passing bool
	sharing bool // some keys are let through the grab
}

// llkhfExtended marks a key with the E0 prefix in what the hook is told.
const llkhfExtended = 0x01

// keyInput is INPUT with a KEYBDINPUT in it, as SendInput takes it on a
// 64-bit Windows: 40 bytes.
type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

const (
	inputKeyboard    = 1
	keyEventExtended = 0x0001
	keyEventKeyUp    = 0x0002
	keyEventScanCode = 0x0008
	sizeofKeyInput   = 40
	_                = uint(sizeofKeyInput - unsafe.Sizeof(keyInput{})) // both differences must
	_                = uint(unsafe.Sizeof(keyInput{}) - sizeofKeyInput) // be zero
)

// Pass lets the keys of every keyboard through to other programs except
// for the hidden ones. Like a grab, hiding cannot tell keyboards apart on
// Windows: the keys are hidden whichever keyboard they are pressed on.
func (d *Device) Pass(hidden []control.Code) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	if d.controller {
		return errors.New("a game controller has no keys to hide")
	}
	set := make(map[uint16]bool, len(hidden))
	for _, code := range hidden {
		if code.Kind == control.KindKey {
			set[code.Num] = true
		}
	}
	d.watcher.gate.hidden.Store(&set)
	if !d.passing {
		d.passing = true
		d.watcher.gate.hides.Add(1)
	}
	if d.grabbed {
		d.grabbed = false
		d.watcher.gate.grabs.Add(-1)
	}
	d.unshare()
	return nil
}

// Share keeps the keys from other programs like [Device.Grab], except for
// the open ones. As with a grab, Windows cannot tell keyboards apart here:
// the open keys are let through whichever keyboard they are pressed on,
// and the last keyboard to ask decides which they are.
func (d *Device) Share(open []control.Code) error {
	if err := d.Grab(); err != nil {
		return err
	}
	if d.controller {
		return nil
	}
	set := make(map[uint16]bool, len(open))
	for _, code := range open {
		if code.Kind == control.KindKey {
			set[code.Num] = true
		}
	}
	d.mu.Lock()
	d.sharing = true
	d.mu.Unlock()
	d.watcher.gate.open.Store(&set)
	return nil
}

// unshare ends what Share added to a grab. The caller holds d.mu.
func (d *Device) unshare() {
	if d.sharing {
		d.sharing = false
		d.watcher.gate.open.Store(nil)
	}
}

// Type presses or releases a key for the other programs. What HIDI types
// itself is never swallowed, and does not come back to it as input.
func (d *Device) Type(key control.Code, down bool) error {
	scan, e0, ok := scanCode(key.Num)
	if key.Kind != control.KindKey || !ok {
		return fmt.Errorf("%v cannot be typed", key)
	}
	in := keyInput{typ: inputKeyboard, scan: scan, flags: keyEventScanCode}
	if e0 {
		in.flags |= keyEventExtended
	}
	if !down {
		in.flags |= keyEventKeyUp
	}
	if n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in)); n != 1 {
		return fmt.Errorf("typing %v: %w", key, err)
	}
	return nil
}

// Info describes the device.
func (d *Device) Info() control.DeviceInfo { return d.info }

// Events returns the channel the device's input arrives on.
func (d *Device) Events() <-chan control.Event { return d.queue.out }

// Err returns why Events was closed, or nil while it is open.
func (d *Device) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// Grab keeps the keys from other programs. Windows cannot do that for one
// keyboard alone, so while any keyboard is grabbed, none of them types. A
// key that is down when the grab begins is still released for the other
// programs.
//
// A game controller cannot be kept from other programs on Windows; for
// one, Grab does nothing.
func (d *Device) Grab() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	if !d.grabbed && !d.controller {
		d.grabbed = true
		d.watcher.gate.grabs.Add(1)
	}
	d.unpass()
	d.unshare()
	return nil
}

func (d *Device) unpass() {
	if d.passing {
		d.passing = false
		d.watcher.gate.hides.Add(-1)
	}
}

// Release ends the grab.
func (d *Device) Release() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ungrab()
	return nil
}

func (d *Device) ungrab() {
	d.unpass()
	d.unshare()
	if d.grabbed {
		d.grabbed = false
		d.watcher.gate.grabs.Add(-1)
	}
}

// end stops the device for the given reason, once.
func (d *Device) end(reason error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return
	}
	d.err = reason
	d.ungrab()
	d.queue.close()
}

// Close stops delivering the device's input and ends a grab. The device
// is delivered again if it is unplugged and plugged back in.
func (d *Device) Close() error {
	d.end(ErrClosed)
	return nil
}
