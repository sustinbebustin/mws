package commands

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/worktree"
)

func newWorktreeLsCmd(jsonOut *bool) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List worktrees with their branches, state and liveness",
		Long: `ls lists the worktrees of this working copy (--all: of every copy). A managed
worktree shows each native repo as folder:branch@sha, marked dirty when it has
modified or untracked files and +N when N commits on its branch are reachable
from no other ref; then the pids of processes running inside it and its age.

orphan: registered in the meta repo, directory gone (prune drops it).
unmanaged: a directory in the worktrees dir without marker and registration,
or a native repo worktree registered outside the worktrees dir. mws never
removes unmanaged entries.`,
		Example: `  mws wt ls
  mws wt ls --all --json`,
		Args: wtArgs(cobra.NoArgs, jsonOut),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorktreeLs(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), *jsonOut, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list the worktrees of every working copy")
	return cmd
}

func runWorktreeLs(ctx context.Context, out, errW io.Writer, jsonOut, all bool) error {
	ws, cfg, err := locateForWorktree()
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	entries, err := worktree.List(ctx, consoleReporter{w: errW}, worktree.ListOptions{Workspace: ws, Config: cfg, All: all})
	if err != nil {
		return emitWtError(out, jsonOut, err)
	}
	if jsonOut {
		if entries == nil {
			entries = []worktree.Entry{}
		}
		return writeJSON(out, struct {
			Worktrees []worktree.Entry `json:"worktrees"`
		}{entries})
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for i := range entries {
		e := &entries[i]
		name := e.Name
		if all {
			name = filepath.Base(e.Copy) + "/" + e.Name
		}
		if e.State != worktree.Managed {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, e.State, e.Path, e.Detail)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, describeRepos(e.Repos), describeLive(e.Live), describeAge(e))
	}
	return tw.Flush()
}

func describeRepos(repos []worktree.RepoStatus) string {
	parts := make([]string, 0, len(repos))
	for _, r := range repos {
		branch := r.Branch
		if branch == "" {
			branch = "(detached)"
		}
		s := fmt.Sprintf("%s:%s@%.7s", r.Folder, branch, r.SHA)
		if r.Dirty {
			s += " dirty"
		}
		if r.UniqueCommits > 0 {
			s += fmt.Sprintf(" +%d", r.UniqueCommits)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func describeLive(pids []int) string {
	if len(pids) == 0 {
		return "idle"
	}
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = fmt.Sprint(p)
	}
	return "live " + strings.Join(s, ",")
}

func describeAge(e *worktree.Entry) string {
	if e.Created == nil {
		return "age unknown"
	}
	d := time.Duration(e.AgeSeconds) * time.Second
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
