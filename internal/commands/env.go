package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
	"github.com/sustinbebustin/mws/internal/worktree"
)

const envLong = `env moves env files between env staging (<meta>/.envs/<repo>/<source>) and
working copies (<copy>/<repo>/<target>), following the [[repos.envs]] mappings
in .mws.toml and the [[optional_repos.envs]] of every optional repo present in
the copy. Into a copy, files are rendered: each ${VAR} the copy's Port lease
exports (slot 0 without a lease) is substituted, every other byte is copied
as is, and an unknown ${UPPER_CASE} name is left in place with a warning.
sync and diff also cover each [[renders]] target, rendered from its .render
source under <meta>/.mws/ to the copy root.

Each subcommand defaults to the working copy containing the current directory.
--json prints one JSON object, or {"error": {"code", "message"}} on a usage or
refusal error.`

func newEnvCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Sync, stage and diff env files between staging and working copies",
		Long:  envLong,
	}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "print results as JSON")
	cmd.AddCommand(newEnvSyncCmd(&jsonOut), newEnvStageCmd(&jsonOut), newEnvDiffCmd(&jsonOut))
	return cmd
}

func newEnvSyncCmd(jsonOut *bool) *cobra.Command {
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "sync [copy]",
		Short: "Render staged env files into a working copy (overwrites)",
		Long: `sync renders each staged env file and each [[renders]] source at the working
copy's Port slot and writes it to its target in the copy, overwriting it, so
the copy is back on the staged defaults. --all syncs every working copy. A
missing source is skipped with a warning; any file that fails to copy makes
the command exit 1 after the rest are done.

Each worktree of the copy gets its carried files (the lease, the env targets,
the [[renders]] targets) refreshed too, but only those whose content still
matches what mws carried there; one edited in the worktree is left alone with
a warning. Run from inside a worktree, sync acts on its working copy.

After each copy is rendered, the root [[setup]] commands run at the copy root
with the copy's export set in the environment. --setup runs them without
prompting and --no-setup skips them; without a terminal one of the two is
required when [[setup]] is configured. --dry-run runs no setup.

--dry-run prints one "would copy <source> -> <target>" line per file and
changes nothing. --json prints
{"dry_run", "copies": [{"copy", "path", "files": [{"repo", "from", "to", "status", "error"}], "error", "setup_error"}]}
with status copied, would_copy, skipped or failed; repo is absent for a
[[renders]] target. A copy's error is set when its worktrees were not fully
refreshed, setup_error when a root [[setup]] command failed.`,
		Example: `  mws env sync
  mws env sync feature-x --dry-run
  mws env sync --all --json --no-setup`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := newOutput(cmd, *jsonOut)
			choice, err := setupChoiceFlags(cmd)
			if err != nil {
				return o.fail(err)
			}
			return runEnvSync(cmd.Context(), o, copyArg(args), all, dryRun, choice)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "sync every working copy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the files that would be copied; change nothing")
	addSetupFlags(cmd)
	return cmd
}

// addSetupFlags adds the --setup / --no-setup pair of a render command, which
// setupChoiceFlags reads.
func addSetupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("setup", false, "run the root [[setup]] commands without prompting")
	cmd.Flags().Bool("no-setup", false, "skip the root [[setup]] commands without prompting")
}

// copyArg is the optional [copy] positional argument, or "" when absent.
func copyArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return ""
}

// envTargets resolves the copies an env subcommand acts on: every working
// copy with all, else the named copy (or the one containing cwd).
func envTargets(ws *project.Workspace, name string, all bool, invocation string) ([]string, error) {
	if all {
		if name != "" {
			return nil, usagef("give a copy name or --all, not both; see `%s --help`", invocation)
		}
		copies, err := ws.EnumerateCopies()
		if err != nil {
			return nil, clierr.Failuref("list working copies: %w", err)
		}
		return copies, nil
	}
	target, err := resolveCopy(ws, name, invocation+" <copy>")
	if err != nil {
		return nil, err
	}
	return []string{target}, nil
}

func runEnvSync(ctx context.Context, o output, name string, all, dryRun bool, choice setupChoice) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	copies, err := envTargets(ws, name, all, "mws env sync")
	if err != nil {
		return o.fail(err)
	}
	invocation := "mws env sync"
	switch {
	case all:
		invocation += " --all"
	case name != "":
		invocation += " " + name
	}
	mode, err := resolveRenderMode(cfg, dryRun, choice, invocation)
	if err != nil {
		return o.fail(err)
	}
	r := o.reporter()
	res := envRunResult{DryRun: dryRun, Copies: make([]envCopyResult, 0, len(copies))}
	var setupErrs []error
	for _, c := range copies {
		cr, err := workingCopy{metaRoot: ws.MetaRoot, cfg: cfg, path: c}.render(ctx, r, mode, o.stderr)
		res.Copies = append(res.Copies, cr)
		if err != nil {
			setupErrs = append(setupErrs, err)
		}
	}
	if err := res.print(o, ws.MetaRoot); err != nil {
		return err
	}
	if err := errors.Join(append([]error{res.failure(invocation)}, setupErrs...)...); err != nil {
		return clierr.Failuref("%w", err)
	}
	return nil
}

// rootSetupItems are the root [[setup]] commands, run at the copy root.
func rootSetupItems(cfg *config.Config) []setupItem {
	return collectSetup([]config.Repo{{Folder: ".", Setup: cfg.Setup}})
}

// renderMode is how far a render of a working copy goes.
type renderMode int

const (
	renderPreview  renderMode = iota // --dry-run: report each file as would_copy; change nothing
	renderFiles                      // write the rendered files; skip the root [[setup]]
	renderAndSetup                   // write the rendered files, then run the root [[setup]]
)

// resolveRenderMode decides, before anything changes, how far a render goes:
// a preview under dryRun, else whether the root [[setup]] commands follow it,
// from --setup / --no-setup or by prompting. Without a terminal and without a
// flag it is a usage error naming invocation with each flag, whenever
// [[setup]] is configured.
func resolveRenderMode(cfg *config.Config, dryRun bool, choice setupChoice, invocation string) (renderMode, error) {
	if dryRun {
		return renderPreview, nil
	}
	items := rootSetupItems(cfg)
	if err := requireSetupChoice(choice, items, invocation); err != nil {
		return renderFiles, err
	}
	run, err := confirmSetup(choice, items)
	if err != nil || !run {
		return renderFiles, err
	}
	return renderAndSetup, nil
}

// workingCopy is a working copy as the render path sees it: the copy at path
// in the meta workspace at metaRoot, configured by cfg.
type workingCopy struct {
	metaRoot string
	cfg      *config.Config
	path     string
}

func (c workingCopy) name() string { return filepath.Base(c.path) }

// render renders the copy through renderEnv and then, under renderAndSetup,
// runs the root [[setup]] commands at the copy root with the copy's export
// set, streaming their output to setupOut. It is the whole of a render for
// the commands that clone nothing (`env sync`, `ports claim`, `ports
// release`); clone and include run the repos' setup between the two halves
// and call them separately. The error names the setup commands that failed,
// and the result carries it as SetupError; the render result stands either
// way.
func (c workingCopy) render(ctx context.Context, r Reporter, mode renderMode, setupOut io.Writer) (envCopyResult, error) {
	res := c.renderEnv(r, mode == renderPreview)
	if mode != renderAndSetup {
		return res, nil
	}
	if err := c.runRootSetup(ctx, r, setupOut); err != nil {
		err = fmt.Errorf("%s: %w", res.Copy, err)
		res.SetupError = err.Error()
		return res, err
	}
	return res, nil
}

// runRootSetup runs the root [[setup]] commands at the copy root with the
// copy's export set, streaming their output to setupOut. The error names the
// commands that failed.
func (c workingCopy) runRootSetup(ctx context.Context, r Reporter, setupOut io.Writer) error {
	items := rootSetupItems(c.cfg)
	if len(items) == 0 {
		return nil
	}
	exports, err := c.exports()
	if err != nil {
		return fmt.Errorf("no root setup command ran: %w", err)
	}
	if failed := runSetup(ctx, r, c.path, exports, items, setupOut, setupOut); len(failed) > 0 {
		return fmt.Errorf("%d root setup %s failed: %s; the env files were rendered. Fix the cause and rerun with --setup",
			len(failed), plural(len(failed), "command", "commands"), strings.Join(failed, "; "))
	}
	return nil
}

// renderEnv is the render path: it rewrites the copy's Port lease from the
// current config (refreshLease), renders every staged env file of the copy at
// its Port slot and writes it to its target in the copy, overwriting it, then
// refreshes the carried copies of those files and of the lease in
// each of the copy's worktrees (worktree.Rerender). clone, include, restore
// and `env sync` all materialise env files through it. Under dryRun it
// changes nothing and reports each file as would_copy. Progress,
// unsubstituted placeholders and per-file failures go to r; the result says
// what happened to each file.
func (c workingCopy) renderEnv(r Reporter, dryRun bool) envCopyResult {
	metaRoot, copyPath := c.metaRoot, c.path
	files := c.renderedFiles(r)
	res := envCopyResult{Copy: c.name(), Path: copyPath, Files: make([]envFileResult, 0, len(files))}
	readExports := c.refreshLease
	if dryRun {
		readExports = c.exports
	}
	exports, err := readExports()
	if err != nil {
		r.Fail(fmt.Sprintf("%s: %v; no env file of the copy was rendered", res.Copy, err))
		for _, f := range files {
			res.Files = append(res.Files, envFileResult{Repo: f.repo, From: f.source, To: f.target, Status: envFailed, Error: err.Error()})
		}
		return res
	}
	for _, f := range files {
		res.Files = append(res.Files, renderFile(r, metaRoot, f, exports, dryRun))
	}
	if dryRun {
		return res
	}
	rels := []string{ports.LeaseFile}
	for _, f := range files {
		if rel, err := filepath.Rel(copyPath, f.target); err == nil && filepath.IsLocal(rel) {
			rels = append(rels, filepath.ToSlash(rel))
		}
	}
	if err := worktree.Rerender(r, copyPath, c.cfg.WorktreePolicy(), rels); err != nil {
		res.Error = err.Error()
		r.Fail(fmt.Sprintf("%s: refreshing its worktrees: %v; the copy's own files were rendered", res.Copy, err))
	}
	return res
}

// exports is the export set of the copy at the slot its Port lease records,
// slot 0 when it has none.
func (c workingCopy) exports() (ports.Exports, error) {
	slot, err := ports.ReadSlot(c.path)
	if err != nil {
		return ports.Exports{}, err
	}
	return ports.NewExports(c.cfg, c.metaRoot, c.name(), slot), nil
}

// refreshLease rewrites the Port lease of a copy holding a slot from the
// current config, under the meta-root lock, so a lease written before a
// [ports] or [vars] edit stops exporting the old values. It returns the copy's
// export set; a copy on the default ports has no lease and is left alone.
func (c workingCopy) refreshLease() (ports.Exports, error) {
	unlock, err := ports.LockMeta(c.metaRoot)
	if err != nil {
		return ports.Exports{}, err
	}
	defer unlock()
	e, err := c.exports()
	if err != nil || e.Slot == 0 {
		return e, err
	}
	if err := ports.WriteLease(c.path, e); err != nil {
		return ports.Exports{}, err
	}
	return e, nil
}

// renderFile renders f's source with e onto its target, keeping the
// source's permissions, or under dryRun only reports it. A missing source is
// skipped with a warning; each ${UPPER_CASE} name e does not export is left
// in place and named in one warning for the file.
func renderFile(r Reporter, metaRoot string, f envFile, e ports.Exports, dryRun bool) envFileResult {
	res := envFileResult{Repo: f.repo, From: f.source, To: f.target}
	from, to := relToMeta(metaRoot, f.source), relToMeta(metaRoot, f.target)
	info, err := os.Stat(f.source)
	if err != nil {
		res.Status = envSkipped
		r.Warn(fmt.Sprintf("%s missing, skipping", from))
		return res
	}
	src, err := os.ReadFile(f.source)
	if err != nil {
		res.Status, res.Error = envFailed, err.Error()
		r.Fail(fmt.Sprintf("%s -> %s: %v", from, to, err))
		return res
	}
	out, unresolved := ports.Render(src, e)
	if len(unresolved) > 0 {
		r.Warn(fmt.Sprintf("%s: %s left unsubstituted, since this copy exports no such %s; fix the %s in %s or declare it under [ports] or [vars] in .mws.toml",
			to, placeholderList(unresolved), plural(len(unresolved), "variable", "variables"), plural(len(unresolved), "name", "names"), from))
	}
	if dryRun {
		res.Status = envWouldCopy
		return res
	}
	if err := atomicfile.Write(f.target, out, info.Mode().Perm()); err != nil {
		res.Status, res.Error = envFailed, err.Error()
		r.Fail(fmt.Sprintf("%s -> %s: %v", from, to, err))
		return res
	}
	res.Status = envCopied
	r.OK(fmt.Sprintf("%s -> %s", from, to))
	return res
}

// placeholderList spells names as ${NAME} placeholders, comma-separated.
func placeholderList(names []string) string {
	spelled := make([]string, len(names))
	for i, n := range names {
		spelled[i] = "${" + n + "}"
	}
	return strings.Join(spelled, ", ")
}

func newEnvStageCmd(jsonOut *bool) *cobra.Command {
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:   "stage [copy]",
		Short: "Capture a working copy's env files into staging (overwrites)",
		Long: `stage is the inverse of sync: it copies each env target of the working copy
over its staged source, so the copy's values become the default that future
clones and syncs reproduce. A missing file in the copy is skipped with a
warning; any file that fails to copy makes the command exit 1 after the rest
are done.

A staged source holding ${VAR} placeholders is skipped with a warning, so a
template is never baked to one copy's ports; the other files are still
staged and the command exits 0. --force stages the copy's rendered file over
it anyway. [[renders]] targets are never staged.

stage refuses (exit 3) when run inside a worktree: it captures the working
copy's files, not the worktree's.

--dry-run prints one "would copy <source> -> <target>" line per file and
changes nothing. --json prints the same object as sync, with from the copy's
file and to the staged one.`,
		Example: `  mws env stage
  mws env stage feature-x --dry-run
  mws env stage --force`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvStage(newOutput(cmd, *jsonOut), copyArg(args), force, dryRun)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "stage over sources holding ${VAR} placeholders too")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the files that would be staged; change nothing")
	return cmd
}

func runEnvStage(o output, name string, force, dryRun bool) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	if ws.Worktree != "" {
		return o.fail(clierr.Refusedf("env stage captures the working copy's env files, not the worktree %s's; nothing was staged. Run `cd %s && mws env stage` to stage the copy's files",
			filepath.Base(ws.Worktree), ws.WorkingCopy))
	}
	copies, err := envTargets(ws, name, false, "mws env stage")
	if err != nil {
		return o.fail(err)
	}
	r := o.reporter()
	res := envRunResult{DryRun: dryRun, Copies: make([]envCopyResult, 0, len(copies))}
	for _, path := range copies {
		c := workingCopy{metaRoot: ws.MetaRoot, cfg: cfg, path: path}
		exports, err := c.exports()
		if err != nil {
			return o.fail(clierr.Failuref("%s: %w; nothing was staged", c.name(), err))
		}
		files := c.envFiles(r)
		cr := envCopyResult{Copy: c.name(), Path: path, Files: make([]envFileResult, 0, len(files))}
		for _, f := range files {
			cr.Files = append(cr.Files, stageEnvFile(r, ws.MetaRoot, f, exports, force, dryRun))
		}
		res.Copies = append(res.Copies, cr)
	}
	if err := res.print(o, ws.MetaRoot); err != nil {
		return err
	}
	return res.failure("mws env stage")
}

func newEnvDiffCmd(jsonOut *bool) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "diff [copy]",
		Short: "Compare a working copy's env files with staging",
		Long: `diff compares each env and [[renders]] target of the working copy (or of
every copy with --all) with its source rendered at the copy's Port slot, the
bytes sync would write, and prints one line per file on stdout:
"<state> <target> <- <source>", where state is same, differs, missing in copy
or missing in staging (also when both are missing). It changes nothing.

Exits 0 when every file is the same, 1 otherwise. --json prints
{"same", "copies": [{"copy", "path", "files": [{"repo", "source", "target", "state"}]}]}
with state same, differs, missing_in_copy or missing_in_staging; repo is
absent for a [[renders]] target.`,
		Example: `  mws env diff
  mws env diff feature-x
  mws env diff --all --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvDiff(newOutput(cmd, *jsonOut), copyArg(args), all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "compare every working copy")
	return cmd
}

// envState is how a copy's env file compares with its staged source.
type envState string

const (
	envSame             envState = "same"
	envDiffers          envState = "differs"
	envMissingInCopy    envState = "missing_in_copy"
	envMissingInStaging envState = "missing_in_staging"
)

// label is the state as `env diff` prints it.
func (s envState) label() string { return strings.ReplaceAll(string(s), "_", " ") }

type envDiffFile struct {
	Repo   string   `json:"repo,omitempty"`
	Source string   `json:"source"`
	Target string   `json:"target"`
	State  envState `json:"state"`
}

type envDiffCopy struct {
	Copy  string        `json:"copy"`
	Path  string        `json:"path"`
	Files []envDiffFile `json:"files"`
}

func runEnvDiff(o output, name string, all bool) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	copies, err := envTargets(ws, name, all, "mws env diff")
	if err != nil {
		return o.fail(err)
	}
	r := o.reporter()
	res := struct {
		Same   bool          `json:"same"`
		Copies []envDiffCopy `json:"copies"`
	}{Same: true, Copies: make([]envDiffCopy, 0, len(copies))}
	for _, path := range copies {
		c := workingCopy{metaRoot: ws.MetaRoot, cfg: cfg, path: path}
		dc := envDiffCopy{Copy: c.name(), Path: path, Files: []envDiffFile{}}
		exports, err := c.exports()
		if err != nil {
			return o.fail(clierr.Failuref("%s: %w; nothing was compared", c.name(), err))
		}
		for _, f := range c.renderedFiles(r) {
			state, err := compareEnvFile(f, exports)
			if err != nil {
				return o.fail(clierr.Failuref("compare %s with %s: %w; nothing was changed", f.target, f.source, err))
			}
			res.Same = res.Same && state == envSame
			dc.Files = append(dc.Files, envDiffFile{Repo: f.repo, Source: f.source, Target: f.target, State: state})
		}
		res.Copies = append(res.Copies, dc)
	}
	if err := o.result(res, func(w io.Writer) error {
		for _, c := range res.Copies {
			for _, f := range c.Files {
				if _, err := fmt.Fprintf(w, "%-18s %s <- %s\n", f.State.label(), relToMeta(ws.MetaRoot, f.Target), relToMeta(ws.MetaRoot, f.Source)); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if !res.Same {
		return clierr.Failuref("env files differ from staging; run `mws env sync` to reset a copy to staging or `mws env stage` to keep its values")
	}
	return nil
}

// compareEnvFile compares f's target with its source rendered with e, the
// bytes a render would write. A missing source wins over a missing target:
// without a source there is nothing to render.
func compareEnvFile(f envFile, e ports.Exports) (envState, error) {
	src, err := os.ReadFile(f.source)
	if errors.Is(err, fs.ErrNotExist) {
		return envMissingInStaging, nil
	}
	if err != nil {
		return "", err
	}
	live, err := os.ReadFile(f.target)
	if errors.Is(err, fs.ErrNotExist) {
		return envMissingInCopy, nil
	}
	if err != nil {
		return "", err
	}
	if rendered, _ := ports.Render(src, e); bytes.Equal(rendered, live) {
		return envSame, nil
	}
	return envDiffers, nil
}

// envFile is one rendered file of a working copy: its source (a staged env
// file under <meta>/.envs/, or a `.render` file under the harness dir) and
// its target in the copy, both absolute. repo is empty for a `[[renders]]`
// target, which sits at the copy root.
type envFile struct {
	repo, source, target string
}

// envFiles lists the env files of the copy: the mappings of every default
// repo and of every optional repo the copy holds. A default repo missing from
// the copy is skipped with a warning, so its env files never create the
// repo's directory.
func (c workingCopy) envFiles(r Reporter) []envFile {
	var repos []config.Repo
	for _, repo := range c.cfg.Repos {
		if _, err := os.Stat(filepath.Join(c.path, repo.Folder)); err != nil {
			if len(repo.Envs) > 0 {
				r.Warn(fmt.Sprintf("%s: %s is not in the copy, skipping its env files", c.name(), repo.Folder))
			}
			continue
		}
		repos = append(repos, repo)
	}
	repos = append(repos, presentRepos(c.path, c.cfg.OptionalRepos)...)

	staging := envStagingDir(c.metaRoot)
	var files []envFile
	for _, repo := range repos {
		for _, env := range repo.Envs {
			files = append(files, envFile{
				repo:   repo.Folder,
				source: filepath.Join(staging, repo.Folder, env.Source),
				target: filepath.Join(c.path, repo.Folder, env.Target),
			})
		}
	}
	return files
}

// renderedFiles lists every file the render path writes into the copy: its
// env files, then each `[[renders]]` target at the copy root with its
// `.render` source under the harness dir.
func (c workingCopy) renderedFiles(r Reporter) []envFile {
	files := c.envFiles(r)
	harness := filepath.Join(c.metaRoot, project.HarnessDirName)
	for _, rd := range c.cfg.Renders {
		files = append(files, envFile{
			source: filepath.Join(harness, rd.Source),
			target: filepath.Join(c.path, rd.Target),
		})
	}
	return files
}

// Status values of one file in an env sync or stage result.
const (
	envCopied    = "copied"
	envWouldCopy = "would_copy"
	envSkipped   = "skipped"
	envFailed    = "failed"
)

// envFileResult is what happened to one file of an env sync or stage.
type envFileResult struct {
	Repo   string `json:"repo,omitempty"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// stageEnvFile copies f's target in the copy over its staged source, or
// under dryRun only reports it. A missing target is skipped with a warning.
// Unless force, so is a source holding ${VAR} placeholders that e renders:
// staging over it would bake this copy's slot into every copy.
func stageEnvFile(r Reporter, metaRoot string, f envFile, e ports.Exports, force, dryRun bool) envFileResult {
	res := envFileResult{Repo: f.repo, From: f.target, To: f.source}
	from, to := relToMeta(metaRoot, f.target), relToMeta(metaRoot, f.source)
	if _, err := os.Stat(f.target); err != nil {
		res.Status = envSkipped
		r.Warn(fmt.Sprintf("%s missing, skipping", from))
		return res
	}
	if !force && holdsPlaceholders(f.source, e) {
		res.Status = envSkipped
		r.Warn(fmt.Sprintf("%s holds ${VAR} placeholders, skipping so it stays a template; edit it by hand, or rerun with --force to stage %s's rendered values over it",
			to, from))
		return res
	}
	if dryRun {
		res.Status = envWouldCopy
		return res
	}
	if err := atomicfile.Copy(f.target, f.source); err != nil {
		res.Status, res.Error = envFailed, err.Error()
		r.Fail(fmt.Sprintf("%s -> %s: %v", from, to, err))
		return res
	}
	res.Status = envCopied
	r.OK(fmt.Sprintf("%s -> %s", from, to))
	return res
}

// holdsPlaceholders reports whether the staged source at path contains a
// ${VAR} placeholder: one e substitutes, or an unexported ${UPPER_CASE} name.
// An unreadable or missing source holds none.
func holdsPlaceholders(path string, e ports.Exports) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	out, unresolved := ports.Render(src, e)
	return len(unresolved) > 0 || !bytes.Equal(out, src)
}

// relToMeta shows p relative to the meta root when it lies inside it.
func relToMeta(metaRoot, p string) string {
	if rel, err := filepath.Rel(metaRoot, p); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return p
}

// envCopyResult is one working copy's part of an env sync or stage result.
// Error is set when refreshing the copy's worktrees failed, SetupError when
// a root [[setup]] command failed after the render.
type envCopyResult struct {
	Copy       string          `json:"copy"`
	Path       string          `json:"path"`
	Files      []envFileResult `json:"files"`
	Error      string          `json:"error,omitempty"`
	SetupError string          `json:"setup_error,omitempty"`
}

// envRunResult is the result of `mws env sync|stage`.
type envRunResult struct {
	DryRun bool            `json:"dry_run"`
	Copies []envCopyResult `json:"copies"`
}

// print writes the result as JSON under --json; otherwise only a dry run
// speaks on stdout, one "would copy" line per file.
func (res envRunResult) print(o output, metaRoot string) error {
	return o.result(res, func(w io.Writer) error {
		for _, c := range res.Copies {
			if err := writeWouldCopy(w, metaRoot, c.Files); err != nil {
				return err
			}
		}
		return nil
	})
}

// writeWouldCopy writes one "would copy <from> -> <to>" line per file a dry
// run would copy.
func writeWouldCopy(w io.Writer, metaRoot string, files []envFileResult) error {
	for _, f := range files {
		if f.Status != envWouldCopy {
			continue
		}
		if _, err := fmt.Fprintf(w, "would copy %s -> %s\n", relToMeta(metaRoot, f.From), relToMeta(metaRoot, f.To)); err != nil {
			return err
		}
	}
	return nil
}

// failure is the exit-1 error when any file failed to copy, or nil.
func (res envRunResult) failure(invocation string) error {
	failed, worktreesFailed := 0, 0
	for _, c := range res.Copies {
		for _, f := range c.Files {
			if f.Status == envFailed {
				failed++
			}
		}
		if c.Error != "" {
			worktreesFailed++
		}
	}
	var errs []error
	if failed > 0 {
		errs = append(errs, clierr.Failuref("%d env %s failed to copy (see the [FAIL] lines above); the other files were copied. Fix the cause and rerun `%s`",
			failed, plural(failed, "file", "files"), invocation))
	}
	if worktreesFailed > 0 {
		errs = append(errs, clierr.Failuref("the worktrees of %d working %s were not fully refreshed (see the [FAIL] lines above); the copies' own files were written. Fix the cause and rerun `%s`",
			worktreesFailed, plural(worktreesFailed, "copy", "copies"), invocation))
	}
	return errors.Join(errs...)
}
