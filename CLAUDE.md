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

Code is split across 7 files:

| File | Responsibility |
|---|---|
| `main.go` | Orchestration only (~50 lines) |
| `args.go` | `config` struct, `parseArgs()`, `usage()` |
| `git.go` | `repoContext` struct, `resolvePath()`, `getRepoContext()` (dispatches to the fast path with a fallback), `repoContextViaGit()` (subprocess fallback, four `git` invocations), `convertToHTTPS()` |
| `gitfile.go` | Pure-Go `.git` reader, the fast path: `configEntry`, `parseGitConfig()`, `firstConfigValue()`, `lastConfigValue()`, `readConfigFile()`, `branchFromHEAD()`, `isHexSHA()`, `isValidBranchName()`, `discoverGitDir()`, `needsGitFallback()`, `readRepoContextFromDisk()`, plus the `configScanner` machinery that resolves `include`/`includeIf` directives |
| `url.go` | `provider` struct, `providers` slice, `buildWebURL()`, `detectProvider()`, `pathJoin()`, line anchor helpers |
| `output.go` | `openBrowser()`, `copyToClipboard()` |
| `completion.go` | `detectShell()`, `printCompletion()`, bash/zsh/fish completion scripts |

Each source file has a matching `*_test.go` (table-driven tests).

The flow in `main()` is strictly sequential:

1. `parseArgs()` — custom flag parser; flags and positional args in any order, `--flag value`, `--flag=value`, `-fvalue` (for `-l`/`-r`). No stdlib `flag` package.
2. `resolvePath()` — resolves target to absolute path; applies `GIT_PREFIX` (set by git when called via alias, changes cwd to repo root).
3. `getRepoContext()` — tries `readRepoContextFromDisk()` (`gitfile.go`) first, a direct read of `.git` with no subprocess. Falls back to `repoContextViaGit()` (`git.go`, four `git` invocations) whenever the fast path can't be certain of the result — config includes it can't resolve, worktree config, unsupported ref storage, `GIT_CONFIG_*` env overrides, and the like. Either path returns the same `repoContext{baseURL, branch, relPath}`.
4. `buildWebURL()` — detects provider, delegates to the matching `provider` struct.
5. Output mode, precedence `print` > `copy` > `open`: `-p`/`--print` writes the bare URL to stdout and exits (for scripting), `-c`/`--copy` calls `copyToClipboard()`, otherwise `openBrowser()`.

**Adding a new platform**: add a `provider{}` entry to the `providers` slice in `url.go` — no other file to touch. Line anchor format differs per platform; see existing anchor helpers (`anchorLN`, `anchorGL`, `anchorBB`, `anchorADO`).

## Gotchas

- **Adding a new flag**: update `parseArgs()` in `args.go` only. Boolean flags must match the full arg string (`arg == "-c"`), not just the first character.
- **`buildWebURL()` signature**: `(ctx repoContext, lineNumber, commitHash string)` — commit URLs use different path prefixes per platform (`/commit/`, `/-/commit/`, `/commits/`).
- **`flag` package removed**: stdlib `flag` is not used; don't re-add it.
- **Build command**: always `go build -o gopen .` (not `go build -o gopen main.go`) since code spans multiple files.
- **Two paths in `getRepoContext()`.** The fast path (`readRepoContextFromDisk()`, `gitfile.go`) reads `.git` directly; `repoContextViaGit()` (`git.go`) is the subprocess fallback. Any change to either must keep `TestDifferential_FastPathMatchesGit` (`gitfile_test.go`) green — it runs every fixture through both paths and asserts they agree. That test is the safety net for the whole design.
- **The governing invariant**: the fast path must never produce a value different from what `git` would produce, including silently. Erroring or falling back where `git` succeeds is an acceptable lost optimization; returning a plausible-looking wrong repo, branch, or URL is not. This is why the fast path errs on the side of bailing out to `git` rather than guessing.
- **First url wins.** `git remote get-url` returns a remote's *first* `url` line in the config, while `git config --get` returns the *last*. `firstConfigValue()` matches `get-url`; `lastConfigValue()` is for keys (like `core.repositoryformatversion`) where git itself takes the last. Do not "fix" `firstConfigValue()` into last-wins.
- **Reftable repositories.** With `extensions.refstorage=reftable`, `.git/HEAD` literally contains `ref: refs/heads/.invalid` while `git` reports the real branch. `checkRepoConfig()`'s extensions gate (`safeRepoExtensions`) must keep rejecting anything but `refstorage=files`, or the fast path will read the decoy ref as the branch name.

## CI/CD

- **CI** (`.github/workflows/ci.yml`): `go mod verify` + `go test -race` + build matrix (ubuntu/macos/windows) on push/PR
- **Lint** (`.github/workflows/lint.yml`): `golangci-lint` (config `.golangci.yml`, v2 schema)
- **Other checks**: `govulncheck.yml`, `go-format.yml`, `markdownlint.yml`, `github-actions.yml`
- **Release** (`.github/workflows/release.yml`): automatic on push to `main` — [`svu`](https://github.com/caarlos0/svu) computes the next `vX.Y.Z` from the conventional commits since the last tag, the workflow creates the tag through the API, then GoReleaser builds multi-platform binaries + updates the Homebrew tap in the same job. Only `feat:` (minor) and `fix:` (patch) cut a release; **`perf:` does not** — svu follows the Conventional Commits spec, where only those two are normative. `--v0` keeps a breaking change from jumping to `v1.0.0` while pre-1.0. Renovate drives dep releases (gomod minor → `feat(deps)`, patch/digest → `fix(deps)`, github-actions → `chore(deps)` = no release). Manual `v*` tag push still works.
- **GoReleaser config**: `.goreleaser.yml`
