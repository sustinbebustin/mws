package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	Name   string       `json:"name"`
	Path   string       `json:"path"`
	Copy   string       `json:"copy"`
	State  State        `json:"state"`
	Detail string       `json:"detail,omitempty"`
	Repos  []RepoStatus `json:"repos"`
	// RootFiles lists root entries removal would lose (see rootChanges).
	RootFiles []string `json:"root_files"`
	// Dirty is set when a repo is dirty or RootFiles is not empty.
	Dirty      bool       `json:"dirty"`
	Live       PIDs       `json:"live"`
	Created    *time.Time `json:"created,omitempty"`
	AgeSeconds int64      `json:"age_seconds,omitempty"`
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
	return ListCopies(ctx, r, ws.MetaRoot, opts.Config, copies)
}

// ListCopies reports every worktree of the working copies at copies, sorted
// by path. It warns when liveness cannot be checked on this system.
func ListCopies(ctx context.Context, r Reporter, metaRoot string, cfg *config.Config, copies []string) ([]Entry, error) {
	live := scanProcesses()
	if !live.supported {
		r.Warn("cannot list process working directories on this system, so liveness is not shown")
	}
	metaWts, err := git.Worktrees(ctx, metaRoot)
	if err != nil {
		return nil, failuref("list the meta repo's worktrees: %w", err)
	}
	l := lister{policy: cfg.WorktreePolicy(), live: live, now: time.Now(), metaWts: metaWts}
	var out []Entry
	for _, c := range copies {
		entries, err := l.copy(ctx, c)
		if err != nil {
			return nil, failuref("list worktrees of %s: %w", c, err)
		}
		out = append(out, entries...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

type lister struct {
	policy  config.WorktreePolicy
	live    liveness
	now     time.Time
	metaWts []git.Worktree
}

func (l lister) copy(ctx context.Context, copyPath string) ([]Entry, error) {
	s, err := scanCopy(copyPath, l.policy, l.metaWts)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range s.dirs {
		e := Entry{Name: d.name, Path: d.root, Copy: copyPath, State: Unmanaged, Repos: []RepoStatus{}, Live: PIDs{}}
		switch {
		case d.marked && d.registered:
			if err := l.inspect(ctx, &e); err != nil {
				return nil, fmt.Errorf("%s: %w", d.name, err)
			}
		case d.marked:
			e.Detail = "marked but not registered in the meta repo; `mws wt prune` repairs a moved worktree"
		default:
			e.Detail = fmt.Sprintf("no %s marker; mws never touches it", project.WorktreeMarkerFileName)
		}
		out = append(out, e)
	}
	for _, p := range s.orphans {
		out = append(out, Entry{Name: filepath.Base(p), Path: p, Copy: copyPath, State: Orphan, Repos: []RepoStatus{}, Live: PIDs{},
			Detail: "registered in the meta repo, but the directory is gone; `mws wt prune` drops the registration"})
	}

	stray, err := l.strayInner(ctx, copyPath, filepath.Join(copyPath, l.policy.Dir))
	if err != nil {
		return nil, err
	}
	return append(out, stray...), nil
}

// strayInner lists native repo worktrees registered outside the worktrees dir
// (the copy's own checkout excepted): layouts improvised without mws.
func (l lister) strayInner(ctx context.Context, copyPath, wtDir string) ([]Entry, error) {
	folders, err := nativeRepos(copyPath)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, folder := range folders {
		src := filepath.Join(copyPath, folder)
		wts, err := git.Worktrees(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("%s: list worktrees: %w", folder, err)
		}
		for _, w := range wts {
			if w.Path == src || strings.HasPrefix(w.Path, wtDir+string(filepath.Separator)) {
				continue
			}
			out = append(out, Entry{
				Name: filepath.Base(w.Path), Path: w.Path, Copy: copyPath, State: Unmanaged,
				Repos: []RepoStatus{{Folder: folder, Branch: w.Branch}}, Live: PIDs{},
				Detail: fmt.Sprintf("%s worktree registered outside %s; mws never touches it", folder, wtDir),
			})
		}
	}
	return out, nil
}

// inspect fills a managed entry with its repos, liveness and age.
func (l lister) inspect(ctx context.Context, e *Entry) error {
	e.State = Managed
	repos, err := readInnerRepos(ctx, e.Copy, e.Path)
	if err != nil {
		return err
	}
	for _, ir := range repos {
		rs := RepoStatus{Folder: ir.folder, Branch: ir.branch, Dirty: ir.dirty}
		if rs.SHA, err = git.ResolveCommit(ctx, ir.dir, "HEAD"); err != nil {
			return fmt.Errorf("%s: %w", ir.folder, err)
		}
		if ir.branch != "" {
			if rs.UniqueCommits, err = git.UniqueCommits(ctx, ir.src, ir.branch); err != nil {
				return fmt.Errorf("%s: count unmerged commits: %w", ir.folder, err)
			}
		}
		e.Dirty = e.Dirty || ir.dirty
		e.Repos = append(e.Repos, rs)
	}
	if e.RootFiles, err = rootChanges(e.Path, folderNames(repos)); err != nil {
		return fmt.Errorf("inspect the root: %w", err)
	}
	e.Dirty = e.Dirty || len(e.RootFiles) > 0
	if l.live.supported {
		e.Live = append(e.Live, l.live.in(e.Path)...)
	}
	if m, err := readMarker(e.Path); err == nil && !m.Created.IsZero() {
		created := m.Created
		e.Created = &created
		e.AgeSeconds = int64(l.now.Sub(created).Seconds())
	}
	return nil
}
