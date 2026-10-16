package ports

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// LockMeta takes an exclusive flock on the meta root directory, blocking
// until it is free, so concurrent mws runs serialise the read-decide-write
// of a slot allocation without a lock file (ADR-0015). unlock releases it;
// closing the descriptor drops the lock, and a read-only directory
// descriptor has nothing to flush, so unlock reports no error. The kernel
// also drops the lock if the process dies while holding it.
func LockMeta(metaRoot string) (unlock func(), err error) {
	dir, err := os.Open(metaRoot)
	if err != nil {
		return nil, fmt.Errorf("lock meta workspace: %w", err)
	}
	for {
		err = syscall.Flock(int(dir.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		dir.Close()
		return nil, fmt.Errorf("lock meta workspace %s: flock: %w", metaRoot, err)
	}
	return func() { dir.Close() }, nil
}
