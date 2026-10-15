package commands

import (
	"bytes"
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
	"github.com/sustinbebustin/mws/internal/project"
)

const envLong = `env moves env files between env staging (<meta>/.envs/<repo>/<source>) and
working copies (<copy>/<repo>/<target>), following the [[repos.envs]] mappings
in .mws.toml and the [[optional_repos.envs]] of every optional repo present in
the copy.

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
		Short: "Copy staged env files into a working copy (overwrites)",
		Long: `sync copies each staged env file into its target in the working copy,
overwriting it, so the copy is back on the staged defaults. --all syncs every
working copy. A missing staged source is skipped with a warning; any file that
fails to copy makes the command exit 1 after the rest are done.

Worktrees are not refreshed: a worktree gets copies of the working copy's env
targets when it is created and keeps them. Run from inside a worktree, sync
updates its working copy, not the worktree.

--dry-run prints one "would copy <source> -> <target>" line per file and
changes nothing. --json prints
{"dry_run", "copies": [{"copy", "path", "files": [{"repo", "from", "to", "status", "error"}]}]}
with status copied, would_copy, skipped or failed.`,
		Example: `  mws env sync
  mws env sync feature-x --dry-run
  mws env sync --all --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvSync(newOutput(cmd, *jsonOut), copyArg(args), all, dryRun)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "sync every working copy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the files that would be copied; change nothing")
	return cmd
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

func runEnvSync(o output, name string, all, dryRun bool) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	copies, err := envTargets(ws, name, all, "mws env sync")
	if err != nil {
		return o.fail(err)
	}
	r := o.reporter()
	res := envRunResult{DryRun: dryRun, Copies: make([]envCopyResult, 0, len(copies))}
	for _, c := range copies {
		res.Copies = append(res.Copies, renderEnv(r, ws.MetaRoot, cfg, c, dryRun))
	}
	if err := res.print(o, ws.MetaRoot); err != nil {
		return err
	}
	return res.failure("mws env sync")
}

// renderEnv is the render path: it writes every staged env file of the
// working copy at copyPath to its target in the copy, overwriting it. clone,
// include, restore and `env sync` all materialise env files through it.
// Under dryRun it changes nothing and reports each file as would_copy.
// Progress and per-file failures go to r; the result says what happened to
// each file.
func renderEnv(r Reporter, metaRoot string, cfg *config.Config, copyPath string, dryRun bool) envCopyResult {
	files := envFilesOf(r, metaRoot, cfg, copyPath)
	moves := make([]envMove, len(files))
	for i, f := range files {
		moves[i] = envMove{repo: f.repo, from: f.staged, to: f.live}
	}
	return envCopyResult{
		Copy:  filepath.Base(copyPath),
		Path:  copyPath,
		Files: moveEnvFiles(r, metaRoot, moves, dryRun),
	}
}

func newEnvStageCmd(jsonOut *bool) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "stage [copy]",
		Short: "Capture a working copy's env files into staging (overwrites)",
		Long: `stage is the inverse of sync: it copies each env target of the working copy
over its staged source, so the copy's values become the default that future
clones and syncs reproduce. A missing file in the copy is skipped with a
warning; any file that fails to copy makes the command exit 1 after the rest
are done.

stage refuses (exit 3) when run inside a worktree: it captures the working
copy's files, not the worktree's.

--dry-run prints one "would copy <source> -> <target>" line per file and
changes nothing. --json prints the same object as sync, with from the copy's
file and to the staged one.`,
		Example: `  mws env stage
  mws env stage feature-x --dry-run`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvStage(newOutput(cmd, *jsonOut), copyArg(args), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the files that would be staged; change nothing")
	return cmd
}

func runEnvStage(o output, name string, dryRun bool) error {
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
	for _, c := range copies {
		files := envFilesOf(r, ws.MetaRoot, cfg, c)
		moves := make([]envMove, len(files))
		for i, f := range files {
			moves[i] = envMove{repo: f.repo, from: f.live, to: f.staged}
		}
		res.Copies = append(res.Copies, envCopyResult{Copy: filepath.Base(c), Path: c, Files: moveEnvFiles(r, ws.MetaRoot, moves, dryRun)})
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
		Long: `diff compares each env target of the working copy (or of every copy with
--all) with its staged source and prints one line per file on stdout:
"<state> <target> <- <source>", where state is same, differs, missing in copy
or missing in staging (also when both are missing). It changes nothing.

Exits 0 when every file is the same, 1 otherwise. --json prints
{"same", "copies": [{"copy", "path", "files": [{"repo", "source", "target", "state"}]}]}
with state same, differs, missing_in_copy or missing_in_staging.`,
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
	Repo   string   `json:"repo"`
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
	for _, c := range copies {
		dc := envDiffCopy{Copy: filepath.Base(c), Path: c, Files: []envDiffFile{}}
		for _, f := range envFilesOf(r, ws.MetaRoot, cfg, c) {
			state, err := compareEnvFile(f)
			if err != nil {
				return o.fail(clierr.Failuref("compare %s with %s: %w; nothing was changed", f.live, f.staged, err))
			}
			res.Same = res.Same && state == envSame
			dc.Files = append(dc.Files, envDiffFile{Repo: f.repo, Source: f.staged, Target: f.live, State: state})
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

// compareEnvFile reads both sides of f. A missing staged source wins over a
// missing copy file: without a source there is nothing to sync.
func compareEnvFile(f envFile) (envState, error) {
	staged, err := os.ReadFile(f.staged)
	if errors.Is(err, fs.ErrNotExist) {
		return envMissingInStaging, nil
	}
	if err != nil {
		return "", err
	}
	live, err := os.ReadFile(f.live)
	if errors.Is(err, fs.ErrNotExist) {
		return envMissingInCopy, nil
	}
	if err != nil {
		return "", err
	}
	if bytes.Equal(staged, live) {
		return envSame, nil
	}
	return envDiffers, nil
}

// envFile is one env mapping of a working copy: the staged file under
// <meta>/.envs/ and the live file in the copy, both absolute.
type envFile struct {
	repo, staged, live string
}

// envFilesOf lists the env files of the working copy at copyPath: the
// mappings of every default repo and of every optional repo the copy holds. A
// default repo missing from the copy is skipped with a warning, so its env
// files never create the repo's directory.
func envFilesOf(r Reporter, metaRoot string, cfg *config.Config, copyPath string) []envFile {
	var repos []config.Repo
	for _, repo := range cfg.Repos {
		if _, err := os.Stat(filepath.Join(copyPath, repo.Folder)); err != nil {
			if len(repo.Envs) > 0 {
				r.Warn(fmt.Sprintf("%s: %s is not in the copy, skipping its env files", filepath.Base(copyPath), repo.Folder))
			}
			continue
		}
		repos = append(repos, repo)
	}
	repos = append(repos, presentRepos(copyPath, cfg.OptionalRepos)...)

	staging := envStagingDir(metaRoot)
	var files []envFile
	for _, repo := range repos {
		for _, env := range repo.Envs {
			files = append(files, envFile{
				repo:   repo.Folder,
				staged: filepath.Join(staging, repo.Folder, env.Source),
				live:   filepath.Join(copyPath, repo.Folder, env.Target),
			})
		}
	}
	return files
}

// envMove is one file copy of an env subcommand.
type envMove struct {
	repo, from, to string
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
	Repo   string `json:"repo"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// moveEnvFiles copies each move's from onto its to, or under dryRun only
// reports it. A missing from is skipped with a warning; a failed copy is
// reported and the rest still run.
func moveEnvFiles(r Reporter, metaRoot string, moves []envMove, dryRun bool) []envFileResult {
	out := make([]envFileResult, 0, len(moves))
	for _, m := range moves {
		res := envFileResult{Repo: m.repo, From: m.from, To: m.to}
		from, to := relToMeta(metaRoot, m.from), relToMeta(metaRoot, m.to)
		switch _, err := os.Stat(m.from); {
		case err != nil:
			res.Status = envSkipped
			r.Warn(fmt.Sprintf("%s missing, skipping", from))
		case dryRun:
			res.Status = envWouldCopy
		default:
			if err := atomicfile.Copy(m.from, m.to); err != nil {
				res.Status, res.Error = envFailed, err.Error()
				r.Fail(fmt.Sprintf("%s -> %s: %v", from, to, err))
				break
			}
			res.Status = envCopied
			r.OK(fmt.Sprintf("%s -> %s", from, to))
		}
		out = append(out, res)
	}
	return out
}

// relToMeta shows p relative to the meta root when it lies inside it.
func relToMeta(metaRoot, p string) string {
	if rel, err := filepath.Rel(metaRoot, p); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return p
}

// envCopyResult is one working copy's part of an env sync or stage result.
type envCopyResult struct {
	Copy  string          `json:"copy"`
	Path  string          `json:"path"`
	Files []envFileResult `json:"files"`
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
			for _, f := range c.Files {
				if f.Status != envWouldCopy {
					continue
				}
				if _, err := fmt.Fprintf(w, "would copy %s -> %s\n", relToMeta(metaRoot, f.From), relToMeta(metaRoot, f.To)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// failure is the exit-1 error when any file failed to copy, or nil.
func (res envRunResult) failure(invocation string) error {
	failed := 0
	for _, c := range res.Copies {
		for _, f := range c.Files {
			if f.Status == envFailed {
				failed++
			}
		}
	}
	if failed == 0 {
		return nil
	}
	return clierr.Failuref("%d env %s failed to copy (see the [FAIL] lines above); the other files were copied. Fix the cause and rerun `%s`",
		failed, plural(failed, "file", "files"), invocation)
}
