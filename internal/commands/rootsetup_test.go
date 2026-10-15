package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
)

// withRootSetup gives the env fixture one root [[setup]] entry that records
// its cwd and MWS_SLOT in setup.out at its cwd.
func (f envFixture) withRootSetup(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Setup = []config.SetupCommand{{Cmd: `printf '%s %s\n' "$(pwd)" "$MWS_SLOT" >> setup.out`}}
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestEnvSyncHeadlessWithRootSetupNeedsASetupFlag(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withRootSetup(t)

	res := runMWS(t, f.meta, "env", "sync", "a")
	if res.code != 2 {
		t.Fatalf("exit %d, want 2\nstderr: %s", res.code, res.stderr)
	}
	for _, want := range []string{"mws env sync a --setup", "mws env sync a --no-setup"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, res.stderr)
		}
	}
	if fileExists(filepath.Join(f.copy("a"), "setup.out")) {
		t.Error("root setup ran without a flag")
	}
}

func TestEnvSyncSetupRunsRootSetupAtTheCopyRootWithTheLease(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withPorts(t)
	f.withRootSetup(t)
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := ports.WriteLease(f.copy("a"), ports.NewExports(cfg, f.meta, "a", 3)); err != nil {
		t.Fatal(err)
	}

	if res := runMWS(t, f.meta, "env", "sync", "a", "--setup"); res.code != 0 {
		t.Fatalf("--setup: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got, want := readFile(t, filepath.Join(f.copy("a"), "setup.out")), f.copy("a")+" 3\n"; got != want {
		t.Errorf("setup.out = %q, want %q", got, want)
	}

	if res := runMWS(t, f.meta, "env", "sync", "a", "--no-setup"); res.code != 0 {
		t.Fatalf("--no-setup: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "setup.out")); strings.Count(got, "\n") != 1 {
		t.Errorf("--no-setup ran root setup: setup.out = %q", got)
	}
}

func TestCloneSetupRunsRepoSetupThenRootSetupOnceEach(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Repos[1].Setup = []config.SetupCommand{{Cmd: "echo repo >> ../setup.log"}}
	f.cfg.Setup = []config.SetupCommand{{Cmd: "echo root >> setup.log"}}
	f.saveConfig(t)

	res := runMWS(t, f.meta, "clone", "d", "--no-optional")
	if res.code != 2 || !strings.Contains(res.stderr, "mws clone d --setup") || !strings.Contains(res.stderr, "mws clone d --no-setup") {
		t.Fatalf("headless clone without a setup flag: exit %d, want 2 naming both flags\nstderr: %s", res.code, res.stderr)
	}

	if res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--setup"); res.code != 0 {
		t.Fatalf("clone: exit %d\nstderr: %s", res.code, res.stderr)
	}
	log := filepath.Join(f.meta, "d", "setup.log")
	if got, want := readFile(t, log), "repo\nroot\n"; got != want {
		t.Errorf("setup.log = %q, want %q", got, want)
	}

	// A retry that clones nothing renders nothing and runs no setup.
	if res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--setup"); res.code != 0 {
		t.Fatalf("retry: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got, want := readFile(t, log), "repo\nroot\n"; got != want {
		t.Errorf("after a retry that cloned nothing, setup.log = %q, want %q", got, want)
	}
}

func TestIncludeSetupRunsRepoSetupThenRootSetup(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.OptionalRepos[0].Setup = []config.SetupCommand{{Cmd: "echo repo >> ../setup.log"}}
	f.cfg.Setup = []config.SetupCommand{{Cmd: "echo root >> setup.log"}}
	f.saveConfig(t)

	res := runMWS(t, f.meta, "include", "extra", "c")
	if res.code != 2 || !strings.Contains(res.stderr, "--setup") || !strings.Contains(res.stderr, "--no-setup") {
		t.Fatalf("headless include without a setup flag: exit %d, want 2 naming both flags\nstderr: %s", res.code, res.stderr)
	}
	if res := runMWS(t, f.meta, "include", "extra", "c", "--setup"); res.code != 0 {
		t.Fatalf("include: exit %d\nstderr: %s", res.code, res.stderr)
	}
	log := filepath.Join(f.copy, "setup.log")
	if got, want := readFile(t, log), "repo\nroot\n"; got != want {
		t.Errorf("setup.log = %q, want %q", got, want)
	}

	// Including a repo the copy already holds clones nothing: the render
	// still runs root setup, never the repo's.
	if res := runMWS(t, f.meta, "include", "extra", "c", "--setup"); res.code != 0 {
		t.Fatalf("second include: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got, want := readFile(t, log), "repo\nroot\nroot\n"; got != want {
		t.Errorf("after including a present repo, setup.log = %q, want %q", got, want)
	}
}

// seedRenderedWorktree gives the worktree fixture a web port, a placeholder
// env source and a [[renders]] target, renders copy c at slot 0, creates
// worktree x from it, then moves c to slot 2 the way a claim would.
func seedRenderedWorktree(t *testing.T) wtFixture {
	t.Helper()
	f := seedWtWorkspace(t)
	f.cfg.Ports = &config.Ports{Port: []config.Port{{Name: "web", Base: 3000, Var: "PORT"}}}
	f.cfg.Renders = []config.Render{{Source: ".mcp.json.render", Target: ".mcp.json"}}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env"), "PORT=${PORT}\n")
	writeFileAll(t, filepath.Join(f.meta, project.HarnessDirName, ".mcp.json.render"), `{"port":${PORT}}`)
	if res := runMWS(t, f.meta, "env", "sync", "c"); res.code != 0 {
		t.Fatalf("sync at slot 0: exit %d\nstderr: %s", res.code, res.stderr)
	}
	mustNew(t, f.copy, "x", "--no-setup")
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := ports.WriteLease(f.copy, ports.NewExports(cfg, f.meta, "c", 2)); err != nil {
		t.Fatal(err)
	}
	return f
}

// markerHashes reads the carried-file hashes of the worktree at root.
func markerHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	var m struct {
		Copied map[string]string `toml:"copied"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, project.WorktreeMarkerFileName), &m); err != nil {
		t.Fatal(err)
	}
	return m.Copied
}

func sha256Hex(t *testing.T, p string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(readFile(t, p)))
	return hex.EncodeToString(sum[:])
}

func TestEnvSyncReRendersPristineWorktreeFilesAndTheirHashes(t *testing.T) {
	f := seedRenderedWorktree(t)
	wt := f.wtPath("x")

	if res := runMWS(t, f.meta, "env", "sync", "c"); res.code != 0 {
		t.Fatalf("sync: exit %d\nstderr: %s", res.code, res.stderr)
	}
	want := map[string]string{
		"frontend/app.env": "PORT=3020\n",
		".mcp.json":        `{"port":3020}`,
		ports.LeaseFile:    readFile(t, ports.LeasePath(f.copy)),
	}
	hashes := markerHashes(t, wt)
	for rel, body := range want {
		p := filepath.Join(wt, filepath.FromSlash(rel))
		if got := readFile(t, p); got != body {
			t.Errorf("worktree %s = %q, want %q", rel, got, body)
		}
		if got, want := hashes[rel], sha256Hex(t, p); got != want {
			t.Errorf("marker hash of %s = %q, want the new content's %q", rel, got, want)
		}
	}

	if res := runWt(t, f.copy, "rm", "x"); res.code != 0 {
		t.Fatalf("wt rm after a re-render: exit %d\nstderr: %s", res.code, res.stderr)
	}
}

func TestEnvSyncLeavesAnEditedWorktreeFileAndWarns(t *testing.T) {
	f := seedRenderedWorktree(t)
	wt := f.wtPath("x")
	edited := filepath.Join(wt, "frontend", "app.env")
	writeFileAll(t, edited, "PORT=9999\n")

	res := runMWS(t, f.meta, "env", "sync", "c")
	if res.code != 0 {
		t.Fatalf("sync: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, edited); got != "PORT=9999\n" {
		t.Errorf("edited worktree file was overwritten: %q", got)
	}
	warned := false
	for _, line := range strings.Split(res.stderr, "\n") {
		if strings.Contains(line, "[WARN]") && strings.Contains(line, "x") && strings.Contains(line, "frontend/app.env") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no [WARN] naming worktree x and frontend/app.env:\n%s", res.stderr)
	}
	if got := readFile(t, filepath.Join(wt, ".mcp.json")); got != `{"port":3020}` {
		t.Errorf("the other carried file was not re-rendered: %q", got)
	}
}

func TestEnvSyncWithoutRootSetupNeedsNoFlag(t *testing.T) {
	f := seedEnvWorkspace(t)
	if res := runMWS(t, f.meta, "env", "sync", "a"); res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.copy("a"), "setup.out")); err == nil {
		t.Error("setup.out exists without any setup configured")
	}
}
