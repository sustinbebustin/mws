package commands

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestRunClonePlacesPeerUnderWorkingCopiesDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))

	// Stand up a bare upstream repo with one commit so `git clone <url>` succeeds.
	upstream := filepath.Join(root, "upstream-frontend.git")
	if err := exec.Command("git", "init", "-q", "--bare", upstream).Run(); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}
	seed := filepath.Join(root, "seed")
	mustMkdir(t, seed)
	for _, args := range [][]string{
		{"-C", seed, "init", "-q"},
		{"-C", seed, "config", "user.email", "t@e.com"},
		{"-C", seed, "config", "user.name", "t"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(seed, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", seed, "add", "x"},
		{"-C", seed, "commit", "-q", "-m", "init"},
		{"-C", seed, "push", "-q", upstream, "HEAD:refs/heads/main"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	// Point the bare upstream's HEAD at the branch we actually pushed. git init
	// seeds HEAD from the host's init.defaultBranch (often "master" in CI), which
	// need not match "main" -- leaving origin/HEAD unresolvable after clone and
	// making checkoutDefault warn. Real remotes always have a valid HEAD.
	if err := exec.Command("git", "-C", upstream, "symbolic-ref", "HEAD", "refs/heads/main").Run(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v", err)
	}

	cfg := &config.Config{
		ProjectName:      "demo",
		WorkingCopiesDir: "copies",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    upstream,
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), nopReporter{}, "peer", setupSkip, nil); err != nil {
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
	if got := strings.TrimSpace(string(out)); got != upstream {
		t.Fatalf("peer origin: got %q want %q", got, upstream)
	}
}

// recordingReporter captures Fail, Info, and Warn messages so tests can assert
// on per-repo error wording, post-clone hint lines, and best-effort handoff
// warnings -- runClone only surfaces those through the reporter (the returned
// error just lists folder names; the cd hint and handoff warn are fire-and-forget UI).
type recordingReporter struct {
	nopReporter
	fails []string
	infos []string
	warns []string
}

func (r *recordingReporter) Fail(msg string) { r.fails = append(r.fails, msg) }
func (r *recordingReporter) Info(msg string) { r.infos = append(r.infos, msg) }
func (r *recordingReporter) Warn(msg string) { r.warns = append(r.warns, msg) }

// unsetEnv removes key for the duration of the test, restoring whatever value
// (or absence) was present beforehand. testing.T has no Unsetenv counterpart to
// t.Setenv, so we shim one. Lets tests distinguish "var was set to empty" from
// "var was never set" -- the runtime treats both identically today, but
// MWS_CD_FILE is an ADR-0008 public contract worth testing both branches of.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	if prev, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { _ = os.Setenv(key, prev) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
}

func TestRunCloneFailsWhenRepoURLMissing(t *testing.T) {
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
			URL:    "",
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	rep := &recordingReporter{}
	withCwd(t, metaRoot, func() {
		err := runClone(context.Background(), rep, "peer", setupSkip, nil)
		if err == nil {
			t.Fatalf("expected error when repo URL is empty")
		}
	})

	const wantMsg = "frontend: no remote URL configured in .mws.toml"
	if len(rep.fails) != 1 || rep.fails[0] != wantMsg {
		t.Fatalf("Fail messages: got %v, want [%q]", rep.fails, wantMsg)
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
		err := runClone(context.Background(), nopReporter{}, "peer", setupForceRun, nil)
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

// seedWorkingRepo stands up a minimal meta workspace + bare upstream repo
// suitable for driving runClone end-to-end. Returns the meta root.
func seedWorkingRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	// Canonicalise the meta root: runClone derives the emitted target path from
	// os.Getwd(), which returns the symlink-resolved cwd (macOS maps /var ->
	// /private/var). t.TempDir() hands back the unresolved path, so tests that
	// compare against the emitted path must resolve too.
	metaRoot, err := filepath.EvalSymlinks(metaRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(meta root): %v", err)
	}

	upstream := seedBareUpstream(t, root, "upstream-frontend")
	cfg := &config.Config{
		ProjectName: "demo",
		Repos:       []config.Repo{{Folder: "frontend", URL: upstream}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	return metaRoot
}

// seedBareUpstream stands up a bare git repo named <name>.git under dir with a
// single commit pushed to refs/heads/main and origin/HEAD pointed at it.
// Returns the bare repo path, usable as a config.Repo URL for cloning.
func seedBareUpstream(t *testing.T, dir, name string) string {
	t.Helper()
	upstream := filepath.Join(dir, name+".git")
	if err := exec.Command("git", "init", "-q", "--bare", upstream).Run(); err != nil {
		t.Fatalf("git init --bare %s: %v", name, err)
	}
	seed := filepath.Join(dir, "seed-"+name)
	mustMkdir(t, seed)
	for _, args := range [][]string{
		{"-C", seed, "init", "-q"},
		{"-C", seed, "config", "user.email", "t@e.com"},
		{"-C", seed, "config", "user.name", "t"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(seed, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", seed, "add", "x"},
		{"-C", seed, "commit", "-q", "-m", "init"},
		{"-C", seed, "push", "-q", upstream, "HEAD:refs/heads/main"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	// Point the bare upstream's HEAD at the branch we actually pushed. git init
	// seeds HEAD from the host's init.defaultBranch (often "master" in CI), which
	// need not match "main" -- leaving origin/HEAD unresolvable after clone and
	// making checkoutDefault warn. Real remotes always have a valid HEAD.
	if err := exec.Command("git", "-C", upstream, "symbolic-ref", "HEAD", "refs/heads/main").Run(); err != nil {
		t.Fatalf("git symbolic-ref HEAD: %v", err)
	}
	return upstream
}

func TestRunCloneWritesMWSCDFileWhenSet(t *testing.T) {
	metaRoot := seedWorkingRepo(t)
	cdFile := filepath.Join(t.TempDir(), "mws-cd")
	t.Setenv("MWS_CD_FILE", cdFile)

	rep := &recordingReporter{}
	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), rep, "peer", setupSkip, nil); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	got, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("MWS_CD_FILE not written: %v", err)
	}
	want := filepath.Join(metaRoot, "peer")
	if string(got) != want {
		t.Fatalf("MWS_CD_FILE contents: got %q want %q", got, want)
	}
	// ADR 0008 wire format: absolute path, single line, no trailing newline, no
	// CR/NUL bytes. Third parties consume MWS_CD_FILE directly via `cat`/read --
	// any drift here is a breaking change.
	if strings.HasSuffix(string(got), "\n") {
		t.Errorf("MWS_CD_FILE has trailing newline; ADR 0008 specifies no terminator")
	}
	if strings.ContainsAny(string(got), "\r\x00") {
		t.Errorf("MWS_CD_FILE contains CR or NUL; expected single-line UTF-8 path")
	}
	for _, msg := range rep.infos {
		if strings.HasPrefix(msg, "Next: cd ") {
			t.Fatalf("hint should be suppressed when MWS_CD_FILE is set, got info %q", msg)
		}
	}
	if len(rep.warns) != 0 {
		t.Fatalf("unexpected warns on happy path: %v", rep.warns)
	}
}

func TestRunClonePrintsCDHintWhenMWSCDFileEmpty(t *testing.T) {
	metaRoot := seedWorkingRepo(t)
	t.Setenv("MWS_CD_FILE", "") // empty-string -- distinguished from genuinely unset (see sibling test)

	rep := &recordingReporter{}
	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), rep, "peer", setupSkip, nil); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	wantHint := "Next: cd " + filepath.Join(metaRoot, "peer")
	if !slices.Contains(rep.infos, wantHint) {
		t.Fatalf("expected info hint %q in %v", wantHint, rep.infos)
	}
}

func TestRunClonePrintsCDHintWhenMWSCDFileUnset(t *testing.T) {
	metaRoot := seedWorkingRepo(t)
	unsetEnv(t, "MWS_CD_FILE")

	rep := &recordingReporter{}
	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), rep, "peer", setupSkip, nil); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})

	wantHint := "Next: cd " + filepath.Join(metaRoot, "peer")
	if !slices.Contains(rep.infos, wantHint) {
		t.Fatalf("expected info hint %q in %v", wantHint, rep.infos)
	}
}

func TestRunCloneWarnsWhenMWSCDFileUnwritable(t *testing.T) {
	metaRoot := seedWorkingRepo(t)
	// Point at a path whose parent directory does not exist -- WriteFile will
	// fail with ENOENT. The clone itself must still succeed (handoff is
	// best-effort) and the warn must surface so the user sees why no auto-cd.
	cdFile := filepath.Join(t.TempDir(), "nope", "mws-cd")
	t.Setenv("MWS_CD_FILE", cdFile)

	rep := &recordingReporter{}
	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), rep, "peer", setupSkip, nil); err != nil {
			t.Fatalf("runClone should succeed despite handoff failure: %v", err)
		}
	})

	if _, err := os.Stat(cdFile); err == nil {
		t.Fatalf("MWS_CD_FILE was unexpectedly created at %s", cdFile)
	}
	var found bool
	for _, msg := range rep.warns {
		if strings.Contains(msg, "could not write MWS_CD_FILE") && strings.Contains(msg, cdFile) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warn citing MWS_CD_FILE path %q in %v", cdFile, rep.warns)
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

	got := collectSetup(cfg.Repos)
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

func TestResolveOptional(t *testing.T) {
	// No optional repos registered: returns nil without opening a prompt.
	got, err := resolveOptional(&config.Config{}, nil)
	if err != nil || got != nil {
		t.Fatalf("no optional repos: got %v err %v", got, err)
	}

	cfg := &config.Config{OptionalRepos: []config.Repo{
		{Folder: "worker", URL: "u"},
		{Folder: "lib", URL: "v"},
	}}

	// --with selects the named repos non-interactively (no prompt).
	got, err = resolveOptional(cfg, []string{"worker"})
	if err != nil {
		t.Fatalf("--with worker: %v", err)
	}
	if len(got) != 1 || got[0].Folder != "worker" || got[0].URL != "u" {
		t.Fatalf("--with worker: got %+v", got)
	}

	// Unknown --with name is rejected, listing the registered folders.
	_, err = resolveOptional(cfg, []string{"nope"})
	if err == nil {
		t.Fatal("expected error for unknown --with folder")
	}
	if !strings.Contains(err.Error(), "worker") || !strings.Contains(err.Error(), "lib") {
		t.Fatalf("error should list registered optional repos: %v", err)
	}
}

// seedMetaWithOptional builds a meta workspace with one default repo (frontend)
// and one optional repo (worker), each backed by its own bare upstream.
func seedMetaWithOptional(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	metaRoot, err := filepath.EvalSymlinks(metaRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(meta root): %v", err)
	}
	cfg := &config.Config{
		ProjectName:   "demo",
		Repos:         []config.Repo{{Folder: "frontend", URL: seedBareUpstream(t, root, "upstream-frontend")}},
		OptionalRepos: []config.Repo{{Folder: "worker", URL: seedBareUpstream(t, root, "upstream-worker")}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	return metaRoot
}

func TestRunCloneWithIncludesOptionalRepo(t *testing.T) {
	metaRoot := seedMetaWithOptional(t)
	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), nopReporter{}, "peer", setupSkip, []string{"worker"}); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})
	for _, folder := range []string{"frontend", "worker"} {
		if _, err := os.Stat(filepath.Join(metaRoot, "peer", folder)); err != nil {
			t.Fatalf("expected peer/%s to be cloned: %v", folder, err)
		}
	}
}

func TestRunCloneWithUnknownOptionalFailsBeforeMkdir(t *testing.T) {
	metaRoot := seedMetaWithOptional(t)
	var err error
	withCwd(t, metaRoot, func() {
		err = runClone(context.Background(), nopReporter{}, "peer", setupSkip, []string{"nope"})
	})
	if err == nil {
		t.Fatal("expected error for unknown --with folder")
	}
	if !strings.Contains(err.Error(), "no optional repo registered") {
		t.Fatalf("error wording: %v", err)
	}
	// The invalid selection must fail before any working copy is created.
	if _, statErr := os.Stat(filepath.Join(metaRoot, "peer")); statErr == nil {
		t.Fatal("peer dir should not exist after an invalid --with")
	}
}

func TestRunCloneWithRunsOptionalSetup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	root := t.TempDir()
	metaRoot := filepath.Join(root, "demo")
	mustMkdir(t, filepath.Join(metaRoot, ".mws"))
	metaRoot, err := filepath.EvalSymlinks(metaRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(meta root): %v", err)
	}
	cfg := &config.Config{
		ProjectName: "demo",
		OptionalRepos: []config.Repo{{
			Folder: "worker",
			URL:    seedBareUpstream(t, root, "upstream-worker"),
			Setup:  []config.SetupCommand{{Cmd: "touch ran-setup"}},
		}},
	}
	if err := config.Save(metaRoot, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	withCwd(t, metaRoot, func() {
		if err := runClone(context.Background(), nopReporter{}, "peer", setupForceRun, []string{"worker"}); err != nil {
			t.Fatalf("runClone: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(metaRoot, "peer", "worker", "ran-setup")); err != nil {
		t.Fatalf("selected optional repo's setup did not run: %v", err)
	}
}
