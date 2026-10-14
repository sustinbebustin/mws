package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lsOut mirrors the documented `wt ls --json` shape.
type lsOut struct {
	Worktrees []lsEntry `json:"worktrees"`
}

type lsEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Copy   string `json:"copy"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Repos  []struct {
		Folder        string `json:"folder"`
		Branch        string `json:"branch"`
		SHA           string `json:"sha"`
		Dirty         bool   `json:"dirty"`
		UniqueCommits int    `json:"unique_commits"`
	} `json:"repos"`
	Dirty      bool       `json:"dirty"`
	Live       []int      `json:"live"`
	Created    *time.Time `json:"created"`
	AgeSeconds int64      `json:"age_seconds"`
}

func runLs(t *testing.T, cwd string, args ...string) lsOut {
	t.Helper()
	res := runWt(t, cwd, append([]string{"ls", "--json"}, args...)...)
	if res.code != 0 {
		t.Fatalf("wt ls: exit %d\nstderr: %s", res.code, res.stderr)
	}
	var out lsOut
	decodeJSON(t, res, &out)
	return out
}

// entry returns the listed worktree at path, failing when it is missing.
func (o lsOut) entry(t *testing.T, path string) lsEntry {
	t.Helper()
	for i := range o.Worktrees {
		if o.Worktrees[i].Path == path {
			return o.Worktrees[i]
		}
	}
	t.Fatalf("no entry for %s in %+v", path, o.Worktrees)
	return lsEntry{}
}

func TestWtLsReportsDirtyAheadAndLive(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	writeFileAll(t, filepath.Join(f.wtPath("x"), "frontend", "wip.txt"), "unsaved")
	commitIn(t, filepath.Join(f.wtPath("x"), "backend"), "a.txt")
	var agent *exec.Cmd
	if runtime.GOOS == "linux" {
		agent = startIn(t, filepath.Join(f.wtPath("x"), "backend"))
	}

	e := runLs(t, f.copy).entry(t, f.wtPath("x"))
	if !e.Dirty {
		t.Fatalf("entry = %+v, want dirty", e)
	}
	back, front := e.Repos[0], e.Repos[1]
	if back.Dirty || back.UniqueCommits != 1 || back.SHA != gitT(t, filepath.Join(f.wtPath("x"), "backend"), "rev-parse", "HEAD") {
		t.Fatalf("backend = %+v, want clean with 1 unmerged commit at its new HEAD", back)
	}
	if !front.Dirty || front.UniqueCommits != 0 {
		t.Fatalf("frontend = %+v, want dirty with no unmerged commit", front)
	}
	if agent != nil && (len(e.Live) != 1 || e.Live[0] != agent.Process.Pid) {
		t.Fatalf("live = %v, want [%d]", e.Live, agent.Process.Pid)
	}

	plain := runWt(t, f.copy, "ls")
	for _, want := range []string{"backend:wt/x@", "+1", "frontend:wt/x@", "dirty"} {
		if !strings.Contains(plain.stdout, want) {
			t.Fatalf("plain ls lacks %q:\n%s", want, plain.stdout)
		}
	}
	if agent != nil && !strings.Contains(plain.stdout, "live "+strconv.Itoa(agent.Process.Pid)) {
		t.Fatalf("plain ls lacks the live pid:\n%s", plain.stdout)
	}
}

func TestWtLsReportsOrphanAndUnmanaged(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "gone", "--no-setup")
	if err := os.RemoveAll(f.wtPath("gone")); err != nil {
		t.Fatal(err)
	}
	writeFileAll(t, filepath.Join(f.wtPath("plain"), "notes.txt"), "x")
	improvised := filepath.Join(f.copy+".wt", "01")
	gitT(t, f.repo("frontend"), "worktree", "add", "-q", "-b", "agent-01", improvised)

	out := runLs(t, f.copy)
	if len(out.Worktrees) != 3 {
		t.Fatalf("worktrees = %+v, want gone, plain and the improvised one", out.Worktrees)
	}
	if e := out.entry(t, f.wtPath("gone")); e.State != "orphan" || e.Name != "gone" {
		t.Fatalf("gone = %+v, want orphan", e)
	}
	if e := out.entry(t, f.wtPath("plain")); e.State != "unmanaged" || !strings.Contains(e.Detail, "marker") {
		t.Fatalf("plain = %+v, want unmanaged for a missing marker", e)
	}
	e := out.entry(t, improvised)
	if e.State != "unmanaged" || len(e.Repos) != 1 || e.Repos[0].Folder != "frontend" || e.Repos[0].Branch != "agent-01" {
		t.Fatalf("improvised = %+v, want unmanaged frontend worktree on agent-01", e)
	}
}

func TestWtLsAllCoversEveryCopy(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	other := filepath.Join(f.meta, "d")
	cloneIntegration(t, f.cfg.Repos[0].URL, filepath.Join(other, "backend"))
	mustNew(t, other, "y", "--no-setup")

	if out := runLs(t, f.copy); len(out.Worktrees) != 1 {
		t.Fatalf("without --all: %+v, want only this copy's x", out.Worktrees)
	}
	out := runLs(t, f.meta, "--all")
	if e := out.entry(t, filepath.Join(other, ".wt", "y")); e.Copy != other || e.State != "managed" {
		t.Fatalf("y = %+v, want managed in copy d", e)
	}
	out.entry(t, f.wtPath("x"))

	if res := runWt(t, f.meta, "ls"); res.code != 2 {
		t.Fatalf("ls from the meta root without --all: exit %d, want 2", res.code)
	}
}

func TestWtLsReportsCleanWorktree(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	out := runLs(t, f.copy)
	if len(out.Worktrees) != 1 {
		t.Fatalf("worktrees = %+v, want only x", out.Worktrees)
	}
	e := out.entry(t, f.wtPath("x"))
	if e.Name != "x" || e.Copy != f.copy || e.State != "managed" || e.Dirty || len(e.Live) != 0 || e.Created == nil {
		t.Fatalf("entry = %+v, want a clean, idle, managed x with its creation time", e)
	}
	if len(e.Repos) != 2 {
		t.Fatalf("repos = %+v, want backend and frontend", e.Repos)
	}
	for i, folder := range []string{"backend", "frontend"} {
		r := e.Repos[i]
		if r.Folder != folder || r.Branch != "wt/x" || r.SHA != gitT(t, f.repo(folder), "rev-parse", "HEAD") || r.Dirty || r.UniqueCommits != 0 {
			t.Fatalf("repo %d = %+v, want %s clean on wt/x at the copy's HEAD", i, r, folder)
		}
	}
}
