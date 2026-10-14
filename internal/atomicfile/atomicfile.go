// Package atomicfile copies files so readers never observe a partial write.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Copy copies src to dst, creating dst's parent directory as needed.
// The destination is written through a tempfile in the same directory and
// renamed into place, so the copy is atomic from a reader's perspective.
// The destination's permissions are explicitly chmod'd to match src, so
// they survive overwriting an existing dst and aren't subject to umask.
func Copy(src, dst string) error {
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstDir, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	tmp, err := os.CreateTemp(dstDir, "."+filepath.Base(dst)+".mws-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	// cleanup removes the orphaned tempfile. Returned errors are surfaced
	// via errors.Join at each failure site so they aren't silently dropped.
	cleanup := func() error {
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove temp %s: %w", tmpName, err)
		}
		return nil
	}

	if _, err := io.Copy(tmp, in); err != nil {
		return errors.Join(fmt.Errorf("copy %s -> %s: %w", src, dst, err), tmp.Close(), cleanup())
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		return errors.Join(fmt.Errorf("chmod %s: %w", dst, err), tmp.Close(), cleanup())
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close %s: %w", dst, err), cleanup())
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return errors.Join(fmt.Errorf("rename %s -> %s: %w", tmpName, dst, err), cleanup())
	}
	return nil
}
