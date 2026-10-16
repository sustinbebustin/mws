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
	takeCopy                      // move the copy's file into the harness
	keepHarness                   // discard the copy's file, keep the harness one
	resolveSkip                   // leave both as they are
)

// parseResolution reads a --prefer value; "" means ask.
func parseResolution(s string) (resolution, error) {
	switch s {
	case "":
		return resolveAsk, nil
	case "copy":
		return takeCopy, nil
	case "harness":
		return keepHarness, nil
	case "skip":
		return resolveSkip, nil
	}
	return resolveAsk, usagef("--prefer %q: want copy, harness or skip; see `mws relink --help`", s)
}

func (res resolution) String() string {
	switch res {
	case takeCopy:
		return "copy"
	case keepHarness:
		return "harness"
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
for which side wins; --prefer answers for every diverged file (copy: move the
copy's file into the harness; harness: discard the copy's file; skip: leave it).

relink inspects every copy before changing any. It refuses up front, changing
nothing, when a diverged file would need a prompt and there is no terminal,
and when --prefer copy would move different versions of one file from several
copies into the harness (only the last would survive).

relink also refreshes the links in each copy's worktrees. Files a worktree
holds as its own copies are left alone. --dry-run prints one "would <verb>
<object>" line per planned change on stdout (notes stay on stderr), and with
--prefer copy fails on the same conflict relink would. A copy that fails while
relinking is reported, the rest are still relinked, and relink then exits
non-zero.`,
		Example: `  mws relink
  mws relink --dry-run
  mws relink --prefer harness`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if f.prefer, err = parseResolution(prefer); err != nil {
				return err
			}
			return runRelink(newOutput(cmd, false), f)
		},
	}
	cmd.Flags().StringVar(&prefer, "prefer", "", "resolve diverged files without asking: copy, harness or skip")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print what would be linked and repaired; change nothing")
	return cmd
}

// divergence is a top-level regular file of a working copy whose name the
// harness also holds as a regular file.
type divergence struct {
	name, copyPath, harnessPath string
	identical                   bool
}

// relinkPlan is what relink would do in one working copy.
type relinkPlan struct {
	copyDir     string
	divergences []divergence
	links       []string // harness links to create or repoint
	worktrees   []string // worktree roots whose links are refreshed
}

func (p relinkPlan) name() string { return filepath.Base(p.copyDir) }

// scanRelink inspects the copy at peer without changing anything.
func scanRelink(metaRoot, copyDir string, policy config.WorktreePolicy) (relinkPlan, error) {
	p := relinkPlan{copyDir: copyDir}
	harnessRoot := filepath.Join(metaRoot, project.HarnessDirName)
	entries, err := os.ReadDir(copyDir)
	if err != nil {
		return p, err
	}
	for _, e := range entries {
		d := divergence{name: e.Name(), copyPath: filepath.Join(copyDir, e.Name()), harnessPath: filepath.Join(harnessRoot, e.Name())}
		if !isRegularFile(d.copyPath) || !isRegularFile(d.harnessPath) {
			continue
		}
		if d.identical, err = filesEqual(d.copyPath, d.harnessPath); err != nil {
			return p, err
		}
		p.divergences = append(p.divergences, d)
	}
	if p.links, err = project.PendingHarnessLinks(metaRoot, copyDir); err != nil {
		return p, err
	}
	if p.worktrees, err = worktree.Roots(copyDir, policy); err != nil {
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

func runRelink(o output, f relinkFlags) error {
	r := o.reporter()
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

	copies, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if len(copies) == 0 {
		r.Info("No working copies to relink.")
		return nil
	}

	plans := make([]relinkPlan, 0, len(copies))
	for _, copyDir := range copies {
		p, err := scanRelink(ws.MetaRoot, copyDir, policy)
		if err != nil {
			return fmt.Errorf("inspect %s: %w; nothing was changed", filepath.Base(copyDir), err)
		}
		plans = append(plans, p)
	}
	preflight := checkRelink(plans, f.prefer)
	if f.dryRun {
		for _, p := range plans {
			if err := printRelinkPlan(o.stdout, r, p, f.prefer); err != nil {
				return err
			}
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
		return clierr.Wrapf(joined, "relink failed in %d of %d working copies; the others were relinked:\n%w", len(errs), len(copies), joined)
	}
	return nil
}

// checkRelink refuses, before anything changes, a run that would need a
// prompt without a terminal, or whose --prefer copy would move different
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
			"mws relink --prefer harness   (keep the harness files)", "mws relink --prefer skip   (leave the copies' files)", "mws relink --prefer copy   (move the copies' files into the harness)")
	}
	if prefer != takeCopy {
		return nil
	}
	for _, name := range names {
		versions := diverged[name]
		for _, v := range versions[1:] {
			same, err := filesEqual(versions[0].copyPath, v.copyPath)
			if err != nil {
				return clierr.Failuref("compare %s with %s: %w", versions[0].copyPath, v.copyPath, err)
			}
			if !same {
				copies := make([]string, len(versions))
				for i, d := range versions {
					copies[i] = filepath.Base(filepath.Dir(d.copyPath))
				}
				return usagef("--prefer copy would move %d different versions of %s (from %s) into the harness, and only the last would survive; nothing was changed. Reconcile them by hand, or run relink in one copy's favor with --prefer harness after copying its file into %s",
					len(versions), name, strings.Join(copies, ", "), filepath.Dir(versions[0].harnessPath))
			}
		}
	}
	return nil
}

// printRelinkPlan writes one "would <verb> <object>" line per change relink
// would make in one copy to out. What relink would only ask about, leave
// alone, or has nothing to do for goes to r.
func printRelinkPlan(out io.Writer, r Reporter, p relinkPlan, prefer resolution) error {
	would := func(verb, object string) error {
		_, err := fmt.Fprintf(out, "would %s %s/%s\n", verb, p.name(), object)
		return err
	}
	for _, d := range p.divergences {
		var err error
		switch {
		case d.identical:
			err = would("remove", d.name)
		case prefer == resolveAsk:
			r.Info(fmt.Sprintf("%s: %s differs from the harness copy; relink would ask (or use --prefer)", p.name(), d.name))
		case prefer == takeCopy:
			err = would("move", d.name+" into the harness")
		case prefer == keepHarness:
			err = would("discard", d.name)
		default:
			r.Info(fmt.Sprintf("%s: %s differs from the harness copy; --prefer skip leaves it", p.name(), d.name))
		}
		if err != nil {
			return err
		}
	}
	for _, n := range p.links {
		if err := would("link", n); err != nil {
			return err
		}
	}
	for _, root := range p.worktrees {
		if err := would("relink worktree", filepath.Base(root)); err != nil {
			return err
		}
	}
	if len(p.divergences) == 0 && len(p.links) == 0 && len(p.worktrees) == 0 {
		r.Info(fmt.Sprintf("%s: nothing to change", p.name()))
	}
	return nil
}

// applyRelink carries out p: settles its divergences, links the harness and
// refreshes its worktrees. A failing worktree is reported and the rest are
// still refreshed.
func applyRelink(r Reporter, metaRoot string, p relinkPlan, policy config.WorktreePolicy, choose chooser) error {
	if err := applyDivergences(r, p, choose); err != nil {
		return fmt.Errorf("divergence repair: %w", err)
	}
	linked, err := project.LinkHarnessIntoWorkingCopy(metaRoot, p.copyDir)
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
		if err := worktree.Relink(metaRoot, p.copyDir, root, policy); err != nil {
			r.Fail(fmt.Sprintf("%s: worktree %s: %v", p.name(), filepath.Base(root), err))
			errs = append(errs, fmt.Errorf("worktree %s: %w", filepath.Base(root), err))
			continue
		}
		r.OK(fmt.Sprintf("%s: relinked worktree %s", p.name(), filepath.Base(root)))
	}
	return clierr.Join(errs...)
}

// chooser settles one diverged file of the copy called copyName.
type chooser func(copyName, fileName string) (resolution, error)

// chooseResolution answers with prefer when it is set, and otherwise asks on
// the terminal (checkRelink has already refused when there is none).
func chooseResolution(prefer resolution) chooser {
	return func(copyName, fileName string) (resolution, error) {
		if prefer != resolveAsk {
			return prefer, nil
		}
		return huhDivergencePrompt(copyName, fileName)
	}
}

// repairDivergedFiles settles the divergences of the copy at copyDir: identical
// content is auto-resolved by removing the copy's file
// (LinkHarnessIntoWorkingCopy then recreates the symlink), diverged content
// goes through choose.
func repairDivergedFiles(r Reporter, metaRoot, copyDir string, choose chooser) error {
	p, err := scanRelink(metaRoot, copyDir, config.WorktreePolicy{})
	if err != nil {
		return err
	}
	return applyDivergences(r, p, choose)
}

func applyDivergences(r Reporter, p relinkPlan, choose chooser) error {
	for _, d := range p.divergences {
		res := keepHarness
		if !d.identical {
			var err error
			if res, err = choose(p.name(), d.name); err != nil {
				return err
			}
		}
		switch res {
		case takeCopy:
			if err := moveFile(d.copyPath, d.harnessPath); err != nil {
				return fmt.Errorf("move %s -> %s: %w", d.copyPath, d.harnessPath, err)
			}
			r.OK(fmt.Sprintf("%s: moved the copy's %s into the harness", p.name(), d.name))
		case keepHarness:
			if err := os.Remove(d.copyPath); err != nil {
				return err
			}
			if d.identical {
				r.OK(fmt.Sprintf("%s: %s identical to the harness, removed the copy's file", p.name(), d.name))
			} else {
				r.OK(fmt.Sprintf("%s: discarded the copy's %s, kept the harness version", p.name(), d.name))
			}
		case resolveSkip:
			r.Warn(fmt.Sprintf("%s: %s left diverged", p.name(), d.name))
		case resolveAsk:
			return fmt.Errorf("%s: no resolution chosen", d.name)
		}
	}
	return nil
}

func huhDivergencePrompt(copyName, fileName string) (resolution, error) {
	choice := resolveSkip
	err := runField(huh.NewSelect[resolution]().
		Title(fmt.Sprintf("%s: %s differs from the harness. Which to keep?", copyName, fileName)).
		Options(
			huh.NewOption("Copy (move it into the harness, other copies will link to it)", takeCopy),
			huh.NewOption("Harness (discard the copy's edits, restore the symlink)", keepHarness),
			huh.NewOption("Skip (leave diverged)", resolveSkip),
		).
		Value(&choice))
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
