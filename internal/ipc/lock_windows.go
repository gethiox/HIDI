package ipc

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile waits until this process alone holds the lock on f.
func lockFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}
