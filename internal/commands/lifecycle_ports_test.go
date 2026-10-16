package commands

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// withPortTemplate gives the worktree fixture a [ports] table on a free
// base, a root [[setup]] entry that appends $MWS_SLOT to setup.out at the
// copy root, and a staged app.env holding ${PORT}. It returns the base.
func (f wtFixture) withPortTemplate(t *testing.T) int {
	t.Helper()
	f.cfg.Ports = freePortTable(t)
	f.cfg.Setup = []config.SetupCommand{{Cmd: `printf '%s\n' "$MWS_SLOT" >> setup.out`}}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env"), "PORT=${PORT}\n")
	return f.cfg.Ports.Port[0].Base
}

// mustMWS runs mws with args from cwd and fails the test unless it exits 0.
func mustMWS(t *testing.T, cwd string, args ...string) mwsResult {
	t.Helper()
	res := runMWS(t, cwd, args...)
	if res.code != 0 {
		t.Fatalf("mws %v: exit %d\nstdout: %s\nstderr: %s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

func TestClonePortsClaimsTheLowestFreeSlotBeforeTheOneSetupPass(t *testing.T) {
	f := seedWtWorkspace(t)
	base := f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "1", "--no-setup")

	mustMWS(t, f.meta, "clone", "d", "--ports", "--no-optional", "--setup")

	d := filepath.Join(f.meta, "d")
	if got := slotOf(t, d); got != 2 {
		t.Fatalf("d holds slot %d, want the lowest free 2", got)
	}
	if got, want := readFile(t, filepath.Join(d, "frontend", "app.env")), "PORT="+strconv.Itoa(base+20)+"\n"; got != want {
		t.Errorf("d/frontend/app.env = %q, want %q", got, want)
	}
	if got := readFile(t, filepath.Join(d, "setup.out")); got != "2\n" {
		t.Errorf("setup.out = %q, want one run that saw slot 2", got)
	}
}

func TestCloneWithoutPortsWarnsThatTheCopySharesTheDefaults(t *testing.T) {
	f := seedWtWorkspace(t)
	res := mustMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup")
	if strings.Contains(res.stderr, "default ports") {
		t.Errorf("clone without [ports] warned about the default ports:\n%s", res.stderr)
	}

	f.withPortTemplate(t)
	res = mustMWS(t, f.meta, "clone", "e", "--no-optional", "--no-setup")
	if want := "[WARN] e is on the default ports"; !strings.Contains(res.stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
	}
}
