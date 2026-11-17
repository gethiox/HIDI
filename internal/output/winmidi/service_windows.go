package winmidi

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// serviceName is the name of the MIDI service of Windows. A device HIDI
// created lives in that service: when its process ends, by a fault or
// because somebody ended it to get a stuck service going again, the
// device is gone, while sending to it may well go on without an error.
const serviceName = "midisrv"

// servicePoll is how often the service's process is looked at.
const servicePoll = time.Second

// ErrServiceRestarted is what an endpoint reports once the MIDI service
// that held its device has ended.
var ErrServiceRestarted = errors.New("the MIDI service of Windows was stopped or restarted, and the MIDI device went with it")

// stuck is the state of the one MIDI service.
var stuck stuckState

var (
	watchService sync.Once
	// generation counts how often the service's process was seen to end.
	generation atomic.Uint64
)

// servicePID returns the process the MIDI service runs in, 0 while it is
// not running, and false if Windows does not tell.
func servicePID() (uint32, bool) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, false
	}
	defer windows.CloseServiceHandle(manager)
	name, _ := windows.UTF16PtrFromString(serviceName)
	service, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return 0, false
	}
	defer windows.CloseServiceHandle(service)
	var (
		status windows.SERVICE_STATUS_PROCESS
		needed uint32
	)
	if err := windows.QueryServiceStatusEx(service, windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)), uint32(unsafe.Sizeof(status)), &needed); err != nil {
		return 0, false
	}
	return status.ProcessId, true
}

// watch starts looking at the service's process, once. What it sees is
// handed to a [restarts], which counts.
func watch() {
	watchService.Do(func() {
		var seen restarts
		if pid, ok := servicePID(); ok {
			seen.see(pid)
		}
		go func() {
			for range time.Tick(servicePoll) {
				pid, ok := servicePID()
				if ok && seen.see(pid) {
					generation.Add(1)
					// What was stuck was the process that is gone.
					stuck.clear()
				}
			}
		}()
	})
}
