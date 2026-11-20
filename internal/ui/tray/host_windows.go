package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procGetMessage            = user32.NewProc("GetMessageW")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procPostMessage           = user32.NewProc("PostMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreateIcon            = user32.NewProc("CreateIcon")
	procDestroyIcon           = user32.NewProc("DestroyIcon")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu            = user32.NewProc("AppendMenuW")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
)

// Constants from winuser.h and shellapi.h.
const (
	wmDestroy   = 0x0002
	wmClose     = 0x0010
	wmNull      = 0x0000
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmMButtonUp = 0x0208
	wmApp       = 0x8000
	wmTray      = wmApp + 1 // what the icon reports with
	wmShow      = wmApp + 2 // a new view is waiting

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	smCxSmIcon = 49 // the width of a small icon, which is what the area shows

	cmdToggle = 1
	cmdQuit   = 2
	cmdOpen   = 3
)

// Structures from the Windows headers, as laid out on 64-bit Windows.
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
	// notifyIconData is NOTIFYICONDATAW.
	notifyIconData struct {
		size            uint32
		hwnd            uintptr
		id              uint32
		flags           uint32
		callbackMessage uint32
		icon            uintptr
		tip             [128]uint16
		state           uint32
		stateMask       uint32
		info            [256]uint16
		version         uint32
		infoTitle       [64]uint16
		infoFlags       uint32
		guid            windows.GUID
		balloonIcon     uintptr
	}
)

type windowsHost struct {
	actions chan Action
	hwnd    uintptr
	done    chan struct{}
	close   sync.Once

	mu   sync.Mutex
	view View

	// The fields below belong to the window's thread.
	added          bool
	icon           uintptr
	shown          View
	taskbarCreated uint32
}

// the host whose window procedure runs; Windows calls it without an
// argument to tell hosts apart, and a program has one icon.
var current *windowsHost
var currentMu sync.Mutex

// NewHost puts an icon into the notification area. Should the area not be
// there, the icon appears when it is.
func NewHost() (Host, error) {
	h := &windowsHost{actions: make(chan Action, 4), done: make(chan struct{}), view: ViewOf(nil)}
	currentMu.Lock()
	if current != nil {
		currentMu.Unlock()
		return nil, errors.New("the program has an icon already")
	}
	current = h
	currentMu.Unlock()

	ready := make(chan error, 1)
	go h.run(ready)
	if err := <-ready; err != nil {
		currentMu.Lock()
		current = nil
		currentMu.Unlock()
		return nil, err
	}
	return h, nil
}

func (h *windowsHost) Show(v View) error {
	h.mu.Lock()
	h.view = v
	h.mu.Unlock()
	procPostMessage.Call(h.hwnd, wmShow, 0, 0)
	return nil
}

func (h *windowsHost) Actions() <-chan Action { return h.actions }

func (h *windowsHost) Close() error {
	h.close.Do(func() {
		procPostMessage.Call(h.hwnd, wmClose, 0, 0)
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
		}
	})
	return nil
}

func (h *windowsHost) act(a Action) {
	select {
	case h.actions <- a:
	default:
	}
}

// run is the window's thread. An icon in the notification area belongs to
// a window, which receives the clicks; the window is never shown.
func (h *windowsHost) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)
	defer func() {
		currentMu.Lock()
		current = nil
		currentMu.Unlock()
	}()

	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		ready <- fmt.Errorf("finding the program's module: %w", err)
		return
	}
	className, _ := windows.UTF16PtrFromString("HIDI tray")
	class := wndClassEx{wndProc: windows.NewCallback(trayWindowProc), instance: instance, className: className}
	class.size = uint32(unsafe.Sizeof(class))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		ready <- fmt.Errorf("registering the window class: %w", err)
		return
	}
	// Windows announces a notification area that started again, after a
	// crash of the shell for one, with this message.
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(name)))
	h.taskbarCreated = uint32(r)

	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, uintptr(instance), 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("creating the window of the icon: %w", err)
		return
	}
	h.hwnd = hwnd
	h.update()
	ready <- nil

	var msg message
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if h.icon != 0 {
		procDestroyIcon.Call(h.icon)
	}
}

func trayWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	currentMu.Lock()
	h := current
	currentMu.Unlock()
	if h != nil {
		switch uint32(msg) {
		case wmTray:
			switch uint32(lParam) {
			case wmLButtonUp:
				h.act(h.shown.click())
			case wmRButtonUp:
				h.menu()
			case wmMButtonUp:
				// The middle button turns HIDI off and on, whatever a
				// plain click does.
				h.act(ActionToggle)
			}
			return 0
		case wmShow:
			h.update()
			return 0
		case h.taskbarCreated:
			h.added = false
			h.update()
			return 0
		case wmClose:
			if h.added {
				data := h.data()
				procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
			}
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

func (h *windowsHost) data() notifyIconData {
	data := notifyIconData{hwnd: h.hwnd, id: 1}
	data.size = uint32(unsafe.Sizeof(data))
	return data
}

// update makes the icon look like the newest view. Without a notification
// area the icon cannot be added; it is added when the area announces
// itself.
func (h *windowsHost) update() {
	h.mu.Lock()
	v := h.view
	h.mu.Unlock()

	if h.icon == 0 || v.Look != h.shown.Look {
		size, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
		if size == 0 {
			size = 16
		}
		if icon := newIcon(v.Look, int(size)); icon != 0 {
			if h.icon != 0 {
				defer procDestroyIcon.Call(h.icon)
			}
			h.icon = icon
		}
	}
	// The menu is made from the view when it is asked for. Windows is
	// told only of what the icon itself shows: a view arrives with every
	// note that is played.
	unchanged := h.added && v.Look == h.shown.Look && v.Title == h.shown.Title
	h.shown = v
	if unchanged {
		return
	}

	data := h.data()
	data.flags = nifMessage | nifIcon | nifTip
	data.callbackMessage = wmTray
	data.icon = h.icon
	tip, _ := windows.UTF16FromString(v.Title)
	copy(data.tip[:len(data.tip)-1], tip)
	action := uintptr(nimModify)
	if !h.added {
		action = nimAdd
	}
	if r, _, _ := procShellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&data))); r != 0 {
		h.added = true
	}
}

// newIcon makes a Windows icon of the drawn one.
func newIcon(look Look, size int) uintptr {
	img := Icon(look, size)
	// The colours go blue, green, red, alpha. The mask that icons also
	// have is not used when the colours carry alpha.
	colour := make([]byte, size*size*4)
	for i := 0; i < len(img.Pix); i += 4 {
		colour[i], colour[i+1], colour[i+2], colour[i+3] = img.Pix[i+2], img.Pix[i+1], img.Pix[i], img.Pix[i+3]
	}
	mask := make([]byte, ((size+15)/16*2)*size)
	var instance windows.Handle
	windows.GetModuleHandleEx(0, nil, &instance)
	icon, _, _ := procCreateIcon.Call(uintptr(instance), uintptr(size), uintptr(size), 1, 32,
		uintptr(unsafe.Pointer(&mask[0])), uintptr(unsafe.Pointer(&colour[0])))
	return icon
}

// menu shows the menu at the pointer and carries out what is chosen.
func (h *windowsHost) menu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	add := func(flags, id uintptr, label string) {
		text, _ := windows.UTF16PtrFromString(label)
		procAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
	}
	for _, line := range h.shown.Lines {
		add(mfString|mfGrayed, 0, line)
	}
	if h.shown.Open != "" || h.shown.Toggle != "" {
		add(mfSeparator, 0, "")
	}
	if h.shown.Open != "" {
		add(mfString, cmdOpen, h.shown.Open)
	}
	if h.shown.Toggle != "" {
		add(mfString, cmdToggle, h.shown.Toggle)
	}
	add(mfSeparator, 0, "")
	add(mfString, cmdQuit, h.shown.quit())

	var pt [2]int32
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Windows closes the menu on a click elsewhere only if the window is
	// in front, and wants a message afterwards.
	procSetForegroundWindow.Call(h.hwnd)
	chosen, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmNoNotify|tpmReturnCmd, uintptr(pt[0]), uintptr(pt[1]), 0, h.hwnd, 0)
	procPostMessage.Call(h.hwnd, wmNull, 0, 0)
	switch chosen {
	case cmdToggle:
		h.act(ActionToggle)
	case cmdOpen:
		h.act(ActionOpen)
	case cmdQuit:
		h.act(ActionQuit)
	}
}
