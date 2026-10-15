package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RenderSuffix ends every `[[renders]]` source. A distinct suffix keeps the
// source (symlinked like any harness entry) and the rendered target apart.
const RenderSuffix = ".render"

// MarkerPrefix starts the name of every mws marker, a file mws writes at a
// working copy or worktree root (the worktree marker, the port lease).
const MarkerPrefix = ".mws-"

// Render maps a `.render` source under the harness dir to a target file at
// the working copy root.
type Render struct {
	// Source is relative to the harness dir and ends in RenderSuffix.
	Source string `toml:"source"`
	// Target is a single path segment at the working copy root.
	Target string `toml:"target"`
}

// validateRenders checks each `[[renders]]` entry: the source is a local
// path under the harness dir ending in RenderSuffix, and the target is one
// path-safe segment, not an mws marker, and not shared with another entry.
func validateRenders(renders []Render) error {
	seen := map[string]int{}
	for i, r := range renders {
		stem, ok := strings.CutSuffix(r.Source, RenderSuffix)
		if !ok || stem == "" || strings.HasSuffix(stem, "/") {
			return fmt.Errorf("renders[%d].source is %q; must name a file ending in %q (e.g. %q)", i, r.Source, RenderSuffix, ".mcp.json"+RenderSuffix)
		}
		if !filepath.IsLocal(r.Source) {
			return fmt.Errorf("renders[%d].source is %q; must be a relative path inside the harness dir", i, r.Source)
		}
		key := fmt.Sprintf("renders[%d].target", i)
		if r.Target == "" || r.Target == "." || r.Target == ".." || strings.ContainsAny(r.Target, "/\\\x00") {
			return fmt.Errorf("%s is %q; must be a single file name at the working copy root (e.g. %q)", key, r.Target, ".mcp.json")
		}
		if strings.HasPrefix(r.Target, MarkerPrefix) {
			return fmt.Errorf("%s is %q; names starting with %q are reserved for mws markers", key, r.Target, MarkerPrefix)
		}
		if prev, dup := seen[r.Target]; dup {
			return fmt.Errorf("%s is %q, already the target of renders[%d]; each target takes one source", key, r.Target, prev)
		}
		seen[r.Target] = i
	}
	return nil
}
