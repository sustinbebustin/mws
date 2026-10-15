package commands

import (
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
)

// withFreePort gives the env fixture a [ports] table with one port, web
// aliased PORT at stride 10, whose base the OS just handed out as free, so
// the listener probe of a claim does not trip over the host's own services.
// It returns the base.
func (f envFixture) withFreePort(t *testing.T) int {
	t.Helper()
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ports = freePortTable(t)
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Ports.Port[0].Base
}

// freePortTable is a [ports] table with one port, web aliased PORT, based at
// a port the OS just handed out as free.
func freePortTable(t *testing.T) *config.Ports {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", l.Addr())
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return &config.Ports{Port: []config.Port{{Name: "web", Base: addr.Port, Var: "PORT"}}}
}

// slotOf reads the Port slot the lease of the working copy at dir records.
func slotOf(t *testing.T, dir string) int {
	t.Helper()
	slot, err := ports.ReadSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

// mustPorts runs `mws ports <args>` from cwd and fails the test unless it
// exits 0.
func mustPorts(t *testing.T, cwd string, args ...string) mwsResult {
	t.Helper()
	res := runMWS(t, cwd, append([]string{"ports"}, args...)...)
	if res.code != 0 {
		t.Fatalf("mws ports %v: exit %d\nstdout: %s\nstderr: %s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

func TestPortsClaimTakesTheLowestFreeSlotAndReleaseGivesItBack(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	mustMkdir(t, filepath.Join(f.copy("c"), "frontend"))

	mustPorts(t, f.meta, "claim", "a")
	mustPorts(t, f.meta, "claim", "b")
	if got := []int{slotOf(t, f.copy("a")), slotOf(t, f.copy("b"))}; got[0] != 1 || got[1] != 2 {
		t.Fatalf("slots of a, b = %v, want [1 2]", got)
	}

	mustPorts(t, f.meta, "release", "a")
	if got := slotOf(t, f.copy("a")); got != 0 {
		t.Fatalf("after release a is on slot %d, want 0", got)
	}
	mustPorts(t, f.meta, "claim", "c")
	if got := slotOf(t, f.copy("c")); got != 1 {
		t.Fatalf("c claimed slot %d, want the released 1", got)
	}

	// A retry keeps the slot.
	mustPorts(t, f.meta, "claim", "b")
	if got := slotOf(t, f.copy("b")); got != 2 {
		t.Fatalf("re-claim moved b to slot %d, want it kept on 2", got)
	}
}

func TestPortsClaimRefusesAHeldSlotAndAConflict(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	mustMkdir(t, filepath.Join(f.copy("c"), "frontend"))
	mustPorts(t, f.meta, "claim", "a", "--slot", "2")

	res := runMWS(t, f.meta, "ports", "claim", "b", "--slot", "2")
	if res.code != 3 || !strings.Contains(res.stderr, `"a"`) {
		t.Fatalf("claim --slot of a held slot: exit %d, want 3 naming holder a\nstderr: %s", res.code, res.stderr)
	}
	if got := slotOf(t, f.copy("b")); got != 0 {
		t.Fatalf("refused claim left b on slot %d, want 0", got)
	}

	// b duplicates a's lease, as `cp -r` would.
	writeFileAll(t, ports.LeasePath(f.copy("b")), readFile(t, ports.LeasePath(f.copy("a"))))
	res = runMWS(t, f.meta, "ports", "claim", "c")
	if res.code != 3 || !strings.Contains(res.stderr, "a, b") {
		t.Fatalf("claim with a conflict: exit %d, want 3 naming a and b\nstderr: %s", res.code, res.stderr)
	}
	if got := slotOf(t, f.copy("c")); got != 0 {
		t.Fatalf("refused claim gave c slot %d", got)
	}

	// --slot settles the conflict.
	mustPorts(t, f.meta, "claim", "b", "--slot", "1")
	mustPorts(t, f.meta, "claim", "c")
	if got := slotOf(t, f.copy("c")); got != 3 {
		t.Fatalf("after settling, c claimed slot %d, want 3", got)
	}

	res = runMWS(t, f.meta, "ports", "claim", "nope")
	if res.code != 4 {
		t.Fatalf("claim of an unknown copy: exit %d, want 4\nstderr: %s", res.code, res.stderr)
	}
}

func TestPortsClaimDryRunChangesNothing(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "PORT=${PORT}\n")

	got := wouldLines(t, runMWS(t, f.meta, "ports", "claim", "a", "--dry-run"))
	want := []string{"would claim slot 1 for a", "would copy .envs/frontend/root.env -> a/frontend/.env"}
	if !slices.Equal(got, want) {
		t.Fatalf("dry-run lines = %q, want %q", got, want)
	}
	if slotOf(t, f.copy("a")) != 0 || fileExists(filepath.Join(f.copy("a"), "frontend", ".env")) {
		t.Fatal("dry run changed the copy")
	}
}

func TestPortsReleaseOnTheDefaultsSaysSo(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	res := mustPorts(t, f.meta, "release", "a")
	if !strings.Contains(res.stderr, "already on the default ports") {
		t.Fatalf("stderr does not say a is on the defaults:\n%s", res.stderr)
	}
}

func TestPortsShowPrintsTheExportSet(t *testing.T) {
	f := seedEnvWorkspace(t)
	base := f.withFreePort(t)
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Vars = map[string]string{"SITE_URL": "http://localhost:${PORT}"}
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
	mustPorts(t, f.meta, "claim", "a", "--slot", "2")
	port := strconv.Itoa(base + 20)
	want := []string{
		"MWS_SLOT=2",
		"MWS_COPY=a",
		"MWS_COPY_SLUG=a",
		"MWS_META=" + f.meta,
		"MWS_PORT_WEB=" + port,
		"PORT=" + port,
		"SITE_URL=http://localhost:" + port,
	}

	for _, args := range [][]string{{"show", "a"}, {"a"}} {
		res := mustPorts(t, f.meta, args...)
		if got := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n"); !slices.Equal(got, want) {
			t.Errorf("mws ports %v lines = %q, want %q", args, got, want)
		}
	}

	res := mustPorts(t, f.copy("a"), "show", "--export")
	for _, line := range strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n") {
		if !strings.HasPrefix(line, "export ") {
			t.Errorf("--export line %q lacks the export prefix", line)
		}
	}
	if !strings.Contains(res.stdout, "export PORT="+port+"\n") {
		t.Errorf("--export output lacks PORT:\n%s", res.stdout)
	}

	res = mustPorts(t, f.meta, "show", "b", "--json")
	var out struct {
		Copy string            `json:"copy"`
		Slot int               `json:"slot"`
		Vars map[string]string `json:"vars"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, res.stdout)
	}
	if out.Copy != "b" || out.Slot != 0 || out.Vars["PORT"] != strconv.Itoa(base) || out.Vars["MWS_SLOT"] != "0" {
		t.Errorf("show b --json = %+v, want b on slot 0 with PORT %d", out, base)
	}
}

func TestPortsReleaseAfterClaimRestoresTheBytesOfAFreshClone(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Ports = freePortTable(t)
	f.cfg.Vars = map[string]string{"SITE_URL": "http://localhost:${PORT}"}
	f.cfg.Renders = []config.Render{{Source: ".mcp.json.render", Target: ".mcp.json"}}
	f.cfg.Setup = []config.SetupCommand{{Cmd: `printf '%s\n' "$MWS_SLOT" >> setup.out`}}
	f.saveConfig(t)
	staged := "PORT=${PORT}\nURL=${SITE_URL}\nHOME=$HOME\n"
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env"), staged)
	writeFileAll(t, filepath.Join(f.meta, project.HarnessDirName, ".mcp.json.render"), `{"port":${MWS_PORT_WEB}}`)

	if res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup"); res.code != 0 {
		t.Fatalf("clone: exit %d\nstderr: %s", res.code, res.stderr)
	}
	copyDir := filepath.Join(f.meta, "d")
	targets := []string{filepath.Join(copyDir, "frontend", "app.env"), filepath.Join(copyDir, ".mcp.json")}
	fresh := make([]string, len(targets))
	for i, p := range targets {
		fresh[i] = readFile(t, p)
	}

	mustPorts(t, f.meta, "claim", "d", "--setup")
	for i, p := range targets {
		if readFile(t, p) == fresh[i] {
			t.Errorf("claim left %s at the fresh clone's bytes %q", p, fresh[i])
		}
	}
	mustPorts(t, f.meta, "release", "d", "--setup")
	for i, p := range targets {
		if got := readFile(t, p); got != fresh[i] {
			t.Errorf("after claim and release %s = %q, want the fresh clone's %q", p, got, fresh[i])
		}
	}
	if fileExists(ports.LeasePath(copyDir)) {
		t.Error("release kept the lease")
	}
	if got := readFile(t, filepath.Join(copyDir, "setup.out")); got != "1\n0\n" {
		t.Errorf("root setup saw slots %q, want 1 then 0", got)
	}
}
