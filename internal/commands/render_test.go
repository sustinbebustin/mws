package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
)

// withPorts gives the env fixture a [ports] table: web at 3000 aliased
// PORT, db at 54322, stride 10.
func (f envFixture) withPorts(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ports = &config.Ports{Port: []config.Port{
		{Name: "web", Base: 3000, Var: "PORT"},
		{Name: "db", Base: 54322},
	}}
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestCloneRendersAtSlotZeroAndALeaseMovesOnlyThePlaceholders(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Ports = &config.Ports{Port: []config.Port{{Name: "web", Base: 3000, Var: "PORT"}}}
	f.cfg.Vars = map[string]string{"SITE_URL": "http://localhost:${PORT}"}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.meta, project.EnvStagingDirName, "frontend", "app.env"),
		"PORT=${PORT}\nURL=${SITE_URL}/x\nSLUG=${MWS_COPY_SLUG}\n")

	if res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup"); res.code != 0 {
		t.Fatalf("clone: exit %d\nstderr: %s", res.code, res.stderr)
	}
	target := filepath.Join(f.meta, "d", "frontend", "app.env")
	if got, want := readFile(t, target), "PORT=3000\nURL=http://localhost:3000/x\nSLUG=d\n"; got != want {
		t.Errorf("after clone app.env = %q, want slot-0 values %q", got, want)
	}

	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := ports.WriteLease(filepath.Join(f.meta, "d"), ports.NewExports(cfg, f.meta, "d", 2)); err != nil {
		t.Fatal(err)
	}
	if res := runMWS(t, f.meta, "env", "sync", "d"); res.code != 0 {
		t.Fatalf("sync: exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got, want := readFile(t, target), "PORT=3020\nURL=http://localhost:3020/x\nSLUG=d\n"; got != want {
		t.Errorf("after a slot-2 lease app.env = %q, want %q", got, want)
	}
}

func TestCloneRendersHarnessRenderSourceToARegularFileAtTheCopyRoot(t *testing.T) {
	f := seedWtWorkspace(t)
	f.cfg.Ports = &config.Ports{Port: []config.Port{{Name: "web", Base: 3000}}}
	f.cfg.Renders = []config.Render{{Source: ".mcp.json.render", Target: ".mcp.json"}}
	f.saveConfig(t)
	writeFileAll(t, filepath.Join(f.meta, project.HarnessDirName, ".mcp.json.render"), `{"url":"http://localhost:${MWS_PORT_WEB}"}`)

	res := runMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup")
	if res.code != 0 {
		t.Fatalf("clone: exit %d\nstderr: %s", res.code, res.stderr)
	}
	copyDir := filepath.Join(f.meta, "d")
	target := filepath.Join(copyDir, ".mcp.json")
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("d/.mcp.json is not a regular file: %v %v", info, err)
	}
	if got, want := readFile(t, target), `{"url":"http://localhost:3000"}`; got != want {
		t.Errorf("d/.mcp.json = %q, want %q", got, want)
	}
	if info, err := os.Lstat(filepath.Join(copyDir, ".mcp.json.render")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("d/.mcp.json.render is not a harness symlink: %v %v", info, err)
	}

	got := wouldLines(t, runMWS(t, f.meta, "env", "sync", "d", "--dry-run"))
	if want := "would copy .mws/.mcp.json.render -> d/.mcp.json"; !slices.Contains(got, want) {
		t.Errorf("dry run lines = %q, want %q among them", got, want)
	}
}

func TestEnvDiffComparesTheRenderedForm(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withPorts(t)
	writeFileAll(t, f.staged("frontend", "root.env"), "PORT=${PORT}\n")
	writeFileAll(t, filepath.Join(f.copy("a"), "frontend", ".env"), "PORT=3000\n")

	res := runMWS(t, f.meta, "env", "diff", "a")
	if !strings.Contains(res.stdout, "same               a/frontend/.env <- .envs/frontend/root.env") {
		t.Errorf("a slot-0 copy holding the rendered bytes is not reported same:\n%s", res.stdout)
	}
}

func TestEnvSyncPassesEveryOtherDollarThroughAndWarnsOnTypo(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withPorts(t)
	plain := "A=1\nB=$HOME/x\n"
	writeFileAll(t, f.staged("frontend", "apps-internal.env"), plain)
	writeFileAll(t, f.staged("frontend", "root.env"), "HOME_DIR=$HOME\nLOWER=${lower}\nWEB=${PORT}\nBAD=${TYPO}\n")

	res := runMWS(t, f.meta, "env", "sync", "a")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", "apps", "internal", ".env")); got != plain {
		t.Errorf("file without placeholders = %q, want it byte-identical to %q", got, plain)
	}
	want := "HOME_DIR=$HOME\nLOWER=${lower}\nWEB=3000\nBAD=${TYPO}\n"
	if got := readFile(t, filepath.Join(f.copy("a"), "frontend", ".env")); got != want {
		t.Errorf("rendered .env = %q, want %q", got, want)
	}
	if n := strings.Count(res.stderr, "[WARN]"); n != 1 || !strings.Contains(res.stderr, "${TYPO}") {
		t.Errorf("want exactly one [WARN] naming ${TYPO}, got %d:\n%s", n, res.stderr)
	}
}
