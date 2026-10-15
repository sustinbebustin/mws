package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
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

func TestEnvSyncWithoutRootSetupNeedsNoFlag(t *testing.T) {
	f := seedEnvWorkspace(t)
	if res := runMWS(t, f.meta, "env", "sync", "a"); res.code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.copy("a"), "setup.out")); err == nil {
		t.Error("setup.out exists without any setup configured")
	}
}
