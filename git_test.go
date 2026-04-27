package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func newTmpGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	return dir
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
