package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sustinbebustin/mws/internal/project"
)

// copyFile copies src to dst, creating dst's parent directory as needed.
// The destination is written through a tempfile in the same directory and
// renamed into place, so the copy is atomic from a reader's perspective.
// The destination's permissions are explicitly chmod'd to match src, so
// they survive overwriting an existing dst and aren't subject to umask.
func copyFile(src, dst string) error {
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

// envStagingDir returns the env-staging root inside metaRoot.
func envStagingDir(metaRoot string) string {
	return filepath.Join(metaRoot, project.EnvStagingDirName)
}

// looksLikeWorkingCopy reports whether candidate is plausibly an mws-managed
// working copy of metaRoot: at least one top-level entry is a symlink that
// resolves into <metaRoot>/.mws/. Used as a guard against rm'ing a directory
// that happens to live under the meta root but isn't actually a working copy.
func looksLikeWorkingCopy(metaRoot, candidate string) bool {
	harness, err := filepath.EvalSymlinks(filepath.Join(metaRoot, project.HarnessDirName))
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(candidate)
	if err != nil {
		return false
	}
	for _, e := range entries {
		linkPath := filepath.Join(candidate, e.Name())
		st, err := os.Lstat(linkPath)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(harness, resolved)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return true
		}
		// Also accept a symlink whose target IS the harness root itself
		// (defensive; unusual but legal).
		if resolved == harness {
			return true
		}
	}
	return false
}

// moveFile moves src to dst. It first tries os.Rename for an atomic move;
// if rename fails with EXDEV (src and dst on different filesystems), it
// falls back to copyFile followed by os.Remove(src).
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("copied %s to %s but failed to remove source: %w", src, dst, err)
	}
	return nil
}
