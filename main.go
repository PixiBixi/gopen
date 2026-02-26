package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type config struct {
	version    bool
	remoteName string
	copy       bool
	line       string
	commit     string
	paths      []string
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: gopen [flags] [path]

Open a Git repository path in the browser at the current branch.

Flags:
  -v, --version        Print version information
  -c, --copy           Copy URL to clipboard instead of opening browser
  -r, --remote <name>  Git remote to use (default: origin)
  -l, --line <n[-m]>   Highlight line or range (e.g. 42 or 42-50)
      --commit <hash>  Open a specific commit or file at that commit

Examples:
  gopen                        # current directory
  gopen main.go                # file on current branch
  gopen main.go -l 42          # file at line 42
  gopen --commit abc1234       # commit page
  gopen --commit abc1234 -c    # copy commit URL
`)
}

// parseArgs parses flags and positional arguments in any order.
// Supports: --flag value, --flag=value, -f value, -fvalue (for -l/-r).
func parseArgs(args []string) (config, error) {
	cfg := config{remoteName: "origin"}

	stringFlag := func(i *int, arg, prefix string) (string, error) {
		if v, ok := strings.CutPrefix(arg, prefix+"="); ok {
			return v, nil
		}
		*i++
		if *i >= len(args) {
			return "", fmt.Errorf("flag %s requires a value", prefix)
		}
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-v" || arg == "--version":
			cfg.version = true
		case arg == "-c" || arg == "--copy":
			cfg.copy = true
		case arg == "-r" || arg == "--remote" || strings.HasPrefix(arg, "-r") || strings.HasPrefix(arg, "--remote="):
			if arg == "-r" || arg == "--remote" {
				v, err := stringFlag(&i, arg, arg)
				if err != nil {
					return cfg, err
				}
				cfg.remoteName = v
			} else if v, ok := strings.CutPrefix(arg, "--remote="); ok {
				cfg.remoteName = v
			} else {
				cfg.remoteName = arg[2:] // -rorigin
			}
		case arg == "-l" || arg == "--line" || strings.HasPrefix(arg, "-l") || strings.HasPrefix(arg, "--line="):
			if arg == "-l" || arg == "--line" {
				v, err := stringFlag(&i, arg, arg)
				if err != nil {
					return cfg, err
				}
				cfg.line = v
			} else if v, ok := strings.CutPrefix(arg, "--line="); ok {
				cfg.line = v
			} else {
				cfg.line = arg[2:] // -l42
			}
		case arg == "--commit" || strings.HasPrefix(arg, "--commit="):
			v, err := stringFlag(&i, arg, "--commit")
			if err != nil {
				return cfg, err
			}
			cfg.commit = v
		case arg == "--":
			cfg.paths = append(cfg.paths, args[i+1:]...)
			return cfg, nil
		case strings.HasPrefix(arg, "-"):
			return cfg, fmt.Errorf("unknown flag: %s", arg)
		default:
			cfg.paths = append(cfg.paths, arg)
		}
	}
	return cfg, nil
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		usage()
		os.Exit(1)
	}

	if cfg.version {
		fmt.Printf("git-opener %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
		os.Exit(0)
	}

	// Get target path (file or directory)
	var targetPath string
	if len(cfg.paths) > 0 {
		targetPath = cfg.paths[0]
		if !filepath.IsAbs(targetPath) {
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
				os.Exit(1)
			}
			// When called via git alias, git changes cwd to repo root
			// but sets GIT_PREFIX to the original relative path
			if gitPrefix := os.Getenv("GIT_PREFIX"); gitPrefix != "" {
				cwd = filepath.Join(cwd, gitPrefix)
			}
			targetPath = filepath.Join(cwd, targetPath)
		}
	} else {
		targetPath, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
			os.Exit(1)
		}
		// When called via git alias, git changes cwd to repo root
		// but sets GIT_PREFIX to the original relative path
		if gitPrefix := os.Getenv("GIT_PREFIX"); gitPrefix != "" {
			targetPath = filepath.Join(targetPath, gitPrefix)
		}
	}

	// Check if path exists
	fileInfo, err := os.Stat(targetPath)
	if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Path does not exist: %s\n", targetPath)
		os.Exit(1)
	}

	// Determine the directory to check for git repo
	// If targetPath is a file, use its parent directory
	checkDir := targetPath
	if !fileInfo.IsDir() {
		checkDir = filepath.Dir(targetPath)
	}

	// Check if we're in a git repository
	if !isGitRepo(checkDir) {
		fmt.Fprintf(os.Stderr, "Not in a git repository\n")
		os.Exit(1)
	}

	// Get git remote URL
	remoteURL, err := getGitRemoteURL(cfg.remoteName, checkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting git remote URL: %v\n", err)
		os.Exit(1)
	}

	// Convert to HTTPS URL
	httpsURL := convertToHTTPS(remoteURL)

	// Get current branch
	branch, err := getCurrentBranch(checkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current branch: %v\n", err)
		os.Exit(1)
	}

	// Get repository root
	repoRoot, err := getRepoRoot(checkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting repository root: %v\n", err)
		os.Exit(1)
	}

	// Calculate relative path from repo root
	relPath, err := filepath.Rel(repoRoot, targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error calculating relative path: %v\n", err)
		os.Exit(1)
	}

	// Build web URL
	webURL := buildWebURL(httpsURL, branch, relPath, cfg.line, cfg.commit)

	// Copy to clipboard or open in browser
	if cfg.copy {
		if err := copyToClipboard(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error copying to clipboard: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("URL copied to clipboard: %s\n", webURL)
	} else {
		fmt.Printf("Opening: %s\n", webURL)
		if err := openBrowser(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
			os.Exit(1)
		}
	}
}

// isGitRepo checks if the current directory is inside a git repository
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// getGitRemoteURL gets the URL of the specified remote
func getGitRemoteURL(remoteName, dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL for '%s': %w", remoteName, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getCurrentBranch gets the current git branch name
func getCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getRepoRoot gets the root directory of the git repository
func getRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// convertToHTTPS converts git:// or ssh:// URLs to HTTPS
func convertToHTTPS(url string) string {
	// Remove .git suffix if present
	url = strings.TrimSuffix(url, ".git")

	// Handle SSH format: git@github.com:user/repo
	sshRegex := regexp.MustCompile(`^git@([^:]+):(.+)$`)
	if matches := sshRegex.FindStringSubmatch(url); matches != nil {
		return fmt.Sprintf("https://%s/%s", matches[1], matches[2])
	}

	// Handle ssh:// format: ssh://git@github.com/user/repo
	if strings.HasPrefix(url, "ssh://") {
		url = strings.TrimPrefix(url, "ssh://")
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return "https://" + url
	}

	// Handle git:// format: git://github.com/user/repo
	if strings.HasPrefix(url, "git://") {
		return strings.Replace(url, "git://", "https://", 1)
	}

	// Already HTTPS or HTTP
	return url
}

// buildWebURL constructs the web URL for the repository
func buildWebURL(baseURL, branch, relPath, lineNumber, commitHash string) string {
	// Normalize relative path (empty or "." means root)
	if relPath == "." || relPath == "" {
		relPath = ""
	}

	var url string
	var lineFragment string

	// Parse line number to determine if it's a range
	var startLine, endLine string
	if lineNumber != "" {
		parts := strings.Split(lineNumber, "-")
		startLine = parts[0]
		if len(parts) > 1 {
			endLine = parts[1]
		}
	}

	// Detect provider and build appropriate URL
	switch {
	case strings.Contains(baseURL, "github.com"):
		if commitHash != "" {
			// GitHub commit: .../commit/HASH or .../blob/HASH/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/tree/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/tree/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#L%s-L%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#L%s", startLine)
			}
		}

	case strings.Contains(baseURL, "gitlab.com") || strings.Contains(baseURL, "gitlab"):
		if commitHash != "" {
			// GitLab commit: .../-/commit/HASH or .../-/blob/HASH/file
			if relPath == "" {
				url = fmt.Sprintf("%s/-/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/-/blob/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/-/tree/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/-/tree/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#L%s-%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#L%s", startLine)
			}
		}

	case strings.Contains(baseURL, "bitbucket.org"):
		if commitHash != "" {
			// Bitbucket commit: .../commits/HASH or .../src/HASH/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commits/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/src/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/src/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/src/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#lines-%s:%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#lines-%s", startLine)
			}
		}

	case strings.Contains(baseURL, "dev.azure.com") || strings.Contains(baseURL, "visualstudio.com"):
		if commitHash != "" {
			// Azure DevOps commit: .../commit/HASH or ...?version=GC<HASH>&path=/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s?version=GC%s&path=/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s?version=GB%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s?version=GB%s&path=/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("&line=%s&lineEnd=%s&lineStartColumn=1&lineEndColumn=1", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("&line=%s&lineEnd=%s&lineStartColumn=1&lineEndColumn=1", startLine, startLine)
			}
		}

	case strings.Contains(baseURL, "gitea"):
		if commitHash != "" {
			// Gitea commit: .../commit/HASH or .../src/commit/HASH/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/src/commit/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/src/branch/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/src/branch/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#L%s-L%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#L%s", startLine)
			}
		}

	case strings.Contains(baseURL, "gogs"):
		if commitHash != "" {
			// Gogs commit: .../commit/HASH or .../src/HASH/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/src/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/src/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/src/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#L%s-L%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#L%s", startLine)
			}
		}

	case strings.Contains(baseURL, "console.aws.amazon.com") || strings.Contains(baseURL, "codecommit"):
		if commitHash != "" {
			// AWS CodeCommit commit: .../commit/HASH or .../browse/HASH/--/file
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/browse/%s/--/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/browse/refs/heads/%s/--/", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/browse/refs/heads/%s/--/%s", baseURL, branch, relPath)
		}
		// AWS CodeCommit doesn't support line anchors in the same way

	default:
		// Fallback to GitHub-style format
		if commitHash != "" {
			if relPath == "" {
				url = fmt.Sprintf("%s/commit/%s", baseURL, commitHash)
			} else {
				url = fmt.Sprintf("%s/blob/%s/%s", baseURL, commitHash, relPath)
			}
		} else if relPath == "" {
			url = fmt.Sprintf("%s/tree/%s", baseURL, branch)
		} else {
			url = fmt.Sprintf("%s/tree/%s/%s", baseURL, branch, relPath)
		}
		if startLine != "" {
			if endLine != "" {
				lineFragment = fmt.Sprintf("#L%s-L%s", startLine, endLine)
			} else {
				lineFragment = fmt.Sprintf("#L%s", startLine)
			}
		}
	}

	return url + lineFragment
}

// openBrowser opens the URL in the default browser (cross-platform)
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Run()
}

// copyToClipboard copies text to the system clipboard (cross-platform)
func copyToClipboard(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try wl-copy first (Wayland), then xclip, then xsel
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard utility found (install wl-copy, xclip, or xsel)")
		}
	case "windows":
		cmd = exec.Command("cmd", "/c", "echo", text, "|", "clip")
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start clipboard command: %w", err)
	}

	if _, err := stdin.Write([]byte(text)); err != nil {
		return fmt.Errorf("failed to write to clipboard: %w", err)
	}

	if err := stdin.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}

	return cmd.Wait()
}
