package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- effectiveCwd ---

func TestEffectiveCwd(t *testing.T) {
	t.Run("no GIT_PREFIX returns os.Getwd", func(t *testing.T) {
		t.Setenv("GIT_PREFIX", "")
		got, err := effectiveCwd()
		if err != nil {
			t.Fatalf("effectiveCwd() error = %v", err)
		}
		want, _ := os.Getwd()
		if got != want {
			t.Errorf("effectiveCwd() = %q, want %q", got, want)
		}
	})

	t.Run("GIT_PREFIX is joined with cwd", func(t *testing.T) {
		t.Setenv("GIT_PREFIX", "sub/dir")
		got, err := effectiveCwd()
		if err != nil {
			t.Fatalf("effectiveCwd() error = %v", err)
		}
		cwd, _ := os.Getwd()
		want := filepath.Join(cwd, "sub/dir")
		if got != want {
			t.Errorf("effectiveCwd() = %q, want %q", got, want)
		}
	})
}

// --- resolvePath ---

func TestResolvePath(t *testing.T) {
	t.Setenv("GIT_PREFIX", "")

	tmp := t.TempDir()
	existingFile := filepath.Join(tmp, "file.go")
	if err := os.WriteFile(existingFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()

	tests := []struct {
		name    string
		paths   []string
		want    string
		wantErr bool
	}{
		{
			name:  "no paths returns effective cwd",
			paths: nil,
			want:  cwd,
		},
		{
			name:  "absolute path to existing file",
			paths: []string{existingFile},
			want:  existingFile,
		},
		{
			name:  "absolute path to existing dir",
			paths: []string{tmp},
			want:  tmp,
		},
		{
			name:  "relative path is joined with effective cwd",
			paths: []string{"git_test.go"},
			want:  filepath.Join(cwd, "git_test.go"),
		},
		{
			name:    "non-existent path returns error wrapping os.ErrNotExist",
			paths:   []string{filepath.Join(tmp, "no-such-file")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePath(tt.paths)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("expected error to wrap os.ErrNotExist, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePath(%v) error = %v", tt.paths, err)
			}
			if got != tt.want {
				t.Errorf("resolvePath(%v) = %q, want %q", tt.paths, got, tt.want)
			}
		})
	}
}

// --- git integration helpers ---
// These tests run against a real temporary git repository.

// runGit runs a git command in dir and fails the test if it errors.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// gitOut runs a git command in dir and returns its trimmed stdout. It is what
// the pure-Go readers are cross-checked against: the only definition of a
// correct answer is the one the git binary gives.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// tryGit runs a git command in dir and returns its error instead of failing
// the test. Used to pin a test expectation on what the git binary really does
// before asserting that the pure-Go reader agrees.
func tryGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func newTmpGitRepo(t *testing.T) string {
	t.Helper()
	return newTmpGitRepoIn(t, t.TempDir())
}

// newTmpGitRepoIn is newTmpGitRepo at a caller-chosen location, for fixtures
// that need the repository to sit under a particular directory — an includeIf
// gitdir: pattern rooted at ~, for instance.
func newTmpGitRepoIn(t *testing.T, dir string) string {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		runGit(t, dir, args...)
	}
	return dir
}

// newTmpSubmodule builds a superproject with a submodule checked out at
// <super>/sub. It is the fixture for the two things that make submodules
// special on disk: a .git *file* holding a relative gitdir pointer, and a
// module config carrying core.worktree.
func newTmpSubmodule(t *testing.T) (super, sub string) {
	t.Helper()
	src := newTmpGitRepo(t)
	super = newTmpGitRepo(t)
	runGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "sub")
	runGit(t, super, "commit", "-q", "-m", "add submodule")
	return super, filepath.Join(super, "sub")
}

func TestIsGitRepo(t *testing.T) {
	t.Run("inside git repo returns true", func(t *testing.T) {
		dir := newTmpGitRepo(t)
		if !isGitRepo(dir) {
			t.Error("isGitRepo() = false, want true")
		}
	})

	t.Run("outside git repo returns false", func(t *testing.T) {
		dir := t.TempDir()
		if isGitRepo(dir) {
			t.Error("isGitRepo() = true, want false")
		}
	})
}

func TestGetCurrentBranch(t *testing.T) {
	dir := newTmpGitRepo(t)
	branch, err := getCurrentBranch(dir)
	if err != nil {
		t.Fatalf("getCurrentBranch() error = %v", err)
	}
	if branch == "" {
		t.Error("getCurrentBranch() returned empty string")
	}
}

func TestGetRepoRoot(t *testing.T) {
	dir := newTmpGitRepo(t)
	subdir := filepath.Join(dir, "pkg", "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := getRepoRoot(subdir)
	if err != nil {
		t.Fatalf("getRepoRoot() error = %v", err)
	}
	// On macOS, TempDir may return a symlinked path; resolve both.
	gotResolved, _ := filepath.EvalSymlinks(root)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	if gotResolved != wantResolved {
		t.Errorf("getRepoRoot() = %q, want %q", root, dir)
	}
}

func TestGetGitRemoteURL(t *testing.T) {
	dir := newTmpGitRepo(t)
	const remoteURL = "https://github.com/example/repo"
	cmd := exec.Command("git", "remote", "add", "origin", remoteURL)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	got, err := getGitRemoteURL("origin", dir)
	if err != nil {
		t.Fatalf("getGitRemoteURL() error = %v", err)
	}
	if got != remoteURL {
		t.Errorf("getGitRemoteURL() = %q, want %q", got, remoteURL)
	}
}

func TestGetGitRemoteURL_Error(t *testing.T) {
	dir := newTmpGitRepo(t)
	_, err := getGitRemoteURL("nonexistent-remote", dir)
	if err == nil {
		t.Error("expected error for nonexistent remote, got nil")
	}
}

// --- getRepoContext ---

// realPath resolves symlinks — needed on macOS where t.TempDir() returns
// a /var/folders path that git resolves to /private/var/folders.
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return r
}

func TestGetRepoContext(t *testing.T) {
	dir := newTmpGitRepo(t)
	const remoteURL = "https://github.com/example/repo"
	cmd := exec.Command("git", "remote", "add", "origin", remoteURL)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}
	// Use the real (symlink-resolved) dir so paths match what git reports.
	realDir := realPath(t, dir)

	t.Run("repo root path returns empty relPath", func(t *testing.T) {
		ctx, err := getRepoContext(realDir, "origin")
		if err != nil {
			t.Fatalf("getRepoContext() error = %v", err)
		}
		if ctx.relPath != "" {
			t.Errorf("relPath = %q, want empty string for repo root", ctx.relPath)
		}
		if ctx.baseURL != remoteURL {
			t.Errorf("baseURL = %q, want %q", ctx.baseURL, remoteURL)
		}
		if ctx.branch == "" {
			t.Error("branch is empty")
		}
	})

	t.Run("file path returns correct relPath", func(t *testing.T) {
		filePath := filepath.Join(realDir, "main.go")
		if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		ctx, err := getRepoContext(filePath, "origin")
		if err != nil {
			t.Fatalf("getRepoContext() error = %v", err)
		}
		if ctx.relPath != "main.go" {
			t.Errorf("relPath = %q, want %q", ctx.relPath, "main.go")
		}
	})

	t.Run("subdirectory path returns correct relPath", func(t *testing.T) {
		subdir := filepath.Join(realDir, "pkg", "util")
		if err := os.MkdirAll(subdir, 0755); err != nil {
			t.Fatal(err)
		}
		ctx, err := getRepoContext(subdir, "origin")
		if err != nil {
			t.Fatalf("getRepoContext() error = %v", err)
		}
		if ctx.relPath != filepath.Join("pkg", "util") {
			t.Errorf("relPath = %q, want %q", ctx.relPath, filepath.Join("pkg", "util"))
		}
	})

	t.Run("non-git directory returns error", func(t *testing.T) {
		nonGitDir := t.TempDir()
		_, err := getRepoContext(nonGitDir, "origin")
		if err == nil {
			t.Error("expected error for non-git dir, got nil")
		}
	})

	t.Run("non-existent path returns stat error", func(t *testing.T) {
		_, err := getRepoContext(filepath.Join(realDir, "no-such-path"), "origin")
		if err == nil {
			t.Error("expected error for non-existent path, got nil")
		}
	})

	t.Run("wrong remote name returns error", func(t *testing.T) {
		_, err := getRepoContext(realDir, "nonexistent-remote")
		if err == nil {
			t.Error("expected error for nonexistent remote, got nil")
		}
	})
}
