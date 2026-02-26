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

// reorderArgs reorders arguments to put all flags before positional arguments
// This allows "gopen file.go -l 42" to work the same as "gopen -l 42 file.go"
// Also handles flags with values attached like -l42 -> -l 42
func reorderArgs(args []string) []string {
	var flags []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			// Long flag like --line=42 or --line 42
			if strings.Contains(arg, "=") {
				flags = append(flags, arg)
			} else {
				flags = append(flags, arg)
				// Check if next arg is the value
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					if arg == "--line" || arg == "--remote" || arg == "--commit" {
						i++
						flags = append(flags, args[i])
					}
				}
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			// Short flags: match exactly to avoid misclassifying -commit as -c
			if arg == "-c" || arg == "-v" {
				// Boolean short flags
				flags = append(flags, arg)
			} else if arg[1:2] == "l" || arg[1:2] == "r" {
				// Value short flags, optionally with attached value like -l42
				flagChar := arg[1:2]
				if len(arg) > 2 {
					flags = append(flags, "-"+flagChar, arg[2:])
				} else {
					flags = append(flags, arg)
					if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
						i++
						flags = append(flags, args[i])
					}
				}
			} else {
				// Long-name single-dash flags (e.g. -commit, -remote): pass through
				// and consume the next non-flag argument as their value
				flags = append(flags, arg)
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positional = append(positional, arg)
		}
	}

	return append(flags, positional...)
}

func main() {
	// Parse flags - reorder args to support flags after positional arguments
	args := reorderArgs(os.Args[1:])
	os.Args = append([]string{os.Args[0]}, args...)

	versionFlag := flag.Bool("version", false, "Print version information")
	flag.BoolVar(versionFlag, "v", false, "Print version information (shorthand)")
	remoteName := flag.String("remote", "origin", "Git remote name to use")
	flag.StringVar(remoteName, "r", "origin", "Git remote name to use (shorthand)")
	copyFlag := flag.Bool("copy", false, "Copy URL to clipboard instead of opening browser")
	flag.BoolVar(copyFlag, "c", false, "Copy URL to clipboard instead of opening browser (shorthand)")
	lineNumber := flag.String("line", "", "Line number or range (e.g., 42 or 42-50)")
	flag.StringVar(lineNumber, "l", "", "Line number or range (shorthand)")
	commitHash := flag.String("commit", "", "Open a specific commit (hash or short hash)")
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
			// When called via git alias, git changes cwd to repo root
			// but sets GIT_PREFIX to the original relative path
			if gitPrefix := os.Getenv("GIT_PREFIX"); gitPrefix != "" {
				cwd = filepath.Join(cwd, gitPrefix)
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
	remoteURL, err := getGitRemoteURL(*remoteName, checkDir)
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
	webURL := buildWebURL(httpsURL, branch, relPath, *lineNumber, *commitHash)

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
