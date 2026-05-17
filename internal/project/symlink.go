package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LinkHarnessIntoWorkingCopy creates a symlink in workingCopy for every
// top-level entry of <metaRoot>/.mws/. Existing entries in workingCopy that are
// NOT symlinks are left untouched; existing symlinks are recreated so they
// point at the current harness entry. Symlink targets are relative paths from
// workingCopy back into the harness.
//
// Returns the names that were created/refreshed (sorted by name as returned by os.ReadDir).
func LinkHarnessIntoWorkingCopy(metaRoot, workingCopy string) ([]string, error) {
	harness := filepath.Join(metaRoot, HarnessDirName)
	entries, err := os.ReadDir(harness)
	if err != nil {
		return nil, fmt.Errorf("read harness %s: %w", harness, err)
	}
	if err := os.MkdirAll(workingCopy, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir working copy %s: %w", workingCopy, err)
	}

	rel, err := filepath.Rel(workingCopy, harness)
	if err != nil {
		return nil, fmt.Errorf("relpath harness -> working copy: %w", err)
	}

	var linked []string
	for _, e := range entries {
		name := e.Name()
		target := filepath.Join(rel, name)
		linkPath := filepath.Join(workingCopy, name)

		// Preserve any non-symlink that already exists at linkPath -- the user
		// chose to keep a local copy of a harness entry.
		if st, err := os.Lstat(linkPath); err == nil && st.Mode()&os.ModeSymlink == 0 {
			continue
		}

		if err := AtomicSymlink(target, linkPath); err != nil {
			return nil, err
		}
		linked = append(linked, name)
	}
	return linked, nil
}

// AtomicSymlink creates a symlink at linkPath pointing to target. If linkPath
// already exists (as a symlink or otherwise), it is replaced atomically: the
// new link is written to a sibling tempname and renamed over linkPath via
// os.Rename, which on POSIX replaces the destination in a single syscall.
// This avoids the TOCTOU window of remove-then-symlink, during which the path
// is briefly absent and concurrent readers see ENOENT.
func AtomicSymlink(target, linkPath string) error {
	dir := filepath.Dir(linkPath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(linkPath)+".mws-link-*")
	if err != nil {
		return fmt.Errorf("create temp for symlink %s: %w", linkPath, err)
	}
	tmpName := tmp.Name()
	// We only needed CreateTemp to reserve a unique name; close and remove the
	// file so os.Symlink can create a symlink at the same path.
	_ = tmp.Close()
	if err := os.Remove(tmpName); err != nil {
		return fmt.Errorf("clear temp %s: %w", tmpName, err)
	}
	if err := os.Symlink(target, tmpName); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", tmpName, target, err)
	}
	if err := os.Rename(tmpName, linkPath); err != nil {
		renameErr := fmt.Errorf("rename %s -> %s: %w", tmpName, linkPath, err)
		if rmErr := os.Remove(tmpName); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return errors.Join(renameErr, fmt.Errorf("remove orphaned temp symlink %s: %w", tmpName, rmErr))
		}
		return renameErr
	}
	return nil
}
