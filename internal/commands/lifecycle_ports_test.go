package commands

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/trash"
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

// trashedCopy is the working copy inside the newest trash entry for name.
func trashedCopy(t *testing.T, meta, name string) string {
	t.Helper()
	entries, err := trash.Find(meta, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("no trash entry for %s", name)
	}
	return entries[0].CopyPath
}

func TestRmReleasesTheSlotBeforeTrashingAndRestoreComesBackOnTheDefaults(t *testing.T) {
	f := seedWtWorkspace(t)
	base := f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")

	mustMWS(t, f.meta, "rm", "c", "--yes")

	trashed := trashedCopy(t, f.meta, "c")
	slotZero := "PORT=" + strconv.Itoa(base) + "\n"
	if got := readFile(t, filepath.Join(trashed, "frontend", "app.env")); got != slotZero {
		t.Errorf("trashed app.env = %q, want the slot-0 bytes %q", got, slotZero)
	}
	if fileExists(ports.LeasePath(trashed)) {
		t.Error("the trashed copy still holds its lease")
	}
	if res := mustMWS(t, f.meta, "ports", "ls"); strings.Contains(res.stdout, "slot 2") {
		t.Errorf("ports ls still shows slot 2:\n%s", res.stdout)
	}

	mustMWS(t, f.meta, "restore", "c", "--yes")
	if got := slotOf(t, f.copy); got != 0 {
		t.Errorf("restored c is on slot %d, want the defaults", got)
	}
	if got := readFile(t, filepath.Join(f.repo("frontend"), "app.env")); got != slotZero {
		t.Errorf("restored app.env = %q, want %q", got, slotZero)
	}
}

func TestRmTrashesEvenWhenTheReleaseRenderFails(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")
	if err := os.Remove(filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env")); err != nil {
		t.Fatal(err)
	}

	res := mustMWS(t, f.meta, "rm", "c", "--yes")

	if fileExists(f.copy) {
		t.Error("c was not trashed")
	}
	if want := "[WARN] c: not every env file was rendered at the default ports"; !strings.Contains(res.stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
	}
	if fileExists(ports.LeasePath(trashedCopy(t, f.meta, "c"))) {
		t.Error("the trashed copy still holds its lease")
	}
}

func TestRmPurgeSkipsTheRenderAndDryRunNamesTheSlot(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")

	dry := runMWS(t, f.meta, "rm", "c", "--dry-run")
	if want := []string{"would release slot 2 from c", "would trash " + f.copy}; !slices.Equal(wouldLines(t, dry), want) {
		t.Errorf("dry-run lines = %q, want %q", wouldLines(t, dry), want)
	}
	// The dry run prints the confirmation's description.
	if want := "It first releases slot 2"; !strings.Contains(dry.stderr, want) {
		t.Errorf("confirmation does not name the slot; stderr lacks %q:\n%s", want, dry.stderr)
	}
	if got := slotOf(t, f.copy); got != 2 {
		t.Fatalf("dry run moved c to slot %d", got)
	}

	res := mustMWS(t, f.meta, "rm", "c", "--purge", "--yes")
	if strings.Contains(res.stderr, "app.env ->") {
		t.Errorf("rm --purge rendered env files:\n%s", res.stderr)
	}
	if fileExists(f.copy) {
		t.Error("c was not deleted")
	}
}

func TestLsShowsEachCopysPorts(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	mustPorts(t, f.meta, "claim", "b", "--slot", "3")

	res := mustMWS(t, f.meta, "ls")
	for _, want := range []string{"  a  default\n", "  b  slot 3\n"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("ls stdout lacks %q:\n%s", want, res.stdout)
		}
	}

	var got struct {
		Copies []struct {
			Name  string `json:"name"`
			Ports string `json:"ports"`
		} `json:"copies"`
	}
	if err := json.Unmarshal([]byte(mustMWS(t, f.meta, "ls", "--json").stdout), &got); err != nil {
		t.Fatal(err)
	}
	byCopy := map[string]string{}
	for _, c := range got.Copies {
		byCopy[c.Name] = c.Ports
	}
	if want := map[string]string{"a": "default", "b": "slot 3"}; !maps.Equal(byCopy, want) {
		t.Errorf("ls --json ports = %v, want %v", byCopy, want)
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
