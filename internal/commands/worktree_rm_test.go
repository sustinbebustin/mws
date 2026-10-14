package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/project"
)

// decodeJSON parses stdout as exactly one JSON object into v.
func decodeJSON(t *testing.T, res wtResult, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(res.stdout), v); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, res.stdout)
	}
}

type jsonErrorOut struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// decodeError returns the {"error": ...} object of a failed --json run.
func decodeError(t *testing.T, res wtResult) jsonErrorOut {
	t.Helper()
	var out struct {
		Error *jsonErrorOut `json:"error"`
	}
	decodeJSON(t, res, &out)
	if out.Error == nil {
		t.Fatalf("stdout has no error object:\n%s", res.stdout)
	}
	return *out.Error
}

// assertRemoved checks that worktree name is gone: no directory, no meta or
// inner registration.
func assertRemoved(t *testing.T, f wtFixture, name string) {
	t.Helper()
	if _, err := os.Lstat(f.wtPath(name)); err == nil {
		t.Fatalf("%s still exists", f.wtPath(name))
	}
	for _, dir := range []string{f.meta, f.repo("backend"), f.repo("frontend")} {
		if got := gitT(t, dir, "worktree", "list", "--porcelain"); strings.Contains(got, f.wtPath(name)) {
			t.Fatalf("%s still registers %s:\n%s", dir, f.wtPath(name), got)
		}
	}
}

// branchExists reports whether folder's copy repo has the local branch.
func branchExists(t *testing.T, f wtFixture, folder, branch string) bool {
	t.Helper()
	return gitT(t, f.repo(folder), "branch", "--list", branch) != ""
}

// commitIn commits a new file in dir.
func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	writeFileAll(t, filepath.Join(dir, file), file)
	gitT(t, dir, "add", file)
	gitT(t, dir, "commit", "-q", "-m", file)
}

func TestWtRmRefusesDirtyRepoUnlessForced(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	writeFileAll(t, filepath.Join(f.wtPath("x"), "frontend", "wip.txt"), "unsaved")

	res := runWt(t, f.copy, "rm", "x", "--json")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	e := decodeError(t, res)
	if e.Code != "refused" || !strings.Contains(e.Message, "frontend") || strings.Contains(e.Message, "backend") || !strings.Contains(e.Message, "--force") {
		t.Fatalf("error = %+v, want refused naming only frontend and offering --force", e)
	}
	if got := readFile(t, filepath.Join(f.wtPath("x"), "frontend", "wip.txt")); got != "unsaved" {
		t.Fatalf("refused rm touched the work: %q", got)
	}

	if res := runWt(t, f.copy, "rm", "x", "--force"); res.code != 0 {
		t.Fatalf("--force: exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

func TestWtRmKeepsBranchWithUnmergedCommits(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	commitIn(t, filepath.Join(f.wtPath("x"), "frontend"), "a.txt")
	commitIn(t, filepath.Join(f.wtPath("x"), "frontend"), "b.txt")

	res := runWt(t, f.copy, "rm", "x")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
	if !branchExists(t, f, "frontend", "wt/x") {
		t.Fatal("frontend lost wt/x and its two unmerged commits")
	}
	if branchExists(t, f, "backend", "wt/x") {
		t.Fatal("backend kept wt/x although it holds no unique commit")
	}
	if !strings.Contains(res.stdout, "kept frontend:wt/x, 2 commits not merged anywhere") {
		t.Fatalf("stdout should say why frontend:wt/x was kept:\n%s", res.stdout)
	}
}

// A failure after earlier names were removed still reports those removals.
func TestWtRmReportsRemovalsBeforeAFailure(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	mustNew(t, f.copy, "y", "--no-setup")
	// git refuses to remove a locked worktree without --force --force.
	gitT(t, f.repo("frontend"), "worktree", "lock", filepath.Join(f.wtPath("y"), "frontend"))

	res := runWt(t, f.copy, "rm", "x", "y", "--json")
	if res.code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", res.code, res.stderr)
	}
	var out struct {
		Removed []struct {
			Name string `json:"name"`
		} `json:"removed"`
		Error *jsonErrorOut `json:"error"`
	}
	decodeJSON(t, res, &out)
	if len(out.Removed) != 1 || out.Removed[0].Name != "x" || out.Error == nil || !strings.Contains(out.Error.Message, "y") {
		t.Fatalf("output = %+v, want x removed and an error naming y", out)
	}
	assertRemoved(t, f, "x")
}

// The root's .gitignore hides every root entry from git, so mws itself must
// notice work there: new files and directories, and edited copies.
func TestWtRmRefusesRootFilesUnlessForced(t *testing.T) {
	for name, change := range map[string]func(root string){
		"new file":    func(root string) { writeFileAll(t, filepath.Join(root, "plan.md"), "plan") },
		"new dir":     func(root string) { writeFileAll(t, filepath.Join(root, "notes", "a.md"), "a") },
		"edited copy": func(root string) { writeFileAll(t, filepath.Join(root, ".ports.env"), "PITCH_SLOT=9\n") },
	} {
		t.Run(name, func(t *testing.T) {
			f := seedWtWorkspace(t)
			mustNew(t, f.copy, "x", "--no-setup")
			change(f.wtPath("x"))

			res := runWt(t, f.copy, "rm", "x", "--json")
			if res.code != 3 {
				t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
			}
			if e := decodeError(t, res); e.Code != "refused" || !strings.Contains(e.Message, "root") || !strings.Contains(e.Message, "--force") {
				t.Fatalf("error = %+v, want refused naming the root and --force", e)
			}

			if res := runWt(t, f.copy, "rm", "x", "--force"); res.code != 0 {
				t.Fatalf("--force: exit %d\nstderr: %s", res.code, res.stderr)
			}
			assertRemoved(t, f, "x")
		})
	}
}

// A copy-root file that changes after creation leaves the worktree's own,
// untouched copy pristine.
func TestWtRmIgnoresChangesInTheCopyRoot(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	writeFileAll(t, filepath.Join(f.copy, ".ports.env"), "PITCH_SLOT=9\n")

	if res := runWt(t, f.copy, "rm", "x"); res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

// A detached HEAD's commits live only in that worktree's HEAD and reflog,
// which git deletes along with the worktree.
func TestWtRmRefusesDetachedCommitsUnlessForced(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	inner := filepath.Join(f.wtPath("x"), "frontend")
	gitT(t, inner, "switch", "-q", "--detach")
	commitIn(t, inner, "a.txt")
	head := gitT(t, inner, "rev-parse", "HEAD")

	res := runWt(t, f.copy, "rm", "x", "--json")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	e := decodeError(t, res)
	if e.Code != "refused" || !strings.Contains(e.Message, "frontend") || !strings.Contains(e.Message, head[:12]) || !strings.Contains(e.Message, "--force") {
		t.Fatalf("error = %+v, want refused naming frontend, its HEAD and --force", e)
	}

	if res := runWt(t, f.copy, "rm", "x", "--force"); res.code != 0 {
		t.Fatalf("--force: exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

// Detaching without committing loses nothing, so it does not block rm.
func TestWtRmRemovesCleanDetachedRepo(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	gitT(t, filepath.Join(f.wtPath("x"), "frontend"), "switch", "-q", "--detach")

	if res := runWt(t, f.copy, "rm", "x"); res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
}

func TestWtRmDeletesBranchMergedIntoIntegration(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	commitIn(t, filepath.Join(f.wtPath("x"), "frontend"), "a.txt")
	gitT(t, f.repo("frontend"), "merge", "-q", "--no-ff", "-m", "merge x", "wt/x")

	res := runWt(t, f.copy, "rm", "x")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	if branchExists(t, f, "frontend", "wt/x") {
		t.Fatal("frontend kept wt/x although feat/integration holds its commit")
	}
}

func TestWtRmKeepBranch(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	res := runWt(t, f.copy, "rm", "x", "--keep-branch", "--json")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	for _, folder := range []string{"backend", "frontend"} {
		if !branchExists(t, f, folder, "wt/x") {
			t.Fatalf("--keep-branch deleted %s:wt/x", folder)
		}
	}
	var out struct {
		Removed []struct {
			Name     string `json:"name"`
			Path     string `json:"path"`
			Branches []struct {
				Folder        string `json:"folder"`
				Branch        string `json:"branch"`
				Deleted       bool   `json:"deleted"`
				UniqueCommits int    `json:"unique_commits"`
				Reason        string `json:"reason"`
			} `json:"branches"`
		} `json:"removed"`
	}
	decodeJSON(t, res, &out)
	if len(out.Removed) != 1 || out.Removed[0].Name != "x" || out.Removed[0].Path != f.wtPath("x") || len(out.Removed[0].Branches) != 2 {
		t.Fatalf("removed = %+v, want x with two branch outcomes", out.Removed)
	}
	for _, b := range out.Removed[0].Branches {
		if b.Branch != "wt/x" || b.Deleted || b.UniqueCommits != 0 || b.Reason != "--keep-branch" {
			t.Fatalf("branch outcome = %+v, want kept for --keep-branch", b)
		}
	}
}

func TestWtRmRefusesUnmanagedDirectories(t *testing.T) {
	f := seedWtWorkspace(t)

	// Unmarked: a plain directory where a worktree would be.
	writeFileAll(t, filepath.Join(f.wtPath("plain"), "notes.txt"), "keep me")
	// Marked but unregistered: a copied worktree root.
	writeFileAll(t, filepath.Join(f.wtPath("copied"), ".mws-worktree.toml"), "name = \"copied\"\n")

	for _, name := range []string{"plain", "copied"} {
		res := runWt(t, f.copy, "rm", name, "--force", "--json")
		if res.code != 3 {
			t.Fatalf("%s: exit %d, want 3\nstderr: %s", name, res.code, res.stderr)
		}
		if e := decodeError(t, res); e.Code != "refused" || !strings.Contains(e.Message, "nothing was changed") {
			t.Fatalf("%s: error = %+v, want refused saying nothing changed", name, e)
		}
		if _, err := os.Stat(f.wtPath(name)); err != nil {
			t.Fatalf("%s: refused rm removed the directory: %v", name, err)
		}
	}
	if got := readFile(t, filepath.Join(f.wtPath("plain"), "notes.txt")); got != "keep me" {
		t.Fatalf("plain: %q", got)
	}
}

// A refusal for one name removes none of the others.
func TestWtRmChecksEveryNameBeforeRemoving(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	mustNew(t, f.copy, "y", "--no-setup")
	writeFileAll(t, filepath.Join(f.wtPath("y"), "backend", "wip.txt"), "unsaved")

	if res := runWt(t, f.copy, "rm", "x", "y"); res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(f.wtPath("x")); err != nil {
		t.Fatalf("x was removed although y refused: %v", err)
	}
}

// startIn starts a long-running process with its cwd at dir and stops it
// when the test ends.
func startIn(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func TestWtRmRefusesWhileAForeignProcessRunsInside(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process cwds are read from /proc")
	}
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	agent := startIn(t, filepath.Join(f.wtPath("x"), "frontend"))

	res := runWt(t, f.copy, "rm", "x", "--force", "--json")
	if res.code != 3 {
		t.Fatalf("exit %d, want 3\nstderr: %s", res.code, res.stderr)
	}
	e := decodeError(t, res)
	if e.Code != "refused" || !strings.Contains(e.Message, strconv.Itoa(agent.Process.Pid)) || !strings.Contains(e.Message, "nothing was removed") {
		t.Fatalf("error = %+v, want refused naming pid %d", e, agent.Process.Pid)
	}
	if _, err := os.Stat(filepath.Join(f.wtPath("x"), project.WorktreeMarkerFileName)); err != nil {
		t.Fatalf("refused rm removed the worktree: %v", err)
	}
}

// A hook runs mws from a shell whose cwd is inside the worktree it removes:
// that shell is an ancestor of mws and must not count as a live user.
func TestWtRmIgnoresItsOwnProcessTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process cwds are read from /proc")
	}
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// The trailing command keeps sh alive (no exec) while mws runs.
	sh := exec.Command("sh", "-c", `"$0" wt rm x; exit $?`, self)
	sh.Dir = filepath.Join(f.wtPath("x"), "frontend")
	sh.Env = append(os.Environ(), cliEnv+"=1")
	out, err := sh.CombinedOutput()
	if err != nil {
		t.Fatalf("rm from inside the worktree: %v\n%s", err, out)
	}
	assertRemoved(t, f, "x")
}

func TestWtRmRemovesCleanWorktreeAndItsBranches(t *testing.T) {
	f := seedWtWorkspace(t)
	mustNew(t, f.copy, "x", "--no-setup")

	res := runWt(t, f.copy, "rm", "x")
	if res.code != 0 {
		t.Fatalf("exit %d\nstderr: %s", res.code, res.stderr)
	}
	assertRemoved(t, f, "x")
	for _, folder := range []string{"backend", "frontend"} {
		if branchExists(t, f, folder, "wt/x") {
			t.Fatalf("%s kept wt/x although it holds no unique commit", folder)
		}
	}
}
