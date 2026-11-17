package winmidi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	combase                       = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize              = combase.NewProc("RoInitialize")
	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
)

// hresult is a COM result code. Negative values are failures.
type hresult int32

func (hr hresult) failed() bool { return hr < 0 }

func (hr hresult) Error() string {
	// Most codes have a system message; the number is what one searches for.
	msg := syscall.Errno(uint32(hr)).Error()
	return fmt.Sprintf("%s (0x%08X)", msg, uint32(hr))
}

const (
	rpcEChangedMode   = hresult(-2147417850) // 0x80010106
	regdbEClassNotReg = hresult(-2147221164) // 0x80040154
)

// Interface identifiers every COM object has.
var (
	iidIActivationFactory = guid("00000035-0000-0000-C000-000000000046")
	iidIClosable          = guid("30D5A829-7FA4-4026-83BB-D75BAE4EA99E")
)

func guid(s string) windows.GUID {
	g, err := windows.GUIDFromString("{" + s + "}")
	if err != nil {
		panic("winmidi: bad interface identifier " + s)
	}
	return g
}

// object is a pointer to a COM interface: a pointer to a pointer to the
// table of the interface's functions.
type object struct {
	ptr unsafe.Pointer
}

func (o object) isNil() bool { return o.ptr == nil }

// call invokes the method in the given slot of the interface's table. Slots
// 0 to 2 are IUnknown's, 3 to 5 IInspectable's; an interface's own methods
// start at 6, or at 3 for an interface that is not a WinRT one.
//
// Arguments are often the addresses of variables of the caller, turned
// into numbers. The line below makes the compiler keep those variables in
// place for the length of the call, as it does for the calls of the
// syscall package; without it a stack that grows in between would leave
// Windows writing where the variable was.
//
//go:uintptrescapes
func (o object) call(slot int, args ...uintptr) hresult {
	vtable := *(*unsafe.Pointer)(o.ptr)
	fn := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{uintptr(o.ptr)}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return hresult(r)
}

// query asks the object for another of its interfaces.
func (o object) query(iid windows.GUID) (object, error) {
	var out object
	if hr := o.call(0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out.ptr))); hr.failed() {
		return object{}, hr
	}
	return out, nil
}

// release gives up a reference to the object.
func (o object) release() {
	if o.ptr != nil {
		o.call(2)
	}
}

// guidArgs returns a GUID as the arguments of a method that takes it by
// value. 64-bit Windows passes a 16-byte value as a pointer to a copy on
// x64, and in two registers on ARM64.
func guidArgs(g *windows.GUID) []uintptr {
	if runtime.GOARCH == "arm64" {
		words := (*[2]uintptr)(unsafe.Pointer(g))
		return []uintptr{words[0], words[1]}
	}
	return []uintptr{uintptr(unsafe.Pointer(g))}
}

// hstring is a WinRT string handle.
type hstring uintptr

func newHString(s string) (hstring, error) {
	utf16, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	n := len(utf16) - 1 // without the terminating zero
	var data *uint16
	if n > 0 {
		data = &utf16[0]
	}
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(data)), uintptr(n), uintptr(unsafe.Pointer(&h)))
	if hr := hresult(r); hr.failed() {
		return 0, hr
	}
	return h, nil
}

func (h hstring) free() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}

func (h hstring) String() string {
	if h == 0 {
		return ""
	}
	var n uint32
	r, _, _ := procWindowsGetStringRawBuffer.Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
	if r == 0 || n == 0 {
		return ""
	}
	// The buffer belongs to the string; r is its address, which is not a
	// Go pointer and therefore may be converted this way.
	buf := *(*unsafe.Pointer)(unsafe.Pointer(&r))
	return windows.UTF16ToString(unsafe.Slice((*uint16)(buf), n))
}

// The Windows Runtime has to be initialised on a thread before its objects
// can be used, and stays usable for as long as one initialised thread
// lives. Go moves goroutines between threads, so one thread is set aside to
// hold the runtime open; all the others then belong to the same
// multithreaded apartment without initialising anything themselves.
var (
	runtimeOnce sync.Once
	runtimeErr  error
)

func initRuntime() error {
	runtimeOnce.Do(func() {
		if err := combase.Load(); err != nil {
			runtimeErr = fmt.Errorf("the Windows Runtime is not available: %w", err)
			return
		}
		ready := make(chan error)
		go func() {
			runtime.LockOSThread()
			const multithreaded = 1
			r, _, _ := procRoInitialize.Call(multithreaded)
			if hr := hresult(r); hr.failed() && hr != rpcEChangedMode {
				ready <- fmt.Errorf("initialising the Windows Runtime: %w", hr)
				return
			}
			ready <- nil
			select {} // the thread is the one that keeps the runtime open
		}()
		runtimeErr = <-ready
	})
	return runtimeErr
}

// library is the API's DLL when Windows has not registered it and it was
// loaded from beside the program instead.
type library struct {
	getActivationFactory uintptr
}

var (
	localOnce sync.Once
	local     *library
	localErr  error
)

// dllName is the DLL that implements Windows.Devices.Midi2.
const dllName = "Windows.Devices.Midi2.dll"

// DLLVariable names the environment variable that can hold the full path
// of the API's DLL, for when it is neither part of Windows nor beside the
// program. A program started with "go run" lives in a temporary folder,
// which is where that is needed.
const DLLVariable = "HIDI_MIDI_DLL"

// programDir returns the folder of the running program.
func programDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "the program's folder"
	}
	return filepath.Dir(exe)
}

// loadLocal loads the API's DLL for a Windows whose own copy has not
// arrived yet: from the path in the environment variable if it is set, and
// otherwise from the system directory or from beside the program.
func loadLocal() (*library, error) {
	localOnce.Do(func() {
		var module windows.Handle
		var err error
		if path := os.Getenv(DLLVariable); path != "" {
			// Only a full path is taken, so that the variable cannot make
			// Windows search for the name somewhere unexpected.
			if !filepath.IsAbs(path) {
				localErr = fmt.Errorf("%s must hold a full path, not %q", DLLVariable, path)
				return
			}
			module, err = windows.LoadLibraryEx(path, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
			if err != nil {
				localErr = fmt.Errorf("%s names %s, which cannot be loaded: %w", DLLVariable, path, err)
				return
			}
		} else {
			module, err = windows.LoadLibraryEx(dllName, 0,
				windows.LOAD_LIBRARY_SEARCH_SYSTEM32|windows.LOAD_LIBRARY_SEARCH_APPLICATION_DIR)
			if err != nil {
				localErr = fmt.Errorf("%s was not found in Windows or in %s (%w); put Microsoft's preview copy of the file there, or set %s to its full path",
					dllName, programDir(), err, DLLVariable)
				return
			}
		}
		proc, err := windows.GetProcAddress(module, "DllGetActivationFactory")
		if err != nil {
			localErr = fmt.Errorf("%s is not the expected library: %w", dllName, err)
			return
		}
		local = &library{getActivationFactory: proc}
	})
	return local, localErr
}

// source says where the API was found.
type source int

const (
	sourceNone   source = iota
	sourceSystem        // registered with Windows
	sourceLocal         // the DLL beside the program
)

func (s source) String() string {
	switch s {
	case sourceSystem:
		return "Windows"
	case sourceLocal:
		if path := os.Getenv(DLLVariable); path != "" {
			return path
		}
		return filepath.Join(programDir(), dllName)
	}
	return "nowhere"
}

// factory returns the given interface of a class's activation factory:
// the object that creates instances of the class and carries its static
// methods. It asks Windows first and falls back to the DLL beside the
// program.
func factory(class string, iid windows.GUID) (object, source, error) {
	if err := initRuntime(); err != nil {
		return object{}, sourceNone, err
	}
	name, err := newHString(class)
	if err != nil {
		return object{}, sourceNone, err
	}
	defer name.free()

	var out object
	r, _, _ := procRoGetActivationFactory.Call(uintptr(name), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out.ptr)))
	if hr := hresult(r); !hr.failed() {
		return out, sourceSystem, nil
	} else if hr != regdbEClassNotReg {
		return object{}, sourceNone, fmt.Errorf("activating %s: %w", class, hr)
	}

	lib, err := loadLocal()
	if err != nil {
		return object{}, sourceNone, fmt.Errorf("%s is not part of this Windows, and %w", class, err)
	}
	var base object
	r, _, _ = syscall.SyscallN(lib.getActivationFactory, uintptr(name), uintptr(unsafe.Pointer(&base.ptr)))
	if hr := hresult(r); hr.failed() {
		return object{}, sourceNone, fmt.Errorf("activating %s from %s: %w", class, dllName, hr)
	}
	defer base.release()
	out, err = base.query(iid)
	if err != nil {
		return object{}, sourceNone, fmt.Errorf("%s does not have the expected interface: %w", class, err)
	}
	return out, sourceLocal, nil
}

// activate creates an instance of a class that has a constructor without
// parameters, and returns its given interface.
func activate(class string, iid windows.GUID) (object, error) {
	f, _, err := factory(class, iidIActivationFactory)
	if err != nil {
		return object{}, err
	}
	defer f.release()
	var instance object
	if hr := f.call(6, uintptr(unsafe.Pointer(&instance.ptr))); hr.failed() {
		return object{}, fmt.Errorf("creating a %s: %w", class, hr)
	}
	if instance.isNil() {
		return object{}, errors.New("creating a " + class + " returned nothing")
	}
	defer instance.release()
	out, err := instance.query(iid)
	if err != nil {
		return object{}, fmt.Errorf("%s does not have the expected interface: %w", class, err)
	}
	return out, nil
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
