package main

import (
	"flag"
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

func main() {
	// Parse flags
	versionFlag := flag.Bool("version", false, "Print version information")
	flag.BoolVar(versionFlag, "v", false, "Print version information (shorthand)")
	remoteName := flag.String("remote", "origin", "Git remote name to use")
	flag.StringVar(remoteName, "r", "origin", "Git remote name to use (shorthand)")
	copyFlag := flag.Bool("copy", false, "Copy URL to clipboard instead of opening browser")
	flag.BoolVar(copyFlag, "c", false, "Copy URL to clipboard instead of opening browser (shorthand)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("git-opener %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
		os.Exit(0)
	}

	// Get target path (file or directory)
	var targetPath string
	if flag.NArg() > 0 {
		// File/directory provided as argument
		targetPath = flag.Arg(0)
		// Convert to absolute path if relative
		if !filepath.IsAbs(targetPath) {
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
				os.Exit(1)
			}
			targetPath = filepath.Join(cwd, targetPath)
		}
	} else {
		// Use current directory
		var err error
		targetPath, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
			os.Exit(1)
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
	remoteURL, err := getGitRemoteURL(*remoteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting git remote URL: %v\n", err)
		os.Exit(1)
	}

	// Convert to HTTPS URL
	httpsURL := convertToHTTPS(remoteURL)

	// Get current branch
	branch, err := getCurrentBranch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current branch: %v\n", err)
		os.Exit(1)
	}

	// Get repository root
	repoRoot, err := getRepoRoot()
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
	webURL := buildWebURL(httpsURL, branch, relPath)

	// Copy to clipboard or open in browser
	if *copyFlag {
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
func getGitRemoteURL(remoteName string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL for '%s': %w", remoteName, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getCurrentBranch gets the current git branch name
func getCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getRepoRoot gets the root directory of the git repository
func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
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
func buildWebURL(baseURL, branch, relPath string) string {
	// Normalize relative path (empty or "." means root)
	if relPath == "." || relPath == "" {
		relPath = ""
	}

	// Detect provider and build appropriate URL
	switch {
	case strings.Contains(baseURL, "github.com"):
		// GitHub: https://github.com/user/repo/tree/branch/path
		if relPath == "" {
			return fmt.Sprintf("%s/tree/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/tree/%s/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "gitlab.com") || strings.Contains(baseURL, "gitlab"):
		// GitLab: https://gitlab.com/user/repo/-/tree/branch/path
		if relPath == "" {
			return fmt.Sprintf("%s/-/tree/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/-/tree/%s/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "bitbucket.org"):
		// Bitbucket Cloud: https://bitbucket.org/user/repo/src/branch/path
		if relPath == "" {
			return fmt.Sprintf("%s/src/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/src/%s/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "dev.azure.com") || strings.Contains(baseURL, "visualstudio.com"):
		// Azure DevOps: https://dev.azure.com/org/project/_git/repo?version=GBbranch&path=/path
		if relPath == "" {
			return fmt.Sprintf("%s?version=GB%s", baseURL, branch)
		}
		return fmt.Sprintf("%s?version=GB%s&path=/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "gitea"):
		// Gitea: https://gitea.domain.com/user/repo/src/branch/path
		if relPath == "" {
			return fmt.Sprintf("%s/src/branch/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/src/branch/%s/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "gogs"):
		// Gogs: https://gogs.domain.com/user/repo/src/branch/path
		if relPath == "" {
			return fmt.Sprintf("%s/src/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/src/%s/%s", baseURL, branch, relPath)

	case strings.Contains(baseURL, "console.aws.amazon.com") || strings.Contains(baseURL, "codecommit"):
		// AWS CodeCommit: Already HTTPS console URL
		// Format: https://console.aws.amazon.com/codesuite/codecommit/repositories/repo/browse/refs/heads/branch/--/path
		if relPath == "" {
			return fmt.Sprintf("%s/browse/refs/heads/%s/--/", baseURL, branch)
		}
		return fmt.Sprintf("%s/browse/refs/heads/%s/--/%s", baseURL, branch, relPath)

	default:
		// Fallback to GitHub-style format
		if relPath == "" {
			return fmt.Sprintf("%s/tree/%s", baseURL, branch)
		}
		return fmt.Sprintf("%s/tree/%s/%s", baseURL, branch, relPath)
	}
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
