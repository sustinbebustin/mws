package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestInitAndCurrentBranch(t *testing.T) {
	gitAvailable(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := InitQuiet(ctx, dir); err != nil {
		t.Fatalf("InitQuiet: %v", err)
	}

	// Configure user so an initial commit can be made.
	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test User"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run(); err != nil {
			t.Fatalf("git config: %v", err)
		}
	}

	// Create an initial commit so HEAD has a defined branch.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", dir, "add", "README"},
		{"-C", dir, "commit", "-m", "init"},
	} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	br, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if br == "" {
		t.Fatal("CurrentBranch returned empty string")
	}
}

func TestCloneLocal(t *testing.T) {
	gitAvailable(t)
	ctx := context.Background()
	src := t.TempDir()
	if err := InitQuiet(ctx, src); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", src, "config", "user.email", "t@e.com"},
		{"-C", src, "config", "user.name", "t"},
	} {
		_ = exec.Command("git", args...).Run()
	}
	if err := os.WriteFile(filepath.Join(src, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", src, "add", "x").Run()
	_ = exec.Command("git", "-C", src, "commit", "-m", "x").Run()

	dst := filepath.Join(t.TempDir(), "copy")
	if err := CloneLocal(ctx, src, dst); err != nil {
		t.Fatalf("CloneLocal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "x")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
}

func TestSetRemoteURL(t *testing.T) {
	gitAvailable(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := InitQuiet(ctx, dir); err != nil {
		t.Fatal(err)
	}
	const initialURL = "https://example.invalid/initial.git"
	const newURL = "git@example.invalid:owner/repo.git"
	if err := exec.Command("git", "-C", dir, "remote", "add", "origin", initialURL).Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	if err := SetRemoteURL(ctx, dir, "origin", newURL); err != nil {
		t.Fatalf("SetRemoteURL: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("git remote get-url: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != newURL {
		t.Fatalf("origin url: got %q, want %q", got, newURL)
	}
}
