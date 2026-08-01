package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repoContext holds all git information needed to build a web URL.
type repoContext struct {
	baseURL string // HTTPS URL of the remote
	branch  string
	relPath string // relative path from repo root; empty = repo root
}

// effectiveCwd returns the working directory, applying GIT_PREFIX when
// gopen is invoked via a git alias (git changes cwd to repo root).
func effectiveCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	if prefix := os.Getenv("GIT_PREFIX"); prefix != "" {
		return filepath.Join(cwd, prefix), nil
	}
	return cwd, nil
}

// resolvePath returns the absolute path to the target file or directory.
func resolvePath(paths []string) (string, error) {
	var p string
	if len(paths) > 0 {
		p = paths[0]
		if !filepath.IsAbs(p) {
			cwd, err := effectiveCwd()
			if err != nil {
				return "", err
			}
			p = filepath.Join(cwd, p)
		}
	} else {
		var err error
		p, err = effectiveCwd()
		if err != nil {
			return "", err
		}
	}

	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("path does not exist: %w", err)
	}
	return p, nil
}

// getRepoContext collects all git information needed to build the web URL.
//
// It prefers reading .git directly, which avoids four subprocess forks, and
// defers to the git binary whenever the fast path cannot be certain of the
// result. Correctness always wins over speed: the fast path must never return
// a value that differs from what git would have produced, so every state it
// does not fully understand is an error here, not a guess.
func getRepoContext(targetPath, remoteName string) (repoContext, error) {
	if ctx, err := readRepoContextFromDisk(targetPath, remoteName); err == nil {
		return ctx, nil
	}
	return repoContextViaGit(targetPath, remoteName)
}

// repoContextViaGit is the subprocess fallback: four git invocations.
func repoContextViaGit(targetPath, remoteName string) (repoContext, error) {
	// Same resolution the fast path applies, from the same helper so the two
	// cannot drift: git reports a symlink-resolved root, so the target has to
	// be expressed in the resolved namespace too or relPath fills with "..".
	// On macOS /var is a symlink to /private/var, which makes this routine.
	dir, resolvedTarget, err := resolveTarget(targetPath)
	if err != nil {
		return repoContext{}, err
	}

	if !isGitRepo(dir) {
		return repoContext{}, errors.New("not in a git repository")
	}

	remoteURL, err := getGitRemoteURL(remoteName, dir)
	if err != nil {
		return repoContext{}, err
	}

	branch, err := getCurrentBranch(dir)
	if err != nil {
		return repoContext{}, err
	}

	repoRoot, err := getRepoRoot(dir)
	if err != nil {
		return repoContext{}, err
	}

	relPath, err := relativeToRoot(repoRoot, resolvedTarget)
	if err != nil {
		return repoContext{}, err
	}

	return repoContext{
		baseURL: convertToHTTPS(remoteURL),
		branch:  branch,
		relPath: relPath,
	}, nil
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func getGitRemoteURL(remoteName, dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL for '%s': %w", remoteName, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func getCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func getRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
