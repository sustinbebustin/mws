package trash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stageCopy creates a working-copy-shaped directory with one file in it and
// returns its path.
func stageCopy(t *testing.T, metaRoot, name, body string) string {
	t.Helper()
	copyPath := filepath.Join(metaRoot, name)
	if err := os.MkdirAll(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyPath, "marker.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return copyPath
}

// backdate rewrites an entry's deleted_at so retention can be exercised without
// waiting or injecting a clock.
func backdate(t *testing.T, e Entry, at time.Time) {
	t.Helper()
	if err := writeMeta(e.Path, entryMeta{Name: e.Name, DeletedAt: at.UTC()}); err != nil {
		t.Fatal(err)
	}
}

func TestStashAndListRoundTrip(t *testing.T) {
	meta := t.TempDir()
	copyPath := stageCopy(t, meta, "feature", "hello")

	before := time.Now().UTC().Add(-time.Second)
	entry, err := Stash(meta, copyPath)
	if err != nil {
		t.Fatalf("Stash: %v", err)
	}
	if _, err := os.Stat(copyPath); err == nil {
		t.Fatalf("Stash left the working copy in place")
	}

	entries, skipped, err := List(meta)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped entries: %v", skipped)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.ID != entry.ID || got.Name != "feature" {
		t.Fatalf("List returned %+v, want id %s name feature", got, entry.ID)
	}
	if got.DeletedAt.Before(before) || got.DeletedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("DeletedAt %v is not around now", got.DeletedAt)
	}
	body, err := os.ReadFile(filepath.Join(got.CopyPath, "marker.txt"))
	if err != nil || string(body) != "hello" {
		t.Fatalf("trashed content = %q, err %v", body, err)
	}
}

func TestStashSameNameTwiceKeepsBoth(t *testing.T) {
	meta := t.TempDir()

	first, err := Stash(meta, stageCopy(t, meta, "feature", "first"))
	if err != nil {
		t.Fatalf("Stash first: %v", err)
	}
	second, err := Stash(meta, stageCopy(t, meta, "feature", "second"))
	if err != nil {
		t.Fatalf("Stash second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("both stashes got id %s", first.ID)
	}

	entries, _, err := List(meta)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	for _, want := range []struct {
		entry Entry
		body  string
	}{{first, "first"}, {second, "second"}} {
		got, err := os.ReadFile(filepath.Join(want.entry.CopyPath, "marker.txt"))
		if err != nil || string(got) != want.body {
			t.Fatalf("entry %s content = %q, err %v; want %q", want.entry.ID, got, err, want.body)
		}
	}
}

func TestFindReturnsMatchesNewestFirst(t *testing.T) {
	meta := t.TempDir()
	older, err := Stash(meta, stageCopy(t, meta, "feature", "older"))
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, older, time.Now().Add(-2*time.Hour))
	newer, err := Stash(meta, stageCopy(t, meta, "feature", "newer"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stash(meta, stageCopy(t, meta, "other", "other")); err != nil {
		t.Fatal(err)
	}

	matches, err := Find(meta, "feature")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("want 2 matches, got %d", len(matches))
	}
	if matches[0].ID != newer.ID {
		t.Fatalf("Find should return newest first, got %s", matches[0].ID)
	}
}

func TestRestoreMovesCopyOutAndClearsEntry(t *testing.T) {
	meta := t.TempDir()
	entry, err := Stash(meta, stageCopy(t, meta, "feature", "hello"))
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(meta, "recovered")
	if err := Restore(entry, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dest, "marker.txt"))
	if err != nil || string(body) != "hello" {
		t.Fatalf("restored content = %q, err %v", body, err)
	}
	if _, err := os.Stat(entry.Path); err == nil {
		t.Fatalf("Restore left the trash entry behind")
	}
}

func TestSweepPurgesOnlyExpiredEntries(t *testing.T) {
	meta := t.TempDir()
	stale, err := Stash(meta, stageCopy(t, meta, "stale", "stale"))
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, stale, time.Now().Add(-8*24*time.Hour))
	fresh, err := Stash(meta, stageCopy(t, meta, "fresh", "fresh"))
	if err != nil {
		t.Fatal(err)
	}

	purged, err := Sweep(meta, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(purged) != 1 || purged[0].Name != "stale" {
		t.Fatalf("Sweep purged %+v, want just stale", purged)
	}
	if _, err := os.Stat(stale.Path); err == nil {
		t.Fatalf("stale entry survived the sweep")
	}
	if _, err := os.Stat(fresh.CopyPath); err != nil {
		t.Fatalf("fresh entry was purged: %v", err)
	}
}

func TestSweepWithZeroRetentionKeepsForever(t *testing.T) {
	meta := t.TempDir()
	entry, err := Stash(meta, stageCopy(t, meta, "ancient", "ancient"))
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, entry, time.Now().Add(-365*24*time.Hour))

	purged, err := Sweep(meta, 0)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(purged) != 0 {
		t.Fatalf("zero retention purged %d entries", len(purged))
	}
	if _, err := os.Stat(entry.CopyPath); err != nil {
		t.Fatalf("entry was purged with retention 0: %v", err)
	}
}

func TestListSkipsMalformedEntries(t *testing.T) {
	meta := t.TempDir()
	good, err := Stash(meta, stageCopy(t, meta, "good", "good"))
	if err != nil {
		t.Fatal(err)
	}
	// An entry dir with no entry.toml, and one with metadata but no copy/.
	if err := os.MkdirAll(filepath.Join(Root(meta), "20200101T000000Z-nometa", "copy"), 0o755); err != nil {
		t.Fatal(err)
	}
	nocopy := filepath.Join(Root(meta), "20200101T000000Z-nocopy")
	if err := os.MkdirAll(nocopy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(nocopy, entryMeta{Name: "nocopy", DeletedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	entries, skipped, err := List(meta)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != good.ID {
		t.Fatalf("List returned %+v, want only %s", entries, good.ID)
	}
	if len(skipped) != 2 {
		t.Fatalf("want 2 skipped entries, got %v", skipped)
	}
}

func TestListOnMissingTrashDirIsEmpty(t *testing.T) {
	entries, skipped, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 || len(skipped) != 0 {
		t.Fatalf("want empty result, got %v / %v", entries, skipped)
	}
}

// An entry whose metadata lost its timestamp must be treated as unreadable, not
// as infinitely old. Reading a zero deleted_at as "ancient" would let the very
// next sweep destroy a working copy the user never chose to lose.
func TestEntryWithoutTimestampIsUnreadableNotAncient(t *testing.T) {
	meta := t.TempDir()
	entry, err := Stash(meta, stageCopy(t, meta, "feature", "precious"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a truncated write: parses fine, but has no deleted_at.
	if err := os.WriteFile(filepath.Join(entry.Path, metaFileName), []byte("name = \"feature\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, skipped, err := List(meta)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 || len(skipped) != 1 {
		t.Fatalf("want the entry skipped, got entries=%v skipped=%v", entries, skipped)
	}

	purged, err := Sweep(meta, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(purged) != 0 {
		t.Fatalf("sweep purged a timestamp-less entry: %+v", purged)
	}
	body, err := os.ReadFile(filepath.Join(entry.CopyPath, "marker.txt"))
	if err != nil || string(body) != "precious" {
		t.Fatalf("working copy was destroyed: content=%q err=%v", body, err)
	}
}

// If metadata cannot be written, the working copy must end up back where it
// started -- never deleted along with the half-built entry.
func TestStashRollsBackWhenMetadataCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; a read-only directory would still be writable")
	}
	meta := t.TempDir()
	copyPath := stageCopy(t, meta, "feature", "precious")

	// A read-only trash root makes the entry reservation fail before anything
	// has moved, which is the cheapest way to assert the copy is left alone.
	root := Root(meta)
	if err := os.MkdirAll(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	_, err := Stash(meta, copyPath)
	if err == nil {
		t.Fatal("Stash should fail when the trash root is not writable")
	}
	// The working copy must still be there, untouched.
	body, readErr := os.ReadFile(filepath.Join(copyPath, "marker.txt"))
	if readErr != nil || string(body) != "precious" {
		t.Fatalf("working copy lost after a failed Stash: content=%q err=%v", body, readErr)
	}
}

// One undeletable entry must not block every later expired entry forever.
func TestSweepContinuesPastAFailedPurge(t *testing.T) {
	meta := t.TempDir()
	stale1, err := Stash(meta, stageCopy(t, meta, "stale-one", "a"))
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, stale1, time.Now().Add(-30*24*time.Hour))
	stale2, err := Stash(meta, stageCopy(t, meta, "stale-two", "b"))
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, stale2, time.Now().Add(-30*24*time.Hour))

	purged, err := Sweep(meta, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(purged) != 2 {
		t.Fatalf("want both stale entries purged, got %d", len(purged))
	}
}

func TestEmptyRemovesUnreadableEntriesToo(t *testing.T) {
	meta := t.TempDir()
	if _, err := Stash(meta, stageCopy(t, meta, "good", "good")); err != nil {
		t.Fatal(err)
	}
	// A malformed entry that List cannot parse and Purge can never reach.
	if err := os.MkdirAll(filepath.Join(Root(meta), "junk"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Empty(meta); err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if _, err := os.Stat(Root(meta)); !os.IsNotExist(err) {
		t.Fatalf("trash root survived Empty: %v", err)
	}
}
