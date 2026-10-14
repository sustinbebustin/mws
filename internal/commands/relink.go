package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

// resolution settles a diverged file: a working copy's regular file that
// differs from the same-named regular file in the harness.
type resolution int

const (
	resolveAsk  resolution = iota // prompt on the terminal
	resolvePeer                   // move the copy's file into the harness
	resolveMeta                   // discard the copy's file, keep the harness one
	resolveSkip                   // leave both as they are
)

// parseResolution reads a --prefer value; "" means ask.
func parseResolution(s string) (resolution, error) {
	switch s {
	case "":
		return resolveAsk, nil
	case "peer":
		return resolvePeer, nil
	case "meta":
		return resolveMeta, nil
	case "skip":
		return resolveSkip, nil
	}
	return resolveAsk, usagef("--prefer %q: want peer, meta or skip; see `mws relink --help`", s)
}

func (res resolution) String() string {
	switch res {
	case resolvePeer:
		return "peer"
	case resolveMeta:
		return "meta"
	case resolveSkip:
		return "skip"
	}
	return "ask"
}

// relinkFlags is the command-line input of `mws relink`.
type relinkFlags struct {
	prefer resolution
	dryRun bool
}

func newRelinkCmd() *cobra.Command {
	var prefer string
	var f relinkFlags
	cmd := &cobra.Command{
		Use:   "relink",
		Short: "Refresh harness symlinks in every working copy",
		Long: `relink walks every working copy under this meta workspace and re-runs
symlink discovery against the harness. New top-level harness entries get linked
in, broken symlinks are repaired, and existing non-symlink files are left alone.

If a working copy holds a regular file where the harness has the same-named
regular file (typically because an editor's atomic-save replaced a symlink),
relink detects the divergence. Identical content is auto-repaired by removing
the working-copy file so the symlink can be restored. Diverged content prompts
for which side wins; --prefer answers for every diverged file (peer: move the
copy's file into the harness; meta: discard the copy's file; skip: leave it).

relink inspects every copy before changing any. It refuses up front, changing
nothing, when a diverged file would need a prompt and there is no terminal,
and when --prefer peer would move different versions of one file from several
copies into the harness (only the last would survive).

relink also refreshes the links in each copy's worktrees. Files a worktree
holds as its own copies are left alone. --dry-run prints the plan, and with
--prefer peer fails on the same conflict relink would. A copy that fails while relinking is reported, the
rest are still relinked, and relink then exits non-zero.`,
		Example: `  mws relink
  mws relink --dry-run
  mws relink --prefer meta`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if f.prefer, err = parseResolution(prefer); err != nil {
				return err
			}
			return runRelink(newConsoleReporter(), f)
		},
	}
	cmd.Flags().StringVar(&prefer, "prefer", "", "resolve diverged files without asking: peer, meta or skip")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print what would be linked and repaired; change nothing")
	return cmd
}

// divergence is a top-level regular file of a working copy whose name the
// harness also holds as a regular file.
type divergence struct {
	name, peerPath, metaPath string
	identical                bool
}

// relinkPlan is what relink would do in one working copy.
type relinkPlan struct {
	peer        string
	divergences []divergence
	links       []string // harness links to create or repoint
	worktrees   []string // worktree roots whose links are refreshed
}

func (p relinkPlan) name() string { return filepath.Base(p.peer) }

// scanRelink inspects the copy at peer without changing anything.
func scanRelink(metaRoot, peer string, policy config.WorktreePolicy) (relinkPlan, error) {
	p := relinkPlan{peer: peer}
	harnessRoot := filepath.Join(metaRoot, project.HarnessDirName)
	entries, err := os.ReadDir(peer)
	if err != nil {
		return p, err
	}
	for _, e := range entries {
		d := divergence{name: e.Name(), peerPath: filepath.Join(peer, e.Name()), metaPath: filepath.Join(harnessRoot, e.Name())}
		if !isRegularFile(d.peerPath) || !isRegularFile(d.metaPath) {
			continue
		}
		if d.identical, err = filesEqual(d.peerPath, d.metaPath); err != nil {
			return p, err
		}
		p.divergences = append(p.divergences, d)
	}
	if p.links, err = project.PendingHarnessLinks(metaRoot, peer); err != nil {
		return p, err
	}
	if p.worktrees, err = worktree.Roots(peer, policy); err != nil {
		return p, fmt.Errorf("find worktrees: %w", err)
	}
	return p, nil
}

// isRegularFile reports whether path is a regular file, not following a
// symlink.
func isRegularFile(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode().IsRegular()
}

func runRelink(r Reporter, f relinkFlags) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	// Locate tolerates a malformed .mws.toml; surface the parse error here so
	// the user isn't told "nothing to relink" when the real problem is config.
	cfg, err := config.Load(ws.MetaRoot)
	if err != nil {
		return err
	}
	policy := cfg.WorktreePolicy()

	peers, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		r.Info("No working copies to relink.")
		return nil
	}

	plans := make([]relinkPlan, 0, len(peers))
	for _, peer := range peers {
		p, err := scanRelink(ws.MetaRoot, peer, policy)
		if err != nil {
			return fmt.Errorf("inspect %s: %w; nothing was changed", filepath.Base(peer), err)
		}
		plans = append(plans, p)
	}
	preflight := checkRelink(plans, f.prefer)
	if f.dryRun {
		for _, p := range plans {
			printRelinkPlan(r, p, f.prefer)
		}
		// Without --prefer the plan already says relink would ask; only a
		// conflict --prefer itself causes fails the preview.
		if f.prefer == resolveAsk {
			return nil
		}
		return preflight
	}
	if preflight != nil {
		return preflight
	}

	choose := chooseResolution(f.prefer)
	var errs []error
	for _, p := range plans {
		r.Heading(fmt.Sprintf("Relinking %s ...", p.name()))
		if err := applyRelink(r, ws.MetaRoot, p, policy, choose); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", p.name(), err))
			errs = append(errs, fmt.Errorf("%s: %w", p.name(), err))
		}
	}
	if joined := clierr.Join(errs...); joined != nil {
		return clierr.Wrapf(joined, "relink failed in %d of %d working copies; the others were relinked:\n%w", len(errs), len(peers), joined)
	}
	return nil
}

// checkRelink refuses, before anything changes, a run that would need a
// prompt without a terminal, or whose --prefer peer would move different
// versions of one file from several copies into the harness.
func checkRelink(plans []relinkPlan, prefer resolution) error {
	diverged := map[string][]divergence{} // file name -> copies' versions
	var names, where []string
	for _, p := range plans {
		for _, d := range p.divergences {
			if d.identical {
				continue
			}
			if len(diverged[d.name]) == 0 {
				names = append(names, d.name)
			}
			diverged[d.name] = append(diverged[d.name], d)
			where = append(where, p.name()+"/"+d.name)
		}
	}
	if len(where) == 0 {
		return nil
	}
	if prefer == resolveAsk && !canPrompt() {
		return noPrompt(fmt.Sprintf("%s %s from the harness copy and --prefer was not given; nothing was changed", strings.Join(where, ", "), plural(len(where), "differs", "differ")),
			"mws relink --prefer meta   (keep the harness files)", "mws relink --prefer skip   (leave the copies' files)", "mws relink --prefer peer   (move the copies' files into the harness)")
	}
	if prefer != resolvePeer {
		return nil
	}
	for _, name := range names {
		versions := diverged[name]
		for _, v := range versions[1:] {
			same, err := filesEqual(versions[0].peerPath, v.peerPath)
			if err != nil {
				return clierr.Failuref("compare %s with %s: %w", versions[0].peerPath, v.peerPath, err)
			}
			if !same {
				copies := make([]string, len(versions))
				for i, d := range versions {
					copies[i] = filepath.Base(filepath.Dir(d.peerPath))
				}
				return usagef("--prefer peer would move %d different versions of %s (from %s) into the harness, and only the last would survive; nothing was changed. Reconcile them by hand, or run relink in one copy's favor with --prefer meta after copying its file into %s",
					len(versions), name, strings.Join(copies, ", "), filepath.Dir(versions[0].metaPath))
			}
		}
	}
	return nil
}

// printRelinkPlan reports what relink would do in one copy.
func printRelinkPlan(r Reporter, p relinkPlan, prefer resolution) {
	for _, d := range p.divergences {
		switch {
		case d.identical:
			r.Info(fmt.Sprintf("%s: would remove %s, identical to the harness copy", p.name(), d.name))
		case prefer == resolveAsk:
			r.Info(fmt.Sprintf("%s: %s differs from the harness copy; relink would ask (or use --prefer)", p.name(), d.name))
		default:
			r.Info(fmt.Sprintf("%s: %s differs from the harness copy; --prefer %s applies", p.name(), d.name, prefer))
		}
	}
	for _, n := range p.links {
		r.Info(fmt.Sprintf("%s: would link %s", p.name(), n))
	}
	if n := len(p.worktrees); n > 0 {
		r.Info(fmt.Sprintf("%s: would refresh the links of %d %s", p.name(), n, plural(n, "worktree", "worktrees")))
	}
	if len(p.divergences) == 0 && len(p.links) == 0 && len(p.worktrees) == 0 {
		r.Info(fmt.Sprintf("%s: nothing to change", p.name()))
	}
}

// applyRelink carries out p: settles its divergences, links the harness and
// refreshes its worktrees. A failing worktree is reported and the rest are
// still refreshed.
func applyRelink(r Reporter, metaRoot string, p relinkPlan, policy config.WorktreePolicy, choose chooser) error {
	if err := applyDivergences(r, p, choose); err != nil {
		return fmt.Errorf("divergence repair: %w", err)
	}
	linked, err := project.LinkHarnessIntoWorkingCopy(metaRoot, p.peer)
	if err != nil {
		return err
	}
	if len(linked) == 0 {
		r.Info(fmt.Sprintf("%s: harness links already current", p.name()))
	}
	for _, name := range linked {
		r.OK(fmt.Sprintf("%s: linked %s", p.name(), name))
	}
	var errs []error
	for _, root := range p.worktrees {
		if err := worktree.Relink(metaRoot, p.peer, root, policy); err != nil {
			r.Fail(fmt.Sprintf("%s: worktree %s: %v", p.name(), filepath.Base(root), err))
			errs = append(errs, fmt.Errorf("worktree %s: %w", filepath.Base(root), err))
			continue
		}
		r.OK(fmt.Sprintf("%s: relinked worktree %s", p.name(), filepath.Base(root)))
	}
	return clierr.Join(errs...)
}

// chooser settles one diverged file of the copy called peerName.
type chooser func(peerName, fileName string) (resolution, error)

// chooseResolution answers with prefer when it is set, and otherwise asks on
// the terminal (checkRelink has already refused when there is none).
func chooseResolution(prefer resolution) chooser {
	return func(peerName, fileName string) (resolution, error) {
		if prefer != resolveAsk {
			return prefer, nil
		}
		return huhDivergencePrompt(peerName, fileName)
	}
}

// repairDivergedFiles settles the divergences of the copy at peer: identical
// content is auto-resolved by removing the copy's file
// (LinkHarnessIntoWorkingCopy then recreates the symlink), diverged content
// goes through choose.
func repairDivergedFiles(r Reporter, metaRoot, peer string, choose chooser) error {
	p, err := scanRelink(metaRoot, peer, config.WorktreePolicy{})
	if err != nil {
		return err
	}
	return applyDivergences(r, p, choose)
}

func applyDivergences(r Reporter, p relinkPlan, choose chooser) error {
	for _, d := range p.divergences {
		res := resolveMeta
		if !d.identical {
			var err error
			if res, err = choose(p.name(), d.name); err != nil {
				return err
			}
		}
		switch res {
		case resolvePeer:
			if err := moveFile(d.peerPath, d.metaPath); err != nil {
				return fmt.Errorf("move %s -> %s: %w", d.peerPath, d.metaPath, err)
			}
			r.OK(fmt.Sprintf("%s: promoted peer %s into meta", p.name(), d.name))
		case resolveMeta:
			if err := os.Remove(d.peerPath); err != nil {
				return err
			}
			if d.identical {
				r.OK(fmt.Sprintf("%s: %s identical to meta, removed peer copy", p.name(), d.name))
			} else {
				r.OK(fmt.Sprintf("%s: discarded peer %s, kept meta version", p.name(), d.name))
			}
		case resolveSkip:
			r.Warn(fmt.Sprintf("%s: %s left diverged", p.name(), d.name))
		case resolveAsk:
			return fmt.Errorf("%s: no resolution chosen", d.name)
		}
	}
	return nil
}

func huhDivergencePrompt(peerName, fileName string) (resolution, error) {
	choice := resolveSkip
	err := huh.NewSelect[resolution]().
		Title(fmt.Sprintf("%s: %s differs from meta. Which to keep?", peerName, fileName)).
		Options(
			huh.NewOption("Peer (promote into meta, other peers will link to it)", resolvePeer),
			huh.NewOption("Meta (discard peer edits, restore symlink)", resolveMeta),
			huh.NewOption("Skip (leave diverged)", resolveSkip),
		).
		Value(&choice).
		Run()
	return choice, err
}

func filesEqual(a, b string) (bool, error) {
	af, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer af.Close()
	bf, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer bf.Close()

	const bufSize = 64 * 1024
	abuf := make([]byte, bufSize)
	bbuf := make([]byte, bufSize)
	for {
		an, aerr := io.ReadFull(af, abuf)
		bn, berr := io.ReadFull(bf, bbuf)
		if an != bn || !bytes.Equal(abuf[:an], bbuf[:bn]) {
			return false, nil
		}
		if aerr == io.EOF || aerr == io.ErrUnexpectedEOF {
			return berr == io.EOF || berr == io.ErrUnexpectedEOF, nil
		}
		if aerr != nil {
			return false, aerr
		}
	}
}
