package rawinput

import (
	"errors"
	"fmt"
	"runtime"
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
	procGetProductString        = hid.NewProc("HidD_GetProductString")
)

// Constants from winuser.h.
const (
	wmDestroy           = 0x0002
	wmClose             = 0x0010
	wmInput             = 0x00FF
	wmInputDeviceChange = 0x00FE
	wmKeyUp             = 0x0101
	wmSysKeyUp          = 0x0105

	deviceArrival = 1
	deviceRemoval = 2

	ridInput          = 0x10000003
	ridiDeviceName    = 0x20000007
	rimTypeKeyboard   = 1
	ridevInputSink    = 0x00000100 // receive input without being the active window
	ridevDevNotify    = 0x00002000 // be told when devices come and go
	usagePageDesktop  = 0x01
	usageKeyboard     = 0x06
	whKeyboardLL      = 13
	llkhfInjected     = 0x10
	hwndMessage       = ^uintptr(2) // HWND_MESSAGE: a window that only receives messages
	notFoundDeviceMsg = "the keyboard was unplugged"
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
	rawInputKeyboard struct {
		header   rawInputHeader
		keyboard rawKeyboard
	}
	rawInputDeviceList struct {
		device uintptr
		typ    uint32
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

// Watcher finds the keyboards of the system, those present when it starts
// and those plugged in afterwards, and reads them.
type Watcher struct {
	devices chan *Device
	hwnd    uintptr
	done    chan struct{}
	close   sync.Once

	// The fields below belong to the window's thread.
	byHandle map[uintptr]*Device // one entry for each interface
	byGroup  map[string]*Device
	hook     uintptr

	// gate decides what the hook swallows.
	gate gate
}

// the watcher whose hook and window procedure run; Windows calls them
// without an argument to tell watchers apart, and there is only ever one.
var current atomic.Pointer[Watcher]

// Watch starts watching for keyboards.
func Watch() (*Watcher, error) {
	w := &Watcher{
		devices:  make(chan *Device, 16),
		done:     make(chan struct{}),
		byHandle: map[uintptr]*Device{},
		byGroup:  map[string]*Device{},
		gate:     gate{passed: map[uint32]bool{}},
	}
	if !current.CompareAndSwap(nil, w) {
		return nil, errors.New("keyboards are already being watched")
	}
	ready := make(chan error, 1)
	go w.run(ready)
	if err := <-ready; err != nil {
		current.Store(nil)
		return nil, err
	}
	return w, nil
}

// Devices returns the channel new keyboards arrive on. Whoever receives a
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

	request := rawInputDevice{usagePage: usagePageDesktop, usage: usageKeyboard, flags: ridevInputSink | ridevDevNotify, target: hwnd}
	if r, _, err := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&request)), 1, unsafe.Sizeof(request)); r == 0 {
		procDestroyWindow.Call(hwnd)
		fail("asking Windows for keyboard input", err)
		return
	}
	w.hook, _, err = procSetWindowsHookEx.Call(whKeyboardLL, windows.NewCallback(keyboardHook), uintptr(instance), 0)
	if w.hook == 0 {
		procDestroyWindow.Call(hwnd)
		fail("installing the keyboard hook", err)
		return
	}
	defer procUnhookWindowsHookEx.Call(w.hook)

	// Asking for notifications makes Windows announce the keyboards that
	// are there already; listing them as well covers a Windows that does
	// not, and add ignores what it has seen.
	for _, handle := range keyboardHandles() {
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
		if info.flags&llkhfInjected == 0 && w.gate.swallow(info.vkCode, wParam == wmKeyUp || wParam == wmSysKeyUp) {
			return 1
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return r
}

// keyboardHandles lists the keyboard interfaces that are present.
func keyboardHandles() []uintptr {
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
		if entry.typ == rimTypeKeyboard {
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
	if r, _, _ := procGetProductString.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), unsafe.Sizeof(buf)); r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

// add takes in a keyboard interface. The interfaces of one keyboard become
// one device, which is delivered when its first interface appears.
func (w *Watcher) add(handle uintptr) {
	if handle == 0 || w.byHandle[handle] != nil {
		return
	}
	path := devicePath(handle)
	if path == "" {
		return
	}
	id := parsePath(path)
	if d := w.byGroup[id.group]; d != nil {
		w.byHandle[handle] = d
		d.handles++
		return
	}
	name := productName(path)
	if name == "" {
		name = "Keyboard"
		if id.vendor != 0 {
			name = fmt.Sprintf("Keyboard %04x:%04x", id.vendor, id.product)
		}
	}
	d := &Device{
		watcher: w,
		group:   id.group,
		handles: 1,
		queue:   newQueue(),
		down:    map[uint16]bool{},
		info: control.DeviceInfo{
			Hardware: control.HardwareID{Vendor: id.vendor, Product: id.product},
			Name:     name,
			Location: id.group,
			Type:     control.DeviceKeyboard,
		},
	}
	w.byHandle[handle] = d
	w.byGroup[id.group] = d
	select {
	case w.devices <- d:
	default:
		// Nobody takes devices; this one cannot be used.
		w.forget(d)
		d.end(ErrClosed)
	}
}

// remove handles an interface that went away. The device ends with its
// last interface.
func (w *Watcher) remove(handle uintptr) {
	d := w.byHandle[handle]
	if d == nil {
		return
	}
	delete(w.byHandle, handle)
	if d.handles--; d.handles > 0 {
		return
	}
	w.forget(d)
	d.end(ErrDisconnected)
}

func (w *Watcher) forget(d *Device) {
	delete(w.byGroup, d.group)
	for handle, other := range w.byHandle {
		if other == d {
			delete(w.byHandle, handle)
		}
	}
}

// input handles one key event from Raw Input.
func (w *Watcher) input(lParam uintptr) {
	var raw rawInputKeyboard
	size := uint32(unsafe.Sizeof(raw))
	r, _, _ := procGetRawInputData.Call(lParam, ridInput, uintptr(unsafe.Pointer(&raw)), uintptr(unsafe.Pointer(&size)), unsafe.Sizeof(raw.header))
	if int32(r) <= 0 || raw.header.typ != rimTypeKeyboard {
		return
	}
	d := w.byHandle[raw.header.device]
	if d == nil {
		// A key from a keyboard that was not announced: take it in now.
		// Keys that programs type have no device and stay unknown.
		w.add(raw.header.device)
		if d = w.byHandle[raw.header.device]; d == nil {
			return
		}
	}
	code, ok := keyCode(raw.keyboard.makeCode, raw.keyboard.flags)
	if !ok {
		return
	}
	pressed := raw.keyboard.flags&flagBreak == 0
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

// Device is one keyboard. It implements input.Device.
type Device struct {
	watcher *Watcher
	info    control.DeviceInfo
	group   string
	queue   *queue

	// handles and down belong to the window's thread.
	handles int
	down    map[uint16]bool

	mu      sync.Mutex
	err     error
	grabbed bool
}

// Info describes the keyboard.
func (d *Device) Info() control.DeviceInfo { return d.info }

// Events returns the channel the keyboard's keys arrive on.
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
func (d *Device) Grab() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	if !d.grabbed {
		d.grabbed = true
		d.watcher.gate.grabs.Add(1)
	}
	return nil
}

// Release ends the grab.
func (d *Device) Release() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ungrab()
	return nil
}

func (d *Device) ungrab() {
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

// Close stops delivering the keyboard's keys and ends a grab. The keyboard
// is delivered again if it is unplugged and plugged back in.
func (d *Device) Close() error {
	d.end(ErrClosed)
	return nil
}
