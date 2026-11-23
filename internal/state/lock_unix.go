//go:build !windows

package state

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func unlockFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// tryLockFile takes the lock only if nobody holds it. held is true when
// somebody does; any other failure is not "held", it is a lock that could not
// be asked about.
func tryLockFile(f *os.File) (held bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == unix.EWOULDBLOCK {
		return true, err
	}
	return false, err
}
