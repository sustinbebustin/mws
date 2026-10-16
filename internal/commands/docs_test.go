package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// repoFile reads a file relative to the repository root.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// visibleCommands returns every command a user can see in --help, below the
// root, depth first. Hidden commands (the hook adapters) and cobra's help
// command are left out.
func visibleCommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "help" {
			continue
		}
		out = append(out, sub)
		out = append(out, visibleCommands(sub)...)
	}
	return out
}

// TestReadmeCommandTableQuotesEveryShortLine pins the README command table to
// the binary: each visible command has a row that names it and carries its
// Short line verbatim.
func TestReadmeCommandTableQuotesEveryShortLine(t *testing.T) {
	var rows []string
	for _, line := range strings.Split(repoFile(t, "README.md"), "\n") {
		if strings.HasPrefix(line, "| `mws ") {
			rows = append(rows, line)
		}
	}
	for _, c := range visibleCommands(NewRootCmd()) {
		name := c.CommandPath()
		found := false
		for _, row := range rows {
			if strings.HasPrefix(row, "| `"+name+"`") || strings.HasPrefix(row, "| `"+name+" ") {
				found = true
				if !strings.Contains(row, c.Short) {
					t.Errorf("README row for %q does not contain its Short line %q:\n%s", name, c.Short, row)
				}
				break
			}
		}
		if !found {
			t.Errorf("README command table has no row for %q", name)
		}
	}
}

// TestReadmeAndConfigDocNameEveryFlag keeps flags from going undocumented: a
// visible flag must appear as --name in the README or docs/config.md.
func TestReadmeAndConfigDocNameEveryFlag(t *testing.T) {
	docs := repoFile(t, "README.md") + repoFile(t, "docs/config.md")
	seen := map[string]bool{}
	for _, c := range visibleCommands(NewRootCmd()) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Name == "help" || seen[f.Name] {
				return
			}
			seen[f.Name] = true
			if !strings.Contains(docs, "--"+f.Name) {
				t.Errorf("flag --%s (first seen on %q) appears in neither README.md nor docs/config.md", f.Name, c.CommandPath())
			}
		})
	}
}

var retiredTerms = regexp.MustCompile("sync-env|stage-env|`?mws migrate|--prefer meta|--prefer peer|`mws list`|mws list\\b|ports sync")

// TestTeachingFilesUseNoRetiredNames covers every file that teaches the
// tool, except the ADRs and the changelog, which record history under the
// names of their day.
func TestTeachingFilesUseNoRetiredNames(t *testing.T) {
	files := []string{"README.md", "CLAUDE.md", "GLOSSARY.md", "docs/config.md", "skills/mws/SKILL.md"}
	skeletonFiles, err := filepath.Glob("../../skeleton/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := filepath.Glob("../../skeleton/.mws/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(skeletonFiles, hidden...) {
		rel, err := filepath.Rel("../..", f)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, rel)
	}
	for _, rel := range files {
		if m := retiredTerms.FindString(repoFile(t, rel)); m != "" {
			t.Errorf("%s mentions retired name %q", rel, m)
		}
	}

	// A per-copy render source is named .render; .tmpl belongs to the skeleton.
	for _, rel := range []string{"README.md", "docs/config.md", "skills/mws/SKILL.md"} {
		if strings.Contains(repoFile(t, rel), ".tmpl") {
			t.Errorf("%s mentions .tmpl; per-copy renders use the .render suffix", rel)
		}
	}
}

// TestHelpTextUsesNoRetiredNames walks every command's own help text.
func TestHelpTextUsesNoRetiredNames(t *testing.T) {
	root := NewRootCmd()
	for _, c := range append([]*cobra.Command{root}, visibleCommands(root)...) {
		text := c.Short + "\n" + c.Long + "\n" + c.Example
		c.LocalFlags().VisitAll(func(f *pflag.Flag) { text += "\n" + f.Usage })
		if m := retiredTerms.FindString(text); m != "" {
			t.Errorf("help of %q mentions retired name %q", c.CommandPath(), m)
		}
	}
}

// TestRootHelpDoesNotClaimOnlyListCommandsTakeJSON guards the sentence that
// went stale once clone, rm, restore and path gained --json.
func TestRootHelpDoesNotClaimOnlyListCommandsTakeJSON(t *testing.T) {
	if strings.Contains(NewRootCmd().Long, "list commands take --json") {
		t.Error("root help still says \"list commands take --json\"")
	}
}
