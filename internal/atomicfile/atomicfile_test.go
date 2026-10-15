package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFilePreservesPermsOnFreshDst(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "dst")
	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("dst perms = %#o, want 0600", got)
	}
}

func TestCopyFileResetsPermsOnOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("dst perms after overwrite = %#o, want 0600", got)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secret" {
		t.Fatalf("dst body = %q, want %q", string(body), "secret")
	}
}

func TestCopyFileLeavesNoTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "src" && e.Name() != "dst" {
			t.Fatalf("unexpected leftover %q in dst dir", e.Name())
		}
	}
}

func TestWriteReplacesContentAndPerms(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out", "dst")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old content, longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(dst, []byte("new"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("perm = %o, want 644", st.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dst dir holds %d entries, want only dst", len(entries))
	}
}
