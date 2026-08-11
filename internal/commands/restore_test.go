package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/trash"
)

// TestRunRestoreRoundTripsHarnessSymlinks is the regression this design is most
// likely to break: harness symlinks are relative, so they dangle while a copy
// sits in the trash and must be repaired on the way out.
func TestRunRestoreRoundTripsHarnessSymlinks(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")
	mustWriteFile(t, filepath.Join(wc, "local.txt"), "uncommitted work")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
		if err := runRestore(nopReporter{}, "feature", "", true); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})

	body, err := os.ReadFile(filepath.Join(wc, "local.txt"))
	if err != nil {
		t.Fatalf("read restored local.txt: %v", err)
	}
	if string(body) != "uncommitted work" {
		t.Fatalf("restored local.txt = %q", body)
	}

	// The harness symlink must resolve, not dangle.
	link := filepath.Join(wc, "CLAUDE.md")
	st, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat %s: %v", link, err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink after restore", link)
	}
	harnessBody, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("restored harness symlink dangles: %v", err)
	}
	if string(harnessBody) != "# harness" {
		t.Fatalf("harness symlink resolves to %q", harnessBody)
	}

	entries, _, err := trash.List(meta)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("restore should have cleared the trash entry, got %d", len(entries))
	}
}

func TestRunRestoreRefusesToOverwriteExistingCopy(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
		// A new working copy claims the name while the old one is in the trash.
		mustMkdir(t, wc)
		mustWriteFile(t, filepath.Join(wc, "new.txt"), "new work")

		err := runRestore(nopReporter{}, "feature", "", true)
		if err == nil {
			t.Fatalf("runRestore should refuse to overwrite an existing working copy")
		}
		if !strings.Contains(err.Error(), "--as") {
			t.Fatalf("error should point at --as: %v", err)
		}
	})

	// Neither side was touched.
	if _, err := os.Stat(filepath.Join(wc, "new.txt")); err != nil {
		t.Fatalf("existing working copy was disturbed: %v", err)
	}
	entries, _, err := trash.List(meta)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("trash entry should survive a refused restore, got %d", len(entries))
	}
}

func TestRunRestoreAsAlternateName(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
		if err := runRestore(nopReporter{}, "feature", "feature-recovered", true); err != nil {
			t.Fatalf("runRestore: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(meta, "feature-recovered", "CLAUDE.md")); err != nil {
		t.Fatalf("restored copy missing at the --as name: %v", err)
	}
	if _, err := os.Stat(wc); err == nil {
		t.Fatalf("--as should not have restored under the original name")
	}
}

func TestRunRestoreReportsEmptyTrash(t *testing.T) {
	meta, _ := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")

	withCwd(t, meta, func() {
		err := runRestore(nopReporter{}, "", "", true)
		if err == nil {
			t.Fatalf("runRestore should fail when there is nothing to restore")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Fatalf("error should say the trash is empty: %v", err)
		}
	})
}

func TestRunRestoreRejectsUnknownName(t *testing.T) {
	meta, _ := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
		err := runRestore(nopReporter{}, "nope", "", true)
		if err == nil {
			t.Fatalf("runRestore should fail on a name that was never trashed")
		}
		if !strings.Contains(err.Error(), "mws trash list") {
			t.Fatalf("error should point at `mws trash list`: %v", err)
		}
	})
}
