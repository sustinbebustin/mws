package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/project"
)

// State classifies a listed worktree.
type State string

const (
	// Managed is marked and registered: mws may remove it.
	Managed State = "managed"
	// Orphan is registered in the meta repo, but its directory is gone.
	Orphan State = "orphan"
	// Unmanaged is a directory in the worktrees dir without marker and
	// registration, or a native repo worktree registered outside it.
	Unmanaged State = "unmanaged"
)

// RepoStatus is the live git state of one native repo in a worktree.
type RepoStatus struct {
	Folder string `json:"folder"`
	// Branch is "" when the checkout is detached.
	Branch        string `json:"branch"`
	SHA           string `json:"sha,omitempty"`
	Dirty         bool   `json:"dirty"`
	UniqueCommits int    `json:"unique_commits"`
}

// Entry is one listed worktree. Repos, Live and Created are filled for
// Managed entries; Detail explains an Orphan or Unmanaged one.
type Entry struct {
	Name       string       `json:"name"`
	Path       string       `json:"path"`
	Copy       string       `json:"copy"`
	State      State        `json:"state"`
	Detail     string       `json:"detail,omitempty"`
	Repos      []RepoStatus `json:"repos"`
	Dirty      bool         `json:"dirty"`
	Live       []int        `json:"live"`
	Created    *time.Time   `json:"created,omitempty"`
	AgeSeconds int64        `json:"age_seconds,omitempty"`
}

// UniqueCommits sums the unmerged commits across the entry's repos.
func (e *Entry) UniqueCommits() int {
	n := 0
	for _, r := range e.Repos {
		n += r.UniqueCommits
	}
	return n
}

// ListOptions describes one `mws wt ls`.
type ListOptions struct {
	Workspace *project.Workspace
	Config    *config.Config
	// All lists the worktrees of every working copy, not only the current one.
	All bool
}

// List reports every worktree of the workspace's copy (or of every copy),
// sorted by path. It warns when liveness cannot be checked on this system.
func List(ctx context.Context, r Reporter, opts ListOptions) ([]Entry, error) {
	ws := opts.Workspace
	copies := []string{ws.WorkingCopy}
	if opts.All {
		var err error
		if copies, err = ws.EnumerateCopies(); err != nil {
			return nil, failuref("list working copies of %s: %w", ws.MetaRoot, err)
		}
	} else if ws.WorkingCopy == "" {
		return nil, usagef("not inside a working copy: run mws worktree ls from a working copy, or pass --all to list every copy under %s", ws.MetaRoot)
	}
	live := scanProcesses()
	if !live.supported {
		r.Warn("cannot list process working directories on this system, so liveness is not shown")
	}
	metaWts, err := git.Worktrees(ctx, ws.MetaRoot)
	if err != nil {
		return nil, failuref("list the meta repo's worktrees: %w", err)
	}
	l := lister{ctx: ctx, policy: opts.Config.WorktreePolicy(), live: live, now: time.Now(), metaWts: metaWts}
	var out []Entry
	for _, c := range copies {
		entries, err := l.copy(c)
		if err != nil {
			return nil, failuref("list worktrees of %s: %w", c, err)
		}
		out = append(out, entries...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

type lister struct {
	ctx     context.Context
	policy  config.WorktreePolicy
	live    liveness
	now     time.Time
	metaWts []git.Worktree
}

func (l lister) copy(copyPath string) ([]Entry, error) {
	wtDir := filepath.Join(copyPath, l.policy.Dir)
	registered := map[string]bool{}
	for _, w := range l.metaWts {
		if filepath.Dir(w.Path) == wtDir {
			registered[w.Path] = true
		}
	}

	var out []Entry
	dirs, err := os.ReadDir(wtDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		root := filepath.Join(wtDir, d.Name())
		e := Entry{Name: d.Name(), Path: root, Copy: copyPath, State: Unmanaged, Repos: []RepoStatus{}, Live: []int{}}
		marked := hasMarker(root)
		switch {
		case marked && registered[root]:
			if err := l.inspect(&e); err != nil {
				return nil, fmt.Errorf("%s: %w", d.Name(), err)
			}
		case marked:
			e.Detail = "marked but not registered in the meta repo; `mws wt prune` repairs a moved worktree"
		default:
			e.Detail = fmt.Sprintf("no %s marker; mws never touches it", project.WorktreeMarkerFileName)
		}
		out = append(out, e)
	}
	for p := range registered {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			out = append(out, Entry{Name: filepath.Base(p), Path: p, Copy: copyPath, State: Orphan, Repos: []RepoStatus{}, Live: []int{},
				Detail: "registered in the meta repo, but the directory is gone; `mws wt prune` drops the registration"})
		}
	}

	stray, err := l.strayInner(copyPath, wtDir)
	if err != nil {
		return nil, err
	}
	return append(out, stray...), nil
}

// strayInner lists native repo worktrees registered outside the worktrees dir
// (the copy's own checkout excepted): layouts improvised without mws.
func (l lister) strayInner(copyPath, wtDir string) ([]Entry, error) {
	folders, err := nativeRepos(copyPath)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, folder := range folders {
		src := filepath.Join(copyPath, folder)
		wts, err := git.Worktrees(l.ctx, src)
		if err != nil {
			return nil, fmt.Errorf("%s: list worktrees: %w", folder, err)
		}
		for _, w := range wts {
			if w.Path == src || strings.HasPrefix(w.Path, wtDir+string(filepath.Separator)) {
				continue
			}
			out = append(out, Entry{
				Name: filepath.Base(w.Path), Path: w.Path, Copy: copyPath, State: Unmanaged,
				Repos: []RepoStatus{{Folder: folder, Branch: w.Branch}}, Live: []int{},
				Detail: fmt.Sprintf("%s worktree registered outside %s; mws never touches it", folder, wtDir),
			})
		}
	}
	return out, nil
}

// inspect fills a managed entry with its repos, liveness and age.
func (l lister) inspect(e *Entry) error {
	e.State = Managed
	repos, err := readInnerRepos(l.ctx, e.Copy, e.Path)
	if err != nil {
		return err
	}
	for _, ir := range repos {
		rs := RepoStatus{Folder: ir.folder, Branch: ir.branch, Dirty: ir.dirty}
		if rs.SHA, err = git.ResolveCommit(l.ctx, ir.dir, "HEAD"); err != nil {
			return fmt.Errorf("%s: %w", ir.folder, err)
		}
		if ir.branch != "" {
			if rs.UniqueCommits, err = git.UniqueCommits(l.ctx, ir.src, ir.branch); err != nil {
				return fmt.Errorf("%s: count unmerged commits: %w", ir.folder, err)
			}
		}
		e.Dirty = e.Dirty || ir.dirty
		e.Repos = append(e.Repos, rs)
	}
	if l.live.supported {
		e.Live = append(e.Live, l.live.in(e.Path)...)
	}
	var m Marker
	if _, err := toml.DecodeFile(filepath.Join(e.Path, project.WorktreeMarkerFileName), &m); err == nil && !m.Created.IsZero() {
		created := m.Created
		e.Created = &created
		e.AgeSeconds = int64(l.now.Sub(created).Seconds())
	}
	return nil
}

func hasMarker(root string) bool {
	st, err := os.Stat(filepath.Join(root, project.WorktreeMarkerFileName))
	return err == nil && st.Mode().IsRegular()
}
