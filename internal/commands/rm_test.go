package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

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
		err := runRm(nopReporter{}, "stray", true)
		if err == nil {
			t.Fatalf("runRm should refuse a dir with no harness symlinks")
		}
		if !strings.Contains(err.Error(), "does not contain any harness symlinks") {
			t.Fatalf("error doesn't mention harness symlinks: %v", err)
		}
	})
}

func TestRunRmRemovesValidWorkingCopy(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "demo")
	harness := filepath.Join(meta, project.HarnessDirName)
	mustMkdir(t, harness)
	mustWriteFile(t, filepath.Join(harness, "CLAUDE.md"), "# harness")
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	wc := filepath.Join(meta, "feature")
	mustMkdir(t, wc)
	// Wire a real harness symlink so looksLikeWorkingCopy passes.
	if _, err := project.LinkHarnessIntoWorkingCopy(meta, wc); err != nil {
		t.Fatalf("LinkHarnessIntoWorkingCopy: %v", err)
	}

	withCwd(t, meta, func() {
		if err := runRm(nopReporter{}, "feature", true); err != nil {
			t.Fatalf("runRm: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(meta, "feature")); err == nil {
		t.Fatalf("working copy still exists after rm")
	}
}
