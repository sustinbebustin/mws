package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/config"
)

// Rerender brings the carried files of every worktree of the working copy at
// copyPath up to date with the copy after a render. rels are the files the
// render owns, slash-separated and relative to the copy (the lease, the env
// targets, the [[renders]] targets); each lives at the same relative path in
// a worktree.
//
// A carried file is rewritten only while its content still matches the hash
// the worktree's marker records, the rule `wt rm` uses to tell mws's files
// from the user's; the hash is updated after the write. A file the user edited
// (or deleted) is left alone with a warning naming the worktree and the file.
// A file the copy has but the worktree never got, and whose directory exists
// in the worktree, is carried now; one the copy no longer has is removed from
// the worktree while pristine. Every worktree is attempted; the error joins
// the failures.
func Rerender(r Reporter, copyPath string, policy config.WorktreePolicy, rels []string) error {
	roots, err := Roots(copyPath, policy)
	if err != nil {
		return fmt.Errorf("list worktrees of %s: %w", copyPath, err)
	}
	var errs []error
	for _, root := range roots {
		if err := rerenderOne(r, copyPath, root, rels); err != nil {
			errs = append(errs, fmt.Errorf("worktree %s: %w", filepath.Base(root), err))
		}
	}
	return errors.Join(errs...)
}

// rerenderOne is Rerender for the worktree at root.
func rerenderOne(r Reporter, copyPath, root string, rels []string) error {
	m, err := readMarker(root)
	if err != nil {
		return fmt.Errorf("read marker: %w", err)
	}
	if m.Copied == nil {
		m.Copied = map[string]string{}
	}
	name := filepath.Base(root)
	changed := false
	var errs []error
	for _, rel := range rels {
		updated, err := rerenderFile(r, name, copyPath, root, rel, m.Copied)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
		}
		changed = changed || updated
	}
	if changed {
		if err := updateMarker(root, m); err != nil {
			errs = append(errs, fmt.Errorf("update marker: %w", err))
		}
	}
	return errors.Join(errs...)
}

// rerenderFile applies Rerender's rule to one file, keeping hashes in step,
// and reports whether it changed hashes.
func rerenderFile(r Reporter, wtName, copyPath, root, rel string, hashes map[string]string) (bool, error) {
	src := filepath.Join(copyPath, filepath.FromSlash(rel))
	dst := filepath.Join(root, filepath.FromSlash(rel))
	want, carried := hashes[rel]

	fresh, err := os.ReadFile(src)
	srcGone := errors.Is(err, fs.ErrNotExist)
	if err != nil && !srcGone {
		return false, err
	}
	current, err := os.ReadFile(dst)
	dstGone := errors.Is(err, fs.ErrNotExist)
	if err != nil && !dstGone {
		return false, err
	}

	switch {
	case !carried && (!dstGone || srcGone):
		return false, nil // not mws's file, or nothing to carry
	case !carried:
		if _, err := os.Stat(filepath.Dir(dst)); err != nil {
			return false, nil // the worktree does not hold this repo
		}
	case !srcGone && !dstGone && bytes.Equal(current, fresh):
		// Already the copy's content, whoever wrote it: only the hash moves.
		h := hashBytes(fresh)
		updated := hashes[rel] != h
		hashes[rel] = h
		return updated, nil
	case dstGone:
		r.Warn(fmt.Sprintf("worktree %s: %s was deleted since mws carried it, leaving it out; copy it from the working copy by hand if the worktree needs it", wtName, rel))
		return false, nil
	case hashBytes(current) != want:
		r.Warn(fmt.Sprintf("worktree %s: %s was edited since mws carried it, leaving it, so it keeps its old values; copy the working copy's file over it by hand to take the new ones", wtName, rel))
		return false, nil
	}

	if srcGone {
		if err := os.Remove(dst); err != nil {
			return false, err
		}
		delete(hashes, rel)
		r.OK(fmt.Sprintf("worktree %s: removed %s", wtName, rel))
		return true, nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	if err := atomicfile.Write(dst, fresh, info.Mode().Perm()); err != nil {
		return false, err
	}
	hashes[rel] = hashBytes(fresh)
	r.OK(fmt.Sprintf("worktree %s: re-rendered %s", wtName, rel))
	return true, nil
}
