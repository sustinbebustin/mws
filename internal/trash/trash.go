// Package trash implements the soft-delete area for working copies. `mws rm`
// moves a working copy into <meta>/.trash/ instead of deleting it; `mws restore`
// moves it back, and an opportunistic sweep purges entries past their retention.
//
// An entry is a directory <meta>/.trash/<id>/ holding entry.toml (the metadata)
// and copy/ (the working copy, moved in verbatim by os.Rename). Trashing and
// restoring are renames, so they are instant and preserve modes, symlinks, and
// git object files exactly.
//
// The harness symlinks inside a trashed copy are relative and therefore dangle
// while the copy sits in .trash/ at a different depth. That is expected;
// callers repair them with project.LinkHarnessIntoWorkingCopy after restoring.
package trash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

// DirName is the meta-root directory holding trashed working copies. The dot
// prefix keeps it out of project.Workspace.EnumerateCopies and out of the
// working-copy namespace (project.ValidateName bans a leading dot).
const DirName = ".trash"

// metaFileName is the per-entry metadata file inside an entry directory.
const metaFileName = "entry.toml"

// copyDirName is the per-entry subdirectory holding the working copy itself.
const copyDirName = "copy"

// idTimeLayout formats an entry id's timestamp. Timestamp-first so a lexical
// sort of entry ids is chronological.
const idTimeLayout = "20060102T150405Z"

// Entry is one trashed working copy.
type Entry struct {
	// ID is the entry's directory name under .trash/. It is a human-readable
	// convenience only -- Name and DeletedAt come from entry.toml, never from
	// parsing this.
	ID string
	// Name is the working-copy name the entry was trashed under.
	Name string
	// DeletedAt is when the entry was trashed, in UTC.
	DeletedAt time.Time
	// Path is the absolute path to the entry directory.
	Path string
	// CopyPath is the absolute path to the working copy inside the entry.
	CopyPath string
}

// entryMeta is the on-disk shape of entry.toml.
type entryMeta struct {
	Name      string    `toml:"name"`
	DeletedAt time.Time `toml:"deleted_at"`
}

// Root returns the trash directory for a meta workspace. It may not exist.
func Root(metaRoot string) string {
	return filepath.Join(metaRoot, DirName)
}

// Stash moves the working copy at copyPath into the trash and returns the
// resulting entry. The copy is moved with a single rename; on success nothing
// remains at copyPath.
func Stash(metaRoot, copyPath string) (Entry, error) {
	root := Root(metaRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Entry{}, fmt.Errorf("mkdir %s: %w", root, err)
	}

	name := filepath.Base(copyPath)
	now := time.Now().UTC()
	entryDir, id, err := reserveEntryDir(root, now, name)
	if err != nil {
		return Entry{}, err
	}

	dest := filepath.Join(entryDir, copyDirName)
	if err := os.Rename(copyPath, dest); err != nil {
		rmErr := os.RemoveAll(entryDir)
		if errors.Is(err, syscall.EXDEV) {
			return Entry{}, errors.Join(fmt.Errorf(
				"cannot trash %s: it is on a different filesystem than %s, so it cannot be moved without copying it. Nothing was removed. Re-run with --purge to delete it outright, or set trash.disabled in .mws.toml: %w",
				copyPath, root, err), rmErr)
		}
		return Entry{}, errors.Join(fmt.Errorf("move %s -> %s: %w", copyPath, dest, err), rmErr)
	}

	if err := writeMeta(entryDir, entryMeta{Name: name, DeletedAt: now}); err != nil {
		// Put the working copy back so a metadata failure never costs the user
		// their files. Only discard the entry directory once the copy is safely
		// out of it -- clearing it unconditionally would delete the very copy
		// this rollback exists to preserve.
		if rbErr := os.Rename(dest, copyPath); rbErr != nil {
			return Entry{}, errors.Join(err, fmt.Errorf(
				"could not move it back to %s either. Your working copy is intact at %s -- move it back by hand: %w",
				copyPath, dest, rbErr))
		}
		return Entry{}, errors.Join(err, os.RemoveAll(entryDir))
	}

	return Entry{ID: id, Name: name, DeletedAt: now, Path: entryDir, CopyPath: dest}, nil
}

// reserveEntryDir creates a fresh entry directory under root and returns its
// path and id. os.Mkdir fails if the name is taken, so two removals of the same
// name within the same second get distinct entries rather than clobbering.
func reserveEntryDir(root string, now time.Time, name string) (string, string, error) {
	base := now.Format(idTimeLayout) + "-" + name
	for n := 1; n <= 100; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		dir := filepath.Join(root, id)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, id, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return "", "", fmt.Errorf("could not reserve a trash entry for %q under %s after 100 attempts", name, root)
}

// writeMeta writes an entry's metadata through a tempfile in the same directory
// and renames it into place. A half-written entry.toml would be worse than none
// at all: a truncated file can still parse, and a parsed entry missing its
// deleted_at would look infinitely old to Sweep.
func writeMeta(entryDir string, m entryMeta) error {
	p := filepath.Join(entryDir, metaFileName)
	tmp, err := os.CreateTemp(entryDir, "."+metaFileName+".mws-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", p, err)
	}
	tmpName := tmp.Name()
	cleanup := func() error {
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove temp %s: %w", tmpName, err)
		}
		return nil
	}
	if err := toml.NewEncoder(tmp).Encode(m); err != nil {
		return errors.Join(fmt.Errorf("encode %s: %w", p, err), tmp.Close(), cleanup())
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close %s: %w", tmpName, err), cleanup())
	}
	if err := os.Rename(tmpName, p); err != nil {
		return errors.Join(fmt.Errorf("rename %s -> %s: %w", tmpName, p, err), cleanup())
	}
	return nil
}

// List returns every readable trash entry, newest first, along with the ids of
// entries that could not be read. A hand-mangled or half-written entry is
// reported rather than fatal, so it cannot brick `mws rm`.
func List(metaRoot string) ([]Entry, []string, error) {
	root := Root(metaRoot)
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read trash %s: %w", root, err)
	}

	var entries []Entry
	var skipped []string
	for _, de := range dirEntries {
		if !de.IsDir() {
			skipped = append(skipped, de.Name())
			continue
		}
		e, err := load(root, de.Name())
		if err != nil {
			skipped = append(skipped, de.Name())
			continue
		}
		entries = append(entries, e)
	}

	// Newest first, with the id as a tiebreak so entries stashed within the same
	// second still have a stable, chronological order.
	slices.SortFunc(entries, func(a, b Entry) int {
		if c := b.DeletedAt.Compare(a.DeletedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	slices.Sort(skipped)
	return entries, skipped, nil
}

// load reads one entry directory. Both entry.toml and copy/ must be present for
// the entry to be usable.
func load(root, id string) (Entry, error) {
	dir := filepath.Join(root, id)
	data, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		return Entry{}, err
	}
	var m entryMeta
	if err := toml.Unmarshal(data, &m); err != nil {
		return Entry{}, err
	}
	if m.Name == "" {
		return Entry{}, fmt.Errorf("%s: entry.toml has no name", dir)
	}
	// A zero deleted_at would read as infinitely old and be purged by the very
	// next sweep, so an entry that lost its timestamp is unreadable, not ancient.
	if m.DeletedAt.IsZero() {
		return Entry{}, fmt.Errorf("%s: entry.toml has no deleted_at", dir)
	}
	copyPath := filepath.Join(dir, copyDirName)
	st, err := os.Stat(copyPath)
	if err != nil {
		return Entry{}, err
	}
	if !st.IsDir() {
		return Entry{}, fmt.Errorf("%s is not a directory", copyPath)
	}
	return Entry{ID: id, Name: m.Name, DeletedAt: m.DeletedAt.UTC(), Path: dir, CopyPath: copyPath}, nil
}

// Find returns the entries trashed under the given working-copy name, newest
// first. A name may have several entries if it was created and removed more
// than once.
func Find(metaRoot, name string) ([]Entry, error) {
	all, _, err := List(metaRoot)
	if err != nil {
		return nil, err
	}
	var matches []Entry
	for _, e := range all {
		if e.Name == name {
			matches = append(matches, e)
		}
	}
	return matches, nil
}

// Restore moves an entry's working copy to dest and discards the entry. dest
// must not already exist; callers are expected to check first and report a
// better error than a bare rename failure would.
func Restore(e Entry, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
	}
	if err := os.Rename(e.CopyPath, dest); err != nil {
		return fmt.Errorf("move %s -> %s: %w", e.CopyPath, dest, err)
	}
	if err := os.RemoveAll(e.Path); err != nil {
		return fmt.Errorf("restored %s but failed to clear trash entry %s: %w", dest, e.Path, err)
	}
	return nil
}

// Purge deletes a trash entry and everything in it. This is irreversible.
func Purge(e Entry) error {
	if err := os.RemoveAll(e.Path); err != nil {
		return fmt.Errorf("purge %s: %w", e.Path, err)
	}
	return nil
}

// Empty deletes the whole trash directory, including entries too malformed for
// List to return. Sweep and Purge can only act on entries they can parse, so
// this is the only way to reclaim a half-written or hand-mangled entry. This is
// irreversible.
func Empty(metaRoot string) error {
	root := Root(metaRoot)
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("empty %s: %w", root, err)
	}
	return nil
}

// Sweep purges every entry older than retention and returns those it purged.
// A retention of zero or less means "keep forever" and sweeps nothing. Nothing
// schedules a sweep; it happens only when a caller asks for one, so an entry
// outlives its retention until something calls this.
func Sweep(metaRoot string, retention time.Duration) ([]Entry, error) {
	if retention <= 0 {
		return nil, nil
	}
	entries, _, err := List(metaRoot)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-retention)
	var purged []Entry
	var errs []error
	for _, e := range entries {
		if e.DeletedAt.After(cutoff) {
			continue
		}
		// Keep going past a failure: one undeletable entry must not permanently
		// block every later expired entry from ever being reclaimed.
		if err := Purge(e); err != nil {
			errs = append(errs, err)
			continue
		}
		purged = append(purged, e)
	}
	return purged, errors.Join(errs...)
}
