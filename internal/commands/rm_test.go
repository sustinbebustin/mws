package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
)

// setupRmTree builds a meta workspace with a harness and one working copy that
// has real harness symlinks, so looksLikeWorkingCopy passes. Returns the meta
// root and the working copy path.
func setupRmTree(t *testing.T, cfg *config.Config, copyName string) (string, string) {
	t.Helper()
	meta := filepath.Join(t.TempDir(), "demo")
	harness := filepath.Join(meta, project.HarnessDirName)
	mustMkdir(t, harness)
	mustWriteFile(t, filepath.Join(harness, "CLAUDE.md"), "# harness")
	if err := config.Save(meta, cfg); err != nil {
		t.Fatal(err)
	}
	wc := filepath.Join(meta, cfg.WorkingCopiesDir, copyName)
	mustMkdir(t, wc)
	if _, err := project.LinkHarnessIntoWorkingCopy(meta, wc); err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}
	return meta, wc
}

func TestRunRmRefusesUnmanagedDir(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(meta, project.HarnessDirName))
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	// A directory at meta root that contains no harness symlinks -- definitely
	// not a working copy.
	stray := filepath.Join(meta, "stray")
	mustMkdir(t, stray)
	mustWriteFile(t, filepath.Join(stray, "file.txt"), "user data")

	withCwd(t, meta, func() {
		err := runRm(nopReporter{}, "stray", true, false)
		if err == nil {
			t.Fatalf("runRm should refuse a dir with no harness symlinks")
		}
		if !strings.Contains(err.Error(), "does not contain any harness symlinks") {
			t.Fatalf("error doesn't mention harness symlinks: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(stray, "file.txt")); err != nil {
		t.Fatalf("refused rm should not have touched the directory: %v", err)
	}
}

func TestRunRmTrashesWorkingCopyByDefault(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")
	mustWriteFile(t, filepath.Join(wc, "local.txt"), "uncommitted work")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
	})

	if _, err := os.Stat(wc); err == nil {
		t.Fatalf("working copy still in place after rm")
	}
	entries, skipped, err := trash.List(meta)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected unreadable trash entries: %v", skipped)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 trash entry, got %d", len(entries))
	}
	if entries[0].Name != "feature" {
		t.Fatalf("trash entry name = %q, want feature", entries[0].Name)
	}
	// The copy went in verbatim, local files and all.
	body, err := os.ReadFile(filepath.Join(entries[0].CopyPath, "local.txt"))
	if err != nil {
		t.Fatalf("read trashed local.txt: %v", err)
	}
	if string(body) != "uncommitted work" {
		t.Fatalf("trashed local.txt = %q", body)
	}
}

func TestRunRmPurgeDeletesOutright(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, true); err != nil {
			t.Fatalf("runRm: %v", err)
		}
	})

	if _, err := os.Stat(wc); err == nil {
		t.Fatalf("working copy still exists after purge")
	}
	if _, err := os.Stat(trash.Root(meta)); err == nil {
		t.Fatalf("--purge should not have created a trash dir")
	}
}

func TestRunRmHonoursTrashDisabled(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{
		ProjectName: "demo",
		Trash:       &config.Trash{Disabled: true},
	}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
	})

	if _, err := os.Stat(wc); err == nil {
		t.Fatalf("working copy still exists with trash disabled")
	}
	if _, err := os.Stat(trash.Root(meta)); err == nil {
		t.Fatalf("trash.disabled should not have created a trash dir")
	}
}

func TestRunRmTrashesWorkingCopyUnderCopiesDir(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{
		ProjectName:      "demo",
		WorkingCopiesDir: "copies",
	}, "feature")

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true, false); err != nil {
			t.Fatalf("runRm: %v", err)
		}
	})

	if _, err := os.Stat(wc); err == nil {
		t.Fatalf("working copy at %s still exists after rm", wc)
	}
	// The copies subdir itself remains.
	if _, err := os.Stat(filepath.Join(meta, "copies")); err != nil {
		t.Fatalf("copies subdir was removed unexpectedly: %v", err)
	}
	// The trash lives at the meta root, not under the copies dir.
	entries, _, err := trash.List(meta)
	if err != nil {
		t.Fatalf("trash.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 trash entry at the meta root, got %d", len(entries))
	}
}
