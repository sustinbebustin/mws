package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
)

// noTerminal makes canPrompt report no terminal for the rest of the test, as
// for an agent or a script.
func ptr[T any](v T) *T { return &v }

func noTerminal(t *testing.T) {
	t.Helper()
	prev := canPrompt
	canPrompt = func() bool { return false }
	t.Cleanup(func() { canPrompt = prev })
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"bogus"},
		{"clone", "--bogus"},
		{"clone", "a", "b"},
		{"clone", "a", "--setup", "--no-setup"},
		{"clone", "a", "--with", "x", "--no-optional"},
		{"shell-init"},
		{"wt", "new"},
		{"wt", "bogus"},
		{"wt", "ls", "--bogus"},
		{"wt", "hook", "create", "extra"},
		{"trash", "bogus"},
	} {
		err := Execute(context.Background(), args)
		if got := ExitCode(err); got != 2 {
			t.Errorf("mws %s: exit %d (%v), want 2", strings.Join(args, " "), got, err)
		}
	}
}

func TestHelpAndCompletionStillWork(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"help", "clone"},
		{"wt", "--help"},
		{cobra.ShellCompRequestCmd, "cl"},
	} {
		if err := Execute(context.Background(), args); err != nil {
			t.Errorf("mws %s: %v, want success", strings.Join(args, " "), err)
		}
	}
}

func TestParseRepoFlag(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want config.Repo
	}{
		{"git@github.com:org/api.git", config.Repo{Folder: "api", URL: "git@github.com:org/api.git"}},
		{"https://github.com/org/web", config.Repo{Folder: "web", URL: "https://github.com/org/web"}},
		{"front=https://github.com/org/web", config.Repo{Folder: "front", URL: "https://github.com/org/web"}},
		{"https://host/r.git?a=b", config.Repo{Folder: "r.git", URL: "https://host/r.git?a=b"}},
	} {
		got, err := parseRepoFlag(tc.spec)
		if err != nil {
			t.Errorf("parseRepoFlag(%q): %v", tc.spec, err)
			continue
		}
		if got.Folder != tc.want.Folder || got.URL != tc.want.URL {
			t.Errorf("parseRepoFlag(%q) = %+v, want %+v", tc.spec, got, tc.want)
		}
	}
	for _, bad := range []string{"", "api="} {
		if _, err := parseRepoFlag(bad); ExitCode(err) != 2 {
			t.Errorf("parseRepoFlag(%q) = %v, want a usage error", bad, err)
		}
	}
}

func TestInitWithoutTerminal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	noTerminal(t)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = runInit(context.Background(), nopReporter{}, io.Discard, "demo", initFlags{parent: &parent})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("without --yes: %v (exit %d), want a usage error naming --yes", err, ExitCode(err))
	}
	if _, err := os.Stat(filepath.Join(parent, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init without --yes created the workspace: %v", err)
	}

	for _, tc := range []struct {
		name string
		f    initFlags
	}{
		{"bad --copies-dir", initFlags{parent: &parent, copiesDir: ptr("../x"), yes: true}},
		{"duplicate --repo folders", initFlags{parent: &parent, repos: []string{"api=git@h:o/a.git", "api=git@h:o/b.git"}, yes: true}},
	} {
		if err := runInit(context.Background(), nopReporter{}, io.Discard, "demo", tc.f); ExitCode(err) != 2 {
			t.Fatalf("%s: %v (exit %d), want a usage error", tc.name, err, ExitCode(err))
		}
	}

	// A relative --parent still prints an absolute path.
	var out bytes.Buffer
	rel := "."
	var initErr error
	withCwd(t, parent, func() {
		initErr = runInit(context.Background(), nopReporter{}, &out, "demo", initFlags{parent: &rel, yes: true})
	})
	if err := initErr; err != nil {
		t.Fatalf("init --yes: %v", err)
	}
	mainCopy := filepath.Join(parent, "demo", firstWorkingCopyName)
	if out.String() != mainCopy+"\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), mainCopy+"\n")
	}
}

func TestRmWithoutTerminal(t *testing.T) {
	f := seedWtWorkspace(t)
	noTerminal(t)
	withCwd(t, f.meta, func() {
		if err := runRm(t.Context(), nopReporter{}, "c", rmFlags{}); ExitCode(err) != 2 {
			t.Fatalf("rm without --yes: %v (exit %d), want a usage error", err, ExitCode(err))
		}
		if err := runRm(t.Context(), nopReporter{}, "c", rmFlags{dryRun: true}); err != nil {
			t.Fatalf("rm --dry-run: %v", err)
		}
		if _, err := os.Stat(f.copy); err != nil {
			t.Fatalf("rm --dry-run removed the copy: %v", err)
		}
		if err := runRm(t.Context(), nopReporter{}, "c", rmFlags{yes: true}); err != nil {
			t.Fatalf("rm --yes: %v", err)
		}
		err := runRm(t.Context(), nopReporter{}, "c", rmFlags{yes: true})
		if ExitCode(err) != 4 || !strings.Contains(err.Error(), "previous rm already removed it") {
			t.Fatalf("rm --yes of a copy already in the trash: %v (exit %d), want exit 4 naming the trash entry", err, ExitCode(err))
		}
	})
}

func TestRelinkDivergedFileWithoutTerminal(t *testing.T) {
	meta, peer := setupRelinkTree(t)
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(meta, project.HarnessDirName, ".mcp.json"), "meta")
	mustWriteFile(t, filepath.Join(peer, ".mcp.json"), "peer")
	noTerminal(t)
	withCwd(t, meta, func() {
		if err := runRelink(nopReporter{}, relinkFlags{}); ExitCode(err) != 2 || !strings.Contains(err.Error(), "--prefer") {
			t.Fatalf("relink: %v (exit %d), want a usage error naming --prefer", err, ExitCode(err))
		}
		if err := runRelink(nopReporter{}, relinkFlags{dryRun: true}); err != nil {
			t.Fatalf("relink --dry-run: %v", err)
		}
		if err := runRelink(nopReporter{}, relinkFlags{prefer: resolveMeta}); err != nil {
			t.Fatalf("relink --prefer meta: %v", err)
		}
	})
	if st, err := os.Lstat(filepath.Join(peer, ".mcp.json")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("peer .mcp.json should be a harness symlink after --prefer meta: %v", err)
	}
}

func TestRelinkRefusesBeforeChangingAnything(t *testing.T) {
	meta, peer := setupRelinkTree(t)
	other := filepath.Join(meta, "other")
	mustMkdir(t, other)
	if err := config.Save(meta, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(meta, project.HarnessDirName)
	mustWriteFile(t, filepath.Join(harness, "notes.txt"), "harness")
	mustWriteFile(t, filepath.Join(harness, "same.txt"), "same")
	mustWriteFile(t, filepath.Join(peer, "same.txt"), "same")
	mustWriteFile(t, filepath.Join(peer, "notes.txt"), "edit-main")
	mustWriteFile(t, filepath.Join(other, "notes.txt"), "edit-other")
	noTerminal(t)

	for _, tc := range []struct {
		name string
		f    relinkFlags
	}{
		{"no terminal, no --prefer", relinkFlags{}},
		{"--prefer peer with two versions", relinkFlags{prefer: resolvePeer}},
		{"--dry-run --prefer peer", relinkFlags{prefer: resolvePeer, dryRun: true}},
	} {
		withCwd(t, meta, func() {
			if err := runRelink(nopReporter{}, tc.f); ExitCode(err) != 2 {
				t.Fatalf("%s: %v (exit %d), want a usage error", tc.name, err, ExitCode(err))
			}
		})
		for path, want := range map[string]string{
			filepath.Join(harness, "notes.txt"): "harness",
			filepath.Join(peer, "notes.txt"):    "edit-main",
			filepath.Join(other, "notes.txt"):   "edit-other",
			filepath.Join(peer, "same.txt"):     "same",
		} {
			if !isRegularFile(path) {
				t.Fatalf("%s: %s was changed", tc.name, path)
			}
			if got, _ := os.ReadFile(path); string(got) != want {
				t.Fatalf("%s: %s = %q, want %q", tc.name, path, got, want)
			}
		}
	}
}

func TestAddRepoTwiceWithTheSameURLSucceeds(t *testing.T) {
	metaRoot := filepath.Join(t.TempDir(), "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	if err := config.Save(metaRoot, &config.Config{ProjectName: "demo"}); err != nil {
		t.Fatal(err)
	}
	withCwd(t, metaRoot, func() {
		for i := range 2 {
			if err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:example/worker.git", "worker", true); err != nil {
				t.Fatalf("add-repo #%d: %v", i+1, err)
			}
		}
		if err := runAddRepo(context.Background(), nopReporter{}, "git@github.com:other/worker.git", "worker", true); err == nil {
			t.Fatal("add-repo of another URL into the same folder succeeded")
		}
	})
}

func TestTrashEmptyWithoutTerminalNeedsYes(t *testing.T) {
	f := seedWtWorkspace(t)
	withCwd(t, f.meta, func() {
		if err := runRm(t.Context(), nopReporter{}, "c", rmFlags{yes: true}); err != nil {
			t.Fatal(err)
		}
		noTerminal(t)
		if err := runTrashEmpty(t.Context(), nopReporter{}, confirmFlags{}); ExitCode(err) != 2 {
			t.Fatalf("trash empty: %v (exit %d), want a usage error", err, ExitCode(err))
		}
		if err := runTrashEmpty(t.Context(), nopReporter{}, confirmFlags{dryRun: true}); err != nil {
			t.Fatalf("trash empty --dry-run: %v", err)
		}
		if entries, _, _ := trash.List(f.meta); len(entries) != 1 {
			t.Fatalf("trash holds %d entries after the refused and dry runs, want 1", len(entries))
		}
	})
}
