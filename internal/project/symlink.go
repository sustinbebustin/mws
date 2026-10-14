package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LinkHarnessIntoWorkingCopy creates a symlink in workingCopy for every
// top-level entry of <metaRoot>/.mws/. Existing entries in workingCopy that are
// NOT symlinks are left untouched; existing symlinks that point elsewhere are
// recreated so they point at the current harness entry, and correct ones are
// kept. Symlink targets are relative paths from workingCopy back into the
// harness.
//
// Returns the names that were created or repointed (sorted by name as returned
// by os.ReadDir), so running it twice returns nothing the second time.
func LinkHarnessIntoWorkingCopy(metaRoot, workingCopy string) ([]string, error) {
	if err := os.MkdirAll(workingCopy, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir working copy %s: %w", workingCopy, err)
	}
	links, err := pendingHarnessLinks(metaRoot, workingCopy)
	if err != nil {
		return nil, err
	}
	linked := make([]string, 0, len(links))
	for _, l := range links {
		if err := AtomicSymlink(l.target, filepath.Join(workingCopy, l.name)); err != nil {
			return nil, err
		}
		linked = append(linked, l.name)
	}
	return linked, nil
}

// PendingHarnessLinks returns the names LinkHarnessIntoWorkingCopy would create
// or repoint in workingCopy, without changing anything.
func PendingHarnessLinks(metaRoot, workingCopy string) ([]string, error) {
	links, err := pendingHarnessLinks(metaRoot, workingCopy)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(links))
	for i, l := range links {
		names[i] = l.name
	}
	return names, nil
}

// harnessLink is one symlink to write: name in the working copy, pointing at
// target, relative to the working copy.
type harnessLink struct{ name, target string }

func pendingHarnessLinks(metaRoot, workingCopy string) ([]harnessLink, error) {
	harness := filepath.Join(metaRoot, HarnessDirName)
	entries, err := os.ReadDir(harness)
	if err != nil {
		return nil, fmt.Errorf("read harness %s: %w", harness, err)
	}
	rel, err := filepath.Rel(workingCopy, harness)
	if err != nil {
		return nil, fmt.Errorf("relpath harness -> working copy: %w", err)
	}

	var links []harnessLink
	for _, e := range entries {
		name := e.Name()
		target := filepath.Join(rel, name)
		linkPath := filepath.Join(workingCopy, name)

		if st, err := os.Lstat(linkPath); err == nil {
			// Preserve any non-symlink that already exists at linkPath -- the
			// user chose to keep a local copy of a harness entry.
			if st.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if cur, err := os.Readlink(linkPath); err == nil && cur == target {
				continue
			}
		}
		links = append(links, harnessLink{name: name, target: target})
	}
	return links, nil
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
