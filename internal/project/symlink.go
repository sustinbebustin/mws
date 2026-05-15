package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// LinkMetaIntoWorkingCopy creates a symlink in workingCopy for every top-level entry of metaRoot,
// excluding ".git" (the meta's own git data). Existing entries in workingCopy that are NOT symlinks
// are left untouched; existing symlinks are recreated to point at the current meta entry.
//
// Returns the names that were created/refreshed.
func LinkMetaIntoWorkingCopy(metaRoot, workingCopy string) ([]string, error) {
	entries, err := os.ReadDir(metaRoot)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", metaRoot, err)
	}
	if err := os.MkdirAll(workingCopy, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir working copy %s: %w", workingCopy, err)
	}

	rel, err := filepath.Rel(workingCopy, metaRoot)
	if err != nil {
		return nil, fmt.Errorf("relpath meta -> working copy: %w", err)
	}

	var linked []string
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		target := filepath.Join(rel, name)
		linkPath := filepath.Join(workingCopy, name)

		// If something already exists at linkPath that is not a symlink, skip it -- preserve user content.
		if st, err := os.Lstat(linkPath); err == nil {
			if st.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if err := os.Remove(linkPath); err != nil {
				return nil, fmt.Errorf("remove stale symlink %s: %w", linkPath, err)
			}
		}

		if err := os.Symlink(target, linkPath); err != nil {
			return nil, fmt.Errorf("symlink %s -> %s: %w", linkPath, target, err)
		}
		linked = append(linked, name)
	}
	return linked, nil
}
