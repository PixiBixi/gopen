package main

import (
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
		return "", err
	}
	if prefix := os.Getenv("GIT_PREFIX"); prefix != "" {
		return filepath.Join(cwd, prefix), nil
	}
	return cwd, nil
}

// resolvePath returns the absolute path to the target file or directory.
func resolvePath(cfg config) (string, error) {
	var p string
	if len(cfg.paths) > 0 {
		p = cfg.paths[0]
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

	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", fmt.Errorf("path does not exist: %s", p)
	}
	return p, nil
}

// getRepoContext collects all git information needed to build the web URL.
func getRepoContext(targetPath, remoteName string) (repoContext, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return repoContext{}, err
	}

	dir := targetPath
	if !info.IsDir() {
		dir = filepath.Dir(targetPath)
	}

	if !isGitRepo(dir) {
		return repoContext{}, fmt.Errorf("not in a git repository")
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

	relPath, err := filepath.Rel(repoRoot, targetPath)
	if err != nil {
		return repoContext{}, err
	}
	if relPath == "." {
		relPath = ""
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
