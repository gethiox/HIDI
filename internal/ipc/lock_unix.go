//go:build unix

package ipc

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile waits until this process alone holds the lock on f.
func lockFile(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if err != unix.EINTR {
			return err
		}
	}
}
