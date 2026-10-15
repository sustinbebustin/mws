package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// envFixture is a meta workspace without git: a harness, env staging, one
// default repo "frontend" with two env mappings, one optional repo "extra"
// with one, and two working copies "a" and "b" holding frontend only.
type envFixture struct {
	meta string
}

func (f envFixture) copy(name string) string { return filepath.Join(f.meta, name) }

func (f envFixture) staged(repo, source string) string {
	return filepath.Join(f.meta, project.EnvStagingDirName, repo, source)
}

func seedEnvWorkspace(t *testing.T) envFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := envFixture{meta: filepath.Join(root, "demo")}
	mustMkdir(t, filepath.Join(f.meta, project.HarnessDirName))
	cfg := &config.Config{
		ProjectName: "demo",
		Repos: []config.Repo{{
			Folder: "frontend",
			URL:    "git@example.com:demo/frontend.git",
			Envs: []config.EnvMapping{
				{Source: "apps-internal.env", Target: "apps/internal/.env"},
				{Source: "root.env", Target: ".env"},
			},
		}},
		OptionalRepos: []config.Repo{{
			Folder: "extra",
			URL:    "git@example.com:demo/extra.git",
			Envs:   []config.EnvMapping{{Source: "extra.env", Target: ".env"}},
		}},
	}
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"a", "b"} {
		mustMkdir(t, filepath.Join(f.copy(c), "frontend"))
	}
	return f
}

func TestOldEnvCommandsAreUnknown(t *testing.T) {
	f := seedEnvWorkspace(t)
	for _, cmd := range []string{"sync-env", "stage-env"} {
		assertUsage(t, runMWS(t, f.copy("a"), cmd), "unknown command")
	}
}

func TestEnvSyncCopiesStagedFilesIntoCopy(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "apps-internal.env"), "INTERNAL=1\n")
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "STALE\n")

	res := runMWS(t, f.meta, "env", "sync", "a")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", ".env")); got != "ROOT=1\n" {
		t.Errorf("frontend/.env = %q, want the staged bytes", got)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env")); got != "INTERNAL=1\n" {
		t.Errorf("frontend/apps/internal/.env = %q, want the staged bytes", got)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want nothing (progress goes to stderr)", res.stdout)
	}
}

func TestEnvSyncDefaultsToCurrentCopyAndWarnsOnMissingSource(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")

	res := runMWS(t, filepath.Join(f.copy("b"), "frontend"), "env", "sync")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.copy("b"), "frontend", ".env")); got != "ROOT=1\n" {
		t.Errorf("b/frontend/.env = %q, want the staged bytes", got)
	}
	if !strings.Contains(res.stderr, "[WARN] .envs/frontend/apps-internal.env missing, skipping") {
		t.Errorf("stderr does not warn about the missing source:\n%s", res.stderr)
	}
	if fileExists(filepath.Join(f.copy("a"), "frontend", ".env")) {
		t.Error("sync of b wrote into a")
	}
}

func TestEnvSyncAllCoversOptionalReposPresentInEachCopy(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	writeFileAll(t, f.staged("extra", "extra.env"), "EXTRA=1\n")
	mustMkdir(t, filepath.Join(f.copy("a"), "extra"))

	res := runMWS(t, f.meta, "env", "sync", "--all")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	for _, c := range []string{"a", "b"} {
		if got := readFile(t, filepath.Join(f.copy(c), "frontend", ".env")); got != "ROOT=1\n" {
			t.Errorf("%s/frontend/.env = %q, want the staged bytes", c, got)
		}
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "extra", ".env")); got != "EXTRA=1\n" {
		t.Errorf("a/extra/.env = %q, want the staged bytes", got)
	}
	if fileExists(filepath.Join(f.copy("b"), "extra")) {
		t.Error("sync created the optional repo extra in b, which does not hold it")
	}
}

func TestEnvSyncAllWithACopyNameIsUsageError(t *testing.T) {
	f := seedEnvWorkspace(t)
	assertUsage(t, runMWS(t, f.meta, "env", "sync", "a", "--all"), "--all")
}

func TestEnvSyncFromMetaRootWithoutCopyIsUsageError(t *testing.T) {
	f := seedEnvWorkspace(t)
	assertUsage(t, runMWS(t, f.meta, "env", "sync"), "mws env sync <copy>")
}

func TestEnvSyncDryRunSaysWouldCopyAndChangesNothing(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "LOCAL\n")

	got := wouldLines(t, runMWS(t, f.meta, "env", "sync", "a", "--dry-run"))
	if want := "would copy .envs/frontend/root.env -> a/frontend/.env"; len(got) != 1 || got[0] != want {
		t.Fatalf("lines = %q, want one %q", got, want)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", ".env")); got != "LOCAL\n" {
		t.Errorf("dry run changed a/frontend/.env to %q", got)
	}
}

// envRunJSON is the shape of `mws env sync|stage --json`.
type envRunJSON struct {
	DryRun bool `json:"dry_run"`
	Copies []struct {
		Copy  string `json:"copy"`
		Path  string `json:"path"`
		Files []struct {
			Repo   string `json:"repo"`
			From   string `json:"from"`
			To     string `json:"to"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"files"`
	} `json:"copies"`
}

func TestEnvSyncJSONReportsEachFile(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")

	res := runMWS(t, f.meta, "env", "--json", "sync", "a", "--dry-run")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	var out envRunJSON
	decodeJSON(t, res, &out)
	if !out.DryRun || len(out.Copies) != 1 || out.Copies[0].Copy != "a" || out.Copies[0].Path != f.copy("a") {
		t.Fatalf("result = %+v, want one dry-run copy a", out)
	}
	statuses := map[string]string{}
	for _, file := range out.Copies[0].Files {
		statuses[file.To] = file.Status
	}
	want := map[string]string{
		filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env"): "skipped",
		filepath.Join(f.copy("a"), "frontend", ".env"):                     "would_copy",
	}
	if !reflect.DeepEqual(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}
}

func TestEnvSyncExitsOneAfterAFailedFile(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "apps-internal.env"), "INTERNAL=1\n")
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	// A directory where the internal target belongs makes that copy fail.
	mustMkdir(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env", "x"))

	res := runMWS(t, f.meta, "env", "sync", "a")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", ".env")); got != "ROOT=1\n" {
		t.Errorf("the file after the failure was not copied: %q", got)
	}
	if !strings.Contains(res.stderr, "1 env file failed to copy") {
		t.Errorf("stderr does not count the failure:\n%s", res.stderr)
	}
}

func TestEnvStageCapturesCopyFilesIntoStaging(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "STALE=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "LIVE_ROOT=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "extra", ".env"), "LIVE_EXTRA=1\n")

	res := runMWS(t, f.copy("a"), "env", "stage")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, f.staged("frontend", "root.env")); got != "LIVE_ROOT=1\n" {
		t.Errorf("staged root.env = %q, want the copy's bytes", got)
	}
	if got := readFile(t, f.staged("extra", "extra.env")); got != "LIVE_EXTRA=1\n" {
		t.Errorf("staged extra.env = %q, want the optional repo's bytes", got)
	}
	if !strings.Contains(res.stderr, "[WARN] a/frontend/apps/internal/.env missing, skipping") {
		t.Errorf("stderr does not warn about the missing copy file:\n%s", res.stderr)
	}
}

func TestEnvStageDryRunSaysWouldCopyAndChangesNothing(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "LIVE_ROOT=1\n")

	got := wouldLines(t, runMWS(t, f.meta, "env", "stage", "a", "--dry-run"))
	if want := "would copy a/frontend/.env -> .envs/frontend/root.env"; len(got) != 1 || got[0] != want {
		t.Fatalf("lines = %q, want one %q", got, want)
	}
	if fileExists(f.staged("frontend", "root.env")) {
		t.Error("dry run wrote into staging")
	}
}

func TestEnvStageRefusesInsideAWorktree(t *testing.T) {
	f := seedEnvWorkspace(t)
	wt := filepath.Join(f.copy("a"), ".wt", "x")
	writeFileAll(t, filepath.Join(wt, project.WorktreeMarkerFileName), "")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "LIVE_ROOT=1\n")

	res := runMWS(t, wt, "env", "stage")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "cd "+f.copy("a")+" && mws env stage") {
		t.Errorf("stderr does not name the command to run instead:\n%s", res.stderr)
	}
	if fileExists(f.staged("frontend", "root.env")) {
		t.Error("refused stage wrote into staging")
	}
}

func TestEnvStageExitsOneAfterAFailedFile(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env"), "LIVE_INTERNAL=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "LIVE_ROOT=1\n")
	// A directory where the staged file belongs makes that copy fail.
	mustMkdir(t, filepath.Join(f.staged("frontend", "apps-internal.env"), "x"))

	res := runMWS(t, f.meta, "env", "stage", "a")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, f.staged("frontend", "root.env")); got != "LIVE_ROOT=1\n" {
		t.Errorf("the file after the failure was not staged: %q", got)
	}
}

func TestEnvDiffReportsEachStateAndExitsOne(t *testing.T) {
	f := seedEnvWorkspace(t)
	// frontend: apps-internal differs, root is the same.
	writeFileAll(t, f.staged("frontend", "apps-internal.env"), "INTERNAL=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env"), "INTERNAL=2\n")
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "ROOT=1\n")
	// extra is present in a: its file is missing in staging.
	writeFileAll(t, filepath.Join(f.copy("a"), "extra", ".env"), "EXTRA=1\n")
	// b holds frontend with no env files: both are missing in the copy.

	res := runMWS(t, f.meta, "env", "diff", "--all")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	want := "differs            a/frontend/apps/internal/.env <- .envs/frontend/apps-internal.env\n" +
		"same               a/frontend/.env <- .envs/frontend/root.env\n" +
		"missing in staging a/extra/.env <- .envs/extra/extra.env\n" +
		"missing in copy    b/frontend/apps/internal/.env <- .envs/frontend/apps-internal.env\n" +
		"missing in copy    b/frontend/.env <- .envs/frontend/root.env\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", res.stdout, want)
	}
}

func TestEnvDiffExitsZeroWhenEverythingIsTheSame(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "apps-internal.env"), "INTERNAL=1\n")
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	if res := runMWS(t, f.meta, "env", "sync", "a"); res.code != 0 {
		t.Fatalf("sync: exit %d\nstderr: %s", res.code, res.stderr)
	}

	res := runMWS(t, f.copy("a"), "env", "--json", "diff")
	if res.code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", res.code, res.stdout, res.stderr)
	}
	var out struct {
		Same   bool `json:"same"`
		Copies []struct {
			Copy  string `json:"copy"`
			Files []struct {
				Repo   string `json:"repo"`
				Source string `json:"source"`
				Target string `json:"target"`
				State  string `json:"state"`
			} `json:"files"`
		} `json:"copies"`
	}
	decodeJSON(t, res, &out)
	if !out.Same || len(out.Copies) != 1 || out.Copies[0].Copy != "a" || len(out.Copies[0].Files) != 2 {
		t.Fatalf("result = %+v, want copy a with two same files", out)
	}
	for _, file := range out.Copies[0].Files {
		if file.State != "same" || file.Repo != "frontend" {
			t.Errorf("file = %+v, want state same in frontend", file)
		}
	}
}

func TestEnvDiffJSONNamesMissingStates(t *testing.T) {
	f := seedEnvWorkspace(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "ROOT=1\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env"), "INTERNAL=1\n")

	res := runMWS(t, f.meta, "env", "diff", "a", "--json")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	var out struct {
		Same   bool `json:"same"`
		Copies []struct {
			Files []struct {
				State string `json:"state"`
			} `json:"files"`
		} `json:"copies"`
	}
	decodeJSON(t, res, &out)
	var states []string
	for _, file := range out.Copies[0].Files {
		states = append(states, file.State)
	}
	if out.Same || !reflect.DeepEqual(states, []string{"missing_in_staging", "missing_in_copy"}) {
		t.Errorf("same = %v, states = %q", out.Same, states)
	}
}

// The lifecycle commands materialise env files through the render path, so
// they report each file the way `env sync` does.

func TestCloneRendersEnvFiles(t *testing.T) {
	f := seedWtWorkspace(t)
	res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup")
	if res.code != 0 {
		t.Fatalf("clone: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.meta, "d", "frontend", "app.env")); got != "STAGED=1\n" {
		t.Errorf("d/frontend/app.env = %q, want the staged bytes", got)
	}
	if !strings.Contains(res.stderr, "[OK] .envs/frontend/app.env -> d/frontend/app.env") {
		t.Errorf("stderr does not show the render path's line:\n%s", res.stderr)
	}
}

func TestIncludeRendersEnvFiles(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.OptionalRepos[0].Envs = []config.EnvMapping{{Source: "extra.env", Target: ".env"}}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "extra", "extra.env"), "EXTRA=1\n")

	res := runMWS(t, f.copy, "include", "extra", "--no-setup")
	if res.code != 0 {
		t.Fatalf("include: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.repo("extra"), ".env")); got != "EXTRA=1\n" {
		t.Errorf("c/extra/.env = %q, want the staged bytes", got)
	}
	if !strings.Contains(res.stderr, "[OK] .envs/extra/extra.env -> c/extra/.env") {
		t.Errorf("stderr does not show the render path's line:\n%s", res.stderr)
	}
}

func TestRestoreRendersEnvFiles(t *testing.T) {
	f := seedWtWorkspace(t)
	if res := runMWS(t, f.meta, "rm", "c", "--yes"); res.code != 0 {
		t.Fatalf("rm: exit %d\nstderr: %s", res.code, res.stderr)
	}
	res := runMWS(t, f.meta, "restore", "c", "--yes")
	if res.code != 0 {
		t.Fatalf("restore: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.repo("frontend"), "app.env")); got != "STAGED=1\n" {
		t.Errorf("c/frontend/app.env = %q, want the staged bytes", got)
	}
	if !strings.Contains(res.stderr, "[OK] .envs/frontend/app.env -> c/frontend/app.env") {
		t.Errorf("stderr does not show the render path's line:\n%s", res.stderr)
	}
}

// fileExists reports whether p exists, without following a final symlink.
func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
