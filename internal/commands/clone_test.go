package commands

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

func TestConfirmSetupFlagsBypassPrompt(t *testing.T) {
	items := []setupItem{{Folder: "a", Cmd: "true"}}

	run, err := confirmSetup(setupForceRun, items)
	if err != nil {
		t.Fatalf("setupForceRun: unexpected err: %v", err)
	}
	if !run {
		t.Fatalf("setupForceRun: expected run=true")
	}

	run, err = confirmSetup(setupSkip, items)
	if err != nil {
		t.Fatalf("setupSkip: unexpected err: %v", err)
	}
	if run {
		t.Fatalf("setupSkip: expected run=false")
	}

	// Empty items short-circuit even on setupAsk -- no prompt opened.
	run, err = confirmSetup(setupAsk, nil)
	if err != nil {
		t.Fatalf("setupAsk empty: unexpected err: %v", err)
	}
	if run {
		t.Fatalf("setupAsk empty: expected run=false")
	}
}

func TestRunCloneRetargetsOriginToConfigURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))

	const canonicalURL = "git@example.invalid:owner/frontend.git"
	cfg := &config.Config{
		ProjectName: "demo",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    canonicalURL,
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	// Donor working copy with a real git repo whose origin points at a junk path.
	// After mws clone, the new peer's origin must be retargeted to canonicalURL
	// even though --local cloned from this donor.
	donor := filepath.Join(metaRoot, "donor")
	donorRepo := filepath.Join(donor, "frontend")
	mustMkdir(t, donorRepo)
	for _, args := range [][]string{
		{"-C", donorRepo, "init", "-q"},
		{"-C", donorRepo, "config", "user.email", "t@e.com"},
		{"-C", donorRepo, "config", "user.name", "t"},
		{"-C", donorRepo, "remote", "add", "origin", "/some/local/junk/path"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(donorRepo, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", donorRepo, "add", "x"},
		{"-C", donorRepo, "commit", "-q", "-m", "init"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	withCwd(t, donor, func() {
		if err := runClone(context.Background(), nopReporter{}, "peer", setupSkip); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	out, err := exec.Command("git", "-C", filepath.Join(metaRoot, "peer", "frontend"), "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("git remote get-url: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != canonicalURL {
		t.Fatalf("peer origin: got %q, want %q", got, canonicalURL)
	}
}

func TestRunClonePlacesPeerUnderWorkingCopiesDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))

	const canonicalURL = "git@example.invalid:owner/frontend.git"
	cfg := &config.Config{
		ProjectName:      "demo",
		WorkingCopiesDir: "copies",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    canonicalURL,
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	// Donor working copy lives under copies/ to mirror the configured layout.
	donor := filepath.Join(metaRoot, "copies", "donor")
	donorRepo := filepath.Join(donor, "frontend")
	mustMkdir(t, donorRepo)
	for _, args := range [][]string{
		{"-C", donorRepo, "init", "-q"},
		{"-C", donorRepo, "config", "user.email", "t@e.com"},
		{"-C", donorRepo, "config", "user.name", "t"},
		{"-C", donorRepo, "remote", "add", "origin", "/some/local/junk/path"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(donorRepo, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", donorRepo, "add", "x"},
		{"-C", donorRepo, "commit", "-q", "-m", "init"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	withCwd(t, donor, func() {
		if err := runClone(context.Background(), nopReporter{}, "peer", setupSkip); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	peerRepo := filepath.Join(metaRoot, "copies", "peer", "frontend")
	if _, err := os.Stat(peerRepo); err != nil {
		t.Fatalf("peer repo not at copies/peer/frontend: %v", err)
	}
	// Peer must NOT have been created at the meta root next to .mws/.
	if _, err := os.Stat(filepath.Join(metaRoot, "peer")); err == nil {
		t.Fatalf("peer should not exist at meta root when working_copies_dir is set")
	}

	out, err := exec.Command("git", "-C", peerRepo, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("git remote get-url: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != canonicalURL {
		t.Fatalf("peer origin: got %q want %q", got, canonicalURL)
	}
}

func TestRunCloneSkipsSetupWhenClonePhaseFailed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))

	cfg := &config.Config{
		ProjectName: "demo",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    "/does/not/exist-not-a-url",
			Setup: []config.SetupCommand{
				{Cmd: "touch " + filepath.Join(root, "sentinel-should-not-exist")},
			},
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	withCwd(t, metaRoot, func() {
		err := runClone(context.Background(), nopReporter{}, "peer", setupForceRun)
		if err == nil {
			t.Fatalf("expected clone phase to fail")
		}
		if strings.Contains(err.Error(), "setup command") {
			t.Fatalf("error should report clone failure, not setup: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(root, "sentinel-should-not-exist")); err == nil {
		t.Fatalf("setup ran despite clone failure")
	}
}

func TestRunSetupExecutionPolicy(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}

	target := t.TempDir()
	for _, sub := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	items := []setupItem{
		{Folder: "a", Cmd: "true"},
		{Folder: "a", Cmd: "false"},
		{Folder: "a", Cmd: "touch should-not-exist"},
		{Folder: "b", Cmd: "touch b-ran"},
		{Folder: "c", Cmd: "false"},
		{Folder: "c", Cmd: "touch c-after-fail"},
	}

	failed := runSetup(context.Background(), nopReporter{}, target, items, io.Discard, io.Discard)

	wantFailed := []string{"a: false", "c: false"}
	if len(failed) != len(wantFailed) {
		t.Fatalf("failed: got %d entries (%v), want %d (%v)", len(failed), failed, len(wantFailed), wantFailed)
	}
	for i, want := range wantFailed {
		if failed[i] != want {
			t.Fatalf("failed[%d]: got %q want %q", i, failed[i], want)
		}
	}

	// Intra-repo stop-on-fail: nothing after the failing `false` should have run.
	if _, err := os.Stat(filepath.Join(target, "a", "should-not-exist")); err == nil {
		t.Fatalf("a/should-not-exist exists: intra-repo stop-on-fail broken")
	}
	if _, err := os.Stat(filepath.Join(target, "c", "c-after-fail")); err == nil {
		t.Fatalf("c/c-after-fail exists: intra-repo stop-on-fail broken in third repo")
	}
	// Inter-repo continue: b's command ran despite a's failure.
	if _, err := os.Stat(filepath.Join(target, "b", "b-ran")); err != nil {
		t.Fatalf("b/b-ran missing: inter-repo continue broken: %v", err)
	}
}

func TestCollectSetupOrderAndOmits(t *testing.T) {
	cfg := &config.Config{
		Repos: []config.Repo{
			{
				Folder: "a",
				Setup: []config.SetupCommand{
					{Cmd: "cmd1"},
					{Cmd: "cmd2"},
				},
			},
			{Folder: "b"},
			{
				Folder: "c",
				Setup: []config.SetupCommand{
					{Cmd: "  "},   // blank after trim -- skipped
					{Cmd: "cmd3"}, // kept
				},
			},
		},
	}

	got := collectSetup(cfg)
	want := []setupItem{
		{Folder: "a", Cmd: "cmd1"},
		{Folder: "a", Cmd: "cmd2"},
		{Folder: "c", Cmd: "cmd3"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
