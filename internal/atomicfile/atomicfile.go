// Package atomicfile writes files so readers never observe a partial write.
package atomicfile

import (
	"bytes"
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
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	return write(dst, in, st.Mode().Perm(), src)
}

// Write writes data to dst with permissions perm, creating dst's parent
// directory as needed, through the same tempfile-and-rename as Copy.
func Write(dst string, data []byte, perm os.FileMode) error {
	return write(dst, bytes.NewReader(data), perm, "data")
}

// write streams r into a tempfile beside dst, chmods it to perm and renames
// it over dst. from names the source in error messages.
func write(dst string, r io.Reader, perm os.FileMode, from string) error {
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstDir, err)
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

	if _, err := io.Copy(tmp, r); err != nil {
		return errors.Join(fmt.Errorf("copy %s -> %s: %w", from, dst, err), tmp.Close(), cleanup())
	}
	if err := tmp.Chmod(perm); err != nil {
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
