# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

`gopen` — a zero-dependency Go CLI that opens a Git repository in the browser at the exact branch + path (+ optional line number). Single binary, cross-platform.

Module: `github.com/jeremy/gopen` | Go 1.26 | No external deps (stdlib only)

## Commands

```bash
# Build
make build           # binary: ./gopen
make build-all       # all platforms via GoReleaser
go build -v ./...

# Test & lint
go test ./...
go vet ./...
staticcheck ./...

# Install
make install         # → /usr/local/bin (requires sudo)
make install-user    # → ~/bin

# Release (CI handles this on v* tags)
git tag -a vX.Y.Z -m "..."
git push origin vX.Y.Z
```

Pre-commit hooks run automatically: `fmt`, `vet`, `mod tidy`, `build`, `staticcheck`.

## Architecture

Everything lives in `main.go` (~466 lines). The flow is strictly sequential:

1. **`reorderArgs()`** — normalizes flexible flag placement (`gopen file.go -l42` → `gopen -l 42 file.go`)
2. **Path resolution** — handles `GIT_PREFIX` (set when called via `git alias`)
3. **Git queries** — `getGitRemoteURL()`, `getCurrentBranch()`, `getRepoRoot()` via `exec.Command("git", ...)`
4. **`convertToHTTPS()`** — normalizes `git://` and `ssh://` URLs to HTTPS
5. **`buildWebURL()`** — platform-specific URL construction (GitHub, GitLab, Bitbucket, Azure DevOps, Gitea/Gogs, AWS CodeCommit; falls back to GitHub-style)
6. **Output** — `openBrowser()` or `copyToClipboard()`, both cross-platform

**Line number fragment formats differ by platform** — check `buildWebURL()` before adding new platform support. GitLab uses `#L42-50`, GitHub uses `#L42-L50`, Bitbucket uses `#lines-42:50`, Azure DevOps uses query params.

## CI/CD

- **CI** (`.github/workflows/ci.yml`): test + vet + staticcheck on push/PR, matrix: ubuntu/macos/windows
- **Release** (`.github/workflows/release.yml`): triggers on `v*` tags → GoReleaser builds multi-platform binaries + updates Homebrew tap
- **GoReleaser config**: `.goreleaser.yml`
