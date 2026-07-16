# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

`gopen` — a zero-dependency Go CLI that opens a Git repository in the browser at the exact branch + path (+ optional line number). Single binary, cross-platform.

Module: `github.com/PixiBixi/gopen` | Go 1.26 | No external deps (stdlib only)

## Commands

```bash
# Build
make build           # binary: ./gopen
make build-all       # cross-compile all platforms (plain go build, NOT GoReleaser)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o gopen .  # correct since code is split across multiple files
go build -v ./...

# Test & lint
go test ./...                       # CI runs with -race
go test -run TestParseArgs ./...    # single test
golangci-lint run                   # config: .golangci.yml (v2); replaces standalone vet/staticcheck

# Install
make install         # → /usr/local/bin (requires sudo)
make install-user    # → ~/bin

# Release: automatic on push to main (conventional commits drive the bump).
# feat: -> minor, fix: -> patch. Manual tag still works as an escape hatch:
git tag -a vX.Y.Z -m "..."
git push origin vX.Y.Z
```

Pre-commit hooks run automatically: `fmt`, `vet`, `mod tidy`, `build`, `staticcheck` (see `.pre-commit-config.yaml`). CI lint uses `golangci-lint` instead.

## Architecture

Code is split across 6 files:

| File | Responsibility |
|---|---|
| `main.go` | Orchestration only (~50 lines) |
| `args.go` | `config` struct, `parseArgs()`, `usage()` |
| `git.go` | `repoContext` struct, `resolvePath()`, `getRepoContext()`, git subprocess helpers, `convertToHTTPS()` |
| `url.go` | `provider` struct, `providers` slice, `buildWebURL()`, `detectProvider()`, `pathJoin()`, line anchor helpers |
| `output.go` | `openBrowser()`, `copyToClipboard()` |
| `completion.go` | `detectShell()`, `printCompletion()`, bash/zsh/fish completion scripts |

Each source file has a matching `*_test.go` (table-driven tests).

The flow in `main()` is strictly sequential:

1. `parseArgs()` — custom flag parser; flags and positional args in any order, `--flag value`, `--flag=value`, `-fvalue` (for `-l`/`-r`). No stdlib `flag` package.
2. `resolvePath()` — resolves target to absolute path; applies `GIT_PREFIX` (set by git when called via alias, changes cwd to repo root).
3. `getRepoContext()` — single call that runs all git queries and returns `repoContext{baseURL, branch, relPath}`.
4. `buildWebURL()` — detects provider, delegates to the matching `provider` struct.
5. `openBrowser()` or `copyToClipboard()`.

**Adding a new platform**: add a `provider{}` entry to the `providers` slice in `url.go` — no other file to touch. Line anchor format differs per platform; see existing anchor helpers (`anchorLN`, `anchorGL`, `anchorBB`, `anchorADO`).

## Gotchas

- **Adding a new flag**: update `parseArgs()` in `args.go` only. Boolean flags must match the full arg string (`arg == "-c"`), not just the first character.
- **`buildWebURL()` signature**: `(ctx repoContext, lineNumber, commitHash string)` — commit URLs use different path prefixes per platform (`/commit/`, `/-/commit/`, `/commits/`).
- **`flag` package removed**: stdlib `flag` is not used; don't re-add it.
- **Build command**: always `go build -o gopen .` (not `go build -o gopen main.go`) since code spans multiple files.

## CI/CD

- **CI** (`.github/workflows/ci.yml`): `go mod verify` + `go test -race` + build matrix (ubuntu/macos/windows) on push/PR
- **Lint** (`.github/workflows/lint.yml`): `golangci-lint` (config `.golangci.yml`, v2 schema)
- **Other checks**: `govulncheck.yml`, `go-format.yml`, `markdownlint.yml`, `github-actions.yml`
- **Release** (`.github/workflows/release.yml`): automatic on push to `main` — `mathieudutour/github-tag-action` computes the next `vX.Y.Z` from conventional commits (`default_bump: false`) and tags, then GoReleaser builds multi-platform binaries + updates Homebrew tap in the same job. Renovate drives dep releases (gomod minor → `feat(deps)`, patch/digest → `fix(deps)`, github-actions → `chore(deps)` = no release). Manual `v*` tag push still works.
- **GoReleaser config**: `.goreleaser.yml`
