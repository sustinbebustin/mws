package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLinkMetaIntoWorkingCopy(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "proj-meta")
	wc := filepath.Join(root, "proj")

	if err := os.MkdirAll(filepath.Join(meta, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".claude", ".workspace", "CLAUDE.md", "justfile"} {
		p := filepath.Join(meta, name)
		if filepath.Ext(name) == "" && name != "CLAUDE.md" && name != "justfile" {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	linked, err := LinkMetaIntoWorkingCopy(meta, wc)
	if err != nil {
		t.Fatalf("LinkMetaIntoWorkingCopy: %v", err)
	}

	if slices.Contains(linked, ".git") {
		t.Fatal("must not link .git")
	}
	if len(linked) != 4 {
		t.Fatalf("got %d links, want 4: %v", len(linked), linked)
	}

	// Verify each symlink resolves correctly.
	for _, name := range linked {
		linkPath := filepath.Join(wc, name)
		st, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("Lstat %s: %v", linkPath, err)
		}
		if st.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", linkPath)
		}
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			t.Fatalf("EvalSymlinks %s: %v", linkPath, err)
		}
		want, _ := filepath.EvalSymlinks(filepath.Join(meta, name))
		if resolved != want {
			t.Fatalf("symlink %s resolves to %s, want %s", linkPath, resolved, want)
		}
	}
}

func TestLinkPreservesExistingNonSymlinks(t *testing.T) {
	root := t.TempDir()
	meta := filepath.Join(root, "proj-meta")
	wc := filepath.Join(root, "proj")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wc, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a real file at wc/X.
	if err := os.WriteFile(filepath.Join(wc, "X"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And add X to the meta too -- conflict.
	if err := os.WriteFile(filepath.Join(meta, "X"), []byte("meta"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LinkMetaIntoWorkingCopy(meta, wc)
	if err != nil {
		t.Fatalf("LinkMetaIntoWorkingCopy: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(wc, "X"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "local" {
		t.Fatalf("local file overwritten: got %q want %q", string(got), "local")
	}
}
