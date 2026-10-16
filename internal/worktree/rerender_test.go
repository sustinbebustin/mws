package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
)

// recorder is a Reporter that keeps every line.
type recorder struct{ ok, warn []string }

func (r *recorder) OK(s string)   { r.ok = append(r.ok, s) }
func (r *recorder) Warn(s string) { r.warn = append(r.warn, s) }

var testPolicy = config.WorktreePolicy{Dir: ".worktrees"}

// rerenderFixture is a working copy with one marked worktree, w1, whose
// marker records copied as the hashes of the files mws carried.
type rerenderFixture struct{ copy, wt string }

func newRerenderFixture(t *testing.T, copied map[string]string) rerenderFixture {
	t.Helper()
	f := rerenderFixture{copy: t.TempDir()}
	f.wt = Path(f.copy, testPolicy, "w1")
	if err := os.MkdirAll(f.wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeMarker(f.wt, Marker{Copy: filepath.Base(f.copy), Name: "w1", Copied: copied}); err != nil {
		t.Fatal(err)
	}
	return f
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f rerenderFixture) hashes(t *testing.T) map[string]string {
	t.Helper()
	m, err := readMarker(f.wt)
	if err != nil {
		t.Fatal(err)
	}
	return m.Copied
}

func (f rerenderFixture) rerender(t *testing.T, rels ...string) *recorder {
	t.Helper()
	r := &recorder{}
	if err := Rerender(r, f.copy, testPolicy, rels); err != nil {
		t.Fatalf("Rerender: %v", err)
	}
	return r
}

func TestRerenderRewritesAPristineFileAndUpdatesItsHash(t *testing.T) {
	f := newRerenderFixture(t, map[string]string{"frontend/.env": sha("PORT=3000\n")})
	write(t, filepath.Join(f.copy, "frontend", ".env"), "PORT=3010\n")
	write(t, filepath.Join(f.wt, "frontend", ".env"), "PORT=3000\n")

	f.rerender(t, "frontend/.env")

	if got := read(t, filepath.Join(f.wt, "frontend", ".env")); got != "PORT=3010\n" {
		t.Errorf("worktree file = %q, want the copy's PORT=3010", got)
	}
	if got := f.hashes(t)["frontend/.env"]; got != sha("PORT=3010\n") {
		t.Errorf("marker hash = %s, want the hash of the new content", got)
	}
}

func TestRerenderCarriesAFileTheWorktreeNeverGot(t *testing.T) {
	f := newRerenderFixture(t, nil)
	write(t, filepath.Join(f.copy, ".mws-ports.env"), "MWS_SLOT=1\n")
	write(t, filepath.Join(f.copy, "frontend", ".env"), "PORT=3010\n")
	write(t, filepath.Join(f.copy, "backend", ".env"), "PORT=8010\n")
	if err := os.MkdirAll(filepath.Join(f.wt, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}

	f.rerender(t, ".mws-ports.env", "frontend/.env", "backend/.env")

	if got := read(t, filepath.Join(f.wt, ".mws-ports.env")); got != "MWS_SLOT=1\n" {
		t.Errorf("worktree lease = %q, want it carried", got)
	}
	if got := read(t, filepath.Join(f.wt, "frontend", ".env")); got != "PORT=3010\n" {
		t.Errorf("worktree frontend/.env = %q, want it carried", got)
	}
	if _, err := os.Stat(filepath.Join(f.wt, "backend")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("backend/ in the worktree: %v, want it not created for a repo the worktree does not hold", err)
	}
	want := map[string]string{".mws-ports.env": sha("MWS_SLOT=1\n"), "frontend/.env": sha("PORT=3010\n")}
	if got := f.hashes(t); len(got) != len(want) || got[".mws-ports.env"] != want[".mws-ports.env"] || got["frontend/.env"] != want["frontend/.env"] {
		t.Errorf("marker hashes = %v, want %v", got, want)
	}
}

func TestRerenderRemovesAPristineFileTheCopyNoLongerHas(t *testing.T) {
	f := newRerenderFixture(t, map[string]string{".mws-ports.env": sha("MWS_SLOT=2\n")})
	write(t, filepath.Join(f.wt, ".mws-ports.env"), "MWS_SLOT=2\n")

	f.rerender(t, ".mws-ports.env")

	if _, err := os.Stat(filepath.Join(f.wt, ".mws-ports.env")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("worktree lease after release: %v, want it removed", err)
	}
	if _, ok := f.hashes(t)[".mws-ports.env"]; ok {
		t.Error("marker still records the removed file")
	}
}

// After release, a lease the user already deleted from the worktree is gone
// from both sides: there is nothing to warn about, only a stale hash to drop.
func TestRerenderForgetsAFileGoneFromTheCopyAndTheWorktree(t *testing.T) {
	f := newRerenderFixture(t, map[string]string{".mws-ports.env": sha("MWS_SLOT=2\n")})

	r := f.rerender(t, ".mws-ports.env")

	if _, ok := f.hashes(t)[".mws-ports.env"]; ok {
		t.Error("marker still records a file neither side has")
	}
	if len(r.warn) != 0 {
		t.Errorf("warnings = %q, want none", r.warn)
	}
}

func TestRerenderLeavesAnEditedFileAndWarns(t *testing.T) {
	f := newRerenderFixture(t, map[string]string{"frontend/.env": sha("PORT=3000\n")})
	write(t, filepath.Join(f.copy, "frontend", ".env"), "PORT=3010\n")
	write(t, filepath.Join(f.wt, "frontend", ".env"), "PORT=3000\nDEBUG=1\n")

	r := f.rerender(t, "frontend/.env")

	if got := read(t, filepath.Join(f.wt, "frontend", ".env")); got != "PORT=3000\nDEBUG=1\n" {
		t.Errorf("edited worktree file = %q, want it left alone", got)
	}
	if got := f.hashes(t)["frontend/.env"]; got != sha("PORT=3000\n") {
		t.Errorf("marker hash = %s, want the old hash kept", got)
	}
	if len(r.warn) != 1 || !strings.Contains(r.warn[0], "w1") || !strings.Contains(r.warn[0], "frontend/.env") || !strings.Contains(r.warn[0], "edited") {
		t.Errorf("warnings = %q, want one naming worktree w1, frontend/.env and the edit", r.warn)
	}
}

func TestRerenderLeavesAnEditedFileTheCopyNoLongerHas(t *testing.T) {
	f := newRerenderFixture(t, map[string]string{".mcp.json": sha("{}")})
	write(t, filepath.Join(f.wt, ".mcp.json"), `{"edited":true}`)

	r := f.rerender(t, ".mcp.json")

	if got := read(t, filepath.Join(f.wt, ".mcp.json")); got != `{"edited":true}` {
		t.Errorf("edited worktree file = %q, want it kept", got)
	}
	if len(r.warn) != 1 {
		t.Errorf("warnings = %q, want one", r.warn)
	}
}
