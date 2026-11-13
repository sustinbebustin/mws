#!/usr/bin/env bash
# @-mention file picker for mws working copies.
#
# A working copy is not a git repo: its native repos are nested clones and the
# harness is symlinked in from <meta>/.mws/. Claude Code's built-in picker
# resolves those symlinks and offers ../.mws/... paths. This lists every file
# under the project dir with copy-relative paths, honoring ignore rules:
#   - .ignore / .rgignore at the root
#   - .gitignore at every level, applied even outside a git repo via
#     --no-require-git, so the nested repos and the non-repo root are covered.
# --follow descends the harness symlinks (CLAUDE.md, .claude/, .workspace/, ...).
# Without rg it falls back to find, which skips only .git/ and node_modules/.
#
# Contract: reads {"query": "..."} on stdin, writes up to 15 newline-separated
# copy-relative paths to stdout. Needs jq.
set -eu

if ! command -v jq >/dev/null 2>&1; then
  echo "file-suggestion.sh: jq not found on PATH; install jq to enable the @-mention picker." >&2
  exit 1
fi
query=$(jq -r '.query // ""')
cd "${CLAUDE_PROJECT_DIR:-$PWD}"

list_files() {
  if command -v rg >/dev/null 2>&1; then
    rg --files --hidden --follow --no-require-git --glob '!**/.git/' . 2>/dev/null
  else
    find -L . \( -name .git -o -name node_modules \) -prune -o -type f -print 2>/dev/null
  fi | sed 's#^\./##'
}

files=$(list_files)
# Derive selectable directory entries (trailing slash) from the file paths, so a
# native repo folder can be picked directly, not just its contents.
dirs=$(printf '%s\n' "$files" | awk -F/ '
  {
    p = ""
    for (i = 1; i < NF; i++) {
      p = (i == 1 ? $i : p "/" $i)
      print p "/"
    }
  }' | sort -u)
all=$(printf '%s\n%s\n' "$files" "$dirs" | sed '/^$/d')

# Hide top-level hidden entries (.claude/, root dotfiles, ...) unless the query
# starts with a dot. Only the first path segment is judged, so <repo>/.github/...
# stays visible. Everyday targets stay searchable ungated: .workspace/,
# .scratch/, and .claude/{skills,agents,rules}/.
gate_filter() {
  case "$query" in
    .*) cat ;;
    *) awk '
         !/^\./ ||
         index($0, ".workspace/") == 1 ||
         index($0, ".scratch/") == 1 ||
         index($0, ".claude/skills/") == 1 ||
         index($0, ".claude/agents/") == 1 ||
         index($0, ".claude/rules/") == 1
       ' ;;
  esac
}

if [ -z "$query" ]; then
  printf '%s\n' "$all" | gate_filter | head -n 15
  exit 0
fi

# Case-insensitive substring match. Rank tiers (dir with a name match first, so
# `@<repo>` offers the folder at the top), then shallower paths first. The query
# reaches awk through the environment: -v would interpret backslash escapes.
printf '%s\n' "$all" \
  | grep -iF -- "$query" \
  | QUERY="$query" awk '
      BEGIN { q = tolower(ENVIRON["QUERY"]) }
      {
        path = $0
        isdir = (substr(path, length(path), 1) == "/")
        name = isdir ? substr(path, 1, length(path) - 1) : path
        n = split(name, parts, "/")
        bm = index(tolower(parts[n]), q) > 0
        if (isdir && bm) rank = 0
        else if (!isdir && bm) rank = 1
        else if (!isdir) rank = 2
        else rank = 3
        print rank "\t" split(path, _, "/") "\t" path
      }' \
  | sort -s -k1,1n -k2,2n \
  | cut -f3- \
  | gate_filter \
  | head -n 15
