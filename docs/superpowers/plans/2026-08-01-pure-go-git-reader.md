# Pure-Go `.git` Reader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the four `git` subprocess calls on gopen's hot path with a
pure-Go reader of `.git`, falling back to `git` whenever the fast path cannot be
certain of the result.

**Architecture:** A new `gitfile.go` reads `.git/HEAD` and `.git/config` directly.
`git.go` keeps its existing subprocess helpers and becomes the fallback layer.
`getRepoContext()` stays the single entry point and picks a path: it delegates to
`git` when `GIT_DIR`/`GIT_WORK_TREE` is set or when any in-scope config file
mentions `insteadOf`/`include`/`includeIf`, otherwise it reads from disk and falls
back on any parse failure.

**Tech Stack:** Go 1.26, standard library only. No new module dependencies.

## Global Constraints

- Module `github.com/PixiBixi/gopen`, Go 1.26, **stdlib only** — `go.mod` must
  gain no `require` entries.
- Build command is always `go build -o gopen .` (code spans multiple files),
  never `go build -o gopen main.go`.
- Release build for size measurement:
  `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o <out> .`
- The stdlib `flag` package is **not** used and must not be re-added.
- Boolean flags match the full argument string (`arg == "-c"`), never a prefix.
- Every source file has a matching `*_test.go` with table-driven tests.
- Conventional Commits, **one scope per commit**. Never bundle scopes.
- Tests must never depend on the machine's or CI runner's git configuration:
  always pin `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`
  with `t.Setenv` in tests that touch config scope.
- `golangci-lint run` must pass (config `.golangci.yml`, v2 schema).
- Branch: `feat/pure-go-git-reader`, already created from `origin/main`.

## Invariant

**Never produce a URL different from what `git` would produce, including
silently.** Either the fast path is certain, or `git` decides. Every ambiguous
case falls back rather than guessing.

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `args.go` | Modify | Add `print` field, `-p`/`--print` case, usage text |
| `main.go` | Modify | Output-mode precedence: `print` > `copy` > `open` |
| `completion.go` | Modify | Add `-p`/`--print` to all three completion scripts |
| `gitfile.go` | **Create** | Pure-Go reader: config parser, HEAD, git dir discovery, fallback detection |
| `gitfile_test.go` | **Create** | Unit tests on the parser + differential tests vs subprocess |
| `git.go` | Modify | `getRepoContext()` becomes an orchestrator; subprocess helpers stay as fallback; `EvalSymlinks` consistency fix |
| `git_test.go` | Modify | Benchmark, fallback-path tests |
| `README.md` | Modify | Document `--print` |
| `CLAUDE.md` | Modify | Document the two-path architecture |

## Measurement Protocol

Baseline is captured in Task 1 (after `--print` exists, before the git path
changes) and re-run in Task 9. Record all four in the final report:

1. `go test -bench=BenchmarkGetRepoContext -benchmem -count=10 ./...`
2. `hyperfine --warmup 20 --min-runs 200 '<binary> --print README.md'`
3. `wc -c <binary>` and `go tool nm -size -sort size <binary>`
4. Output diff of `gopen --print` between old and new binary, on this repo plus
   a linked worktree

Baseline already known for the **current** binary (no `--print`):
2 148 274 bytes, ~102 ms end-to-end with `-c`, ~16.5 ms per `git` fork.

---

### Task 1: Add the `--print` output mode

This must land **before** any git-path change: the existing output modes fork
`open` or `pbcopy`, which masks the gain. `--print` is what makes an honest
end-to-end measurement possible, and it is useful on its own for scripting.

**Files:**
- Modify: `args.go:9-17` (struct), `args.go:19-41` (usage), `args.go:60-93` (switch)
- Modify: `main.go:48-62` (output dispatch)
- Modify: `completion.go:46`, `completion.go:59-68`, `completion.go:77-83`
- Modify: `README.md`
- Test: `args_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.print bool`. Task 9 relies on the binary accepting
  `--print <path>` and writing the bare URL plus `\n` to stdout.

- [ ] **Step 1: Write the failing tests**

Append to `args_test.go`:

```go
func TestParseArgs_Print(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr bool
	}{
		{
			name: "long form",
			args: []string{"--print"},
			want: config{remoteName: "origin", print: true},
		},
		{
			name: "short form",
			args: []string{"-p"},
			want: config{remoteName: "origin", print: true},
		},
		{
			name: "with a path",
			args: []string{"-p", "main.go"},
			want: config{remoteName: "origin", print: true, paths: []string{"main.go"}},
		},
		{
			name: "combined with copy: both flags set, precedence resolved in main",
			args: []string{"-p", "-c"},
			want: config{remoteName: "origin", print: true, copy: true},
		},
		{
			name:    "-p is not confused with a -p-prefixed unknown flag",
			args:    []string{"-pretty"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an unknown-flag error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) error = %v", tt.args, err)
			}
			if got.print != tt.want.print {
				t.Errorf("print = %v, want %v", got.print, tt.want.print)
			}
			if got.copy != tt.want.copy {
				t.Errorf("copy = %v, want %v", got.copy, tt.want.copy)
			}
			if len(got.paths) != len(tt.want.paths) {
				t.Fatalf("paths = %v, want %v", got.paths, tt.want.paths)
			}
			for i := range got.paths {
				if got.paths[i] != tt.want.paths[i] {
					t.Errorf("paths[%d] = %q, want %q", i, got.paths[i], tt.want.paths[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -run TestParseArgs_Print ./...`
Expected: compile error, `got.print undefined (type config has no field or method print)`.

- [ ] **Step 3: Add the struct field**

In `args.go`, add to `config` (after `copy bool`):

```go
	print      bool
```

- [ ] **Step 4: Add the flag case**

In `args.go`, in the `switch arg` block, right after the `case "-c", "--copy":` arm:

```go
		case "-p", "--print":
			cfg.print = true
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test -run TestParseArgs_Print ./...`
Expected: PASS. The `-pretty` case already errors via the existing
`strings.HasPrefix(arg, "-")` default arm, because the `switch` matches the full
string.

- [ ] **Step 6: Update the usage text**

In `args.go`, in `usage()`, insert after the `-c, --copy` line:

```
  -p, --print          Print the URL to stdout and exit (no browser, no clipboard)
```

And add to the Examples block, after `gopen main.go -l 42`:

```
  gopen -p main.go             # print URL, useful in scripts
```

- [ ] **Step 7: Wire the output dispatch**

In `main.go`, replace the `if cfg.copy { ... } else { ... }` block (lines 50-62)
with a three-way dispatch. `print` wins over `copy`, `copy` wins over `open`:

```go
	switch {
	case cfg.print:
		fmt.Println(webURL)
	case cfg.copy:
		if err := copyToClipboard(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error copying to clipboard: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("URL copied to clipboard: %s\n", webURL)
	default:
		fmt.Printf("Opening: %s\n", webURL)
		if err := openBrowser(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
			os.Exit(1)
		}
	}
```

- [ ] **Step 8: Update the three completion scripts**

In `completion.go`, `bashCompletion`, replace the `compgen -W` word list with:

```
        COMPREPLY=($(compgen -W "-v --version -c --copy -p --print -r --remote -l --line --commit --completion" -- "${cur}"))
```

In `zshCompletion`, insert after the `--copy` line:

```
        '(-p --print)'{-p,--print}'[Print the URL to stdout and exit]' \
```

In `fishCompletion`, insert after the `--copy` line:

```
complete -c gopen -s p -l print -d 'Print the URL to stdout and exit' -f
```

- [ ] **Step 9: Verify the whole suite and the lint**

Run:
```bash
go build -o gopen . && go test ./... && golangci-lint run
```
Expected: build OK, all tests PASS, no lint findings.

- [ ] **Step 10: Verify the flag by hand**

Run:
```bash
./gopen --print README.md
```
Expected: exactly one line, a bare URL with no `Opening:` prefix, exit code 0,
no browser opened. Confirm it pipes cleanly: `./gopen -p README.md | cat`.

- [ ] **Step 11: Update the README**

Add `-p, --print` to the flags table and add a scripting example. Keep the
existing table formatting.

- [ ] **Step 12: Commit**

```bash
git add args.go args_test.go main.go completion.go README.md
git commit -m "feat(args): add --print to output the URL without side effects"
```

- [ ] **Step 13: Capture the baseline measurement**

This is the reference point for the whole plan. Build the release binary and
record all three metrics.

```bash
SCRATCH=/private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$SCRATCH/gopen-baseline" .
wc -c < "$SCRATCH/gopen-baseline" | tee "$SCRATCH/size-baseline.txt"
go tool nm -size -sort size "$SCRATCH/gopen-baseline" > "$SCRATCH/nm-baseline.txt"
hyperfine --warmup 20 --min-runs 200 \
  --export-markdown "$SCRATCH/hyperfine-baseline.md" \
  "$SCRATCH/gopen-baseline --print README.md"
```

Record the numbers in the task notes. Do not commit the scratch artifacts.

---

### Task 2: The git config parser

The trickiest piece. It returns an **ordered slice**, not a map, because
`git remote get-url` returns the **first** `url` value while `git config --get`
returns the **last**. gopen uses `get-url`, so order carries meaning and a map
would silently lose it.

**Files:**
- Create: `gitfile.go`
- Test: `gitfile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type configEntry struct { key, value string }` — `key` is the flattened
    lowercase form `section.subsection.name`, subsection case preserved.
  - `func parseGitConfig(r io.Reader) ([]configEntry, error)`
  - `func firstConfigValue(entries []configEntry, key string) (string, bool)`

- [ ] **Step 1: Write the failing parser tests**

Create `gitfile_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestParseGitConfig(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []configEntry
	}{
		{
			name:  "simple section and key",
			input: "[core]\n\tbare = false\n",
			want:  []configEntry{{"core.bare", "false"}},
		},
		{
			name:  "quoted subsection preserves case",
			input: "[remote \"Origin\"]\n\turl = https://example.com/r.git\n",
			want:  []configEntry{{"remote.Origin.url", "https://example.com/r.git"}},
		},
		{
			name:  "dotted short-form subsection is lowercased",
			input: "[branch.Main]\n\tremote = origin\n",
			want:  []configEntry{{"branch.main.remote", "origin"}},
		},
		{
			name:  "section and key names are case-insensitive",
			input: "[CORE]\n\tBare = true\n",
			want:  []configEntry{{"core.bare", "true"}},
		},
		{
			name:  "key without value is an implicit true",
			input: "[core]\n\tbare\n",
			want:  []configEntry{{"core.bare", "true"}},
		},
		{
			name:  "hash and semicolon comments are ignored",
			input: "# lead\n[core]\n; mid\n\tbare = false # trail\n",
			want:  []configEntry{{"core.bare", "false"}},
		},
		{
			name:  "quoted value keeps inner spaces and hash",
			input: "[user]\n\tname = \"Ada # Lovelace\"\n",
			want:  []configEntry{{"user.name", "Ada # Lovelace"}},
		},
		{
			name:  "escape sequences inside a quoted value",
			input: "[x]\n\ty = \"a\\tb\\nc\\\\d\\\"e\"\n",
			want:  []configEntry{{"x.y", "a\tb\nc\\d\"e"}},
		},
		{
			name:  "line continuation joins values",
			input: "[x]\n\ty = one\\\ntwo\n",
			want:  []configEntry{{"x.y", "onetwo"}},
		},
		{
			name:  "multiple values for the same key keep file order",
			input: "[remote \"origin\"]\n\turl = first\n\tfetch = f\n\turl = second\n",
			want: []configEntry{
				{"remote.origin.url", "first"},
				{"remote.origin.fetch", "f"},
				{"remote.origin.url", "second"},
			},
		},
		{
			name:  "repeated sections are concatenated in order",
			input: "[remote \"origin\"]\n\turl = a\n[core]\n\tbare = false\n[remote \"origin\"]\n\turl = b\n",
			want: []configEntry{
				{"remote.origin.url", "a"},
				{"core.bare", "false"},
				{"remote.origin.url", "b"},
			},
		},
		{
			name:  "blank lines and stray whitespace",
			input: "\n  [core]  \n\n   bare   =   false   \n\n",
			want:  []configEntry{{"core.bare", "false"}},
		},
		{
			name:  "empty input yields no entries",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGitConfig(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("parseGitConfig() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseGitConfig_Malformed(t *testing.T) {
	// Malformed input must error so the caller falls back to git rather than
	// guessing. Never return a partial result as if it were authoritative.
	tests := []struct {
		name  string
		input string
	}{
		{"unclosed section header", "[core\n\tbare = false\n"},
		{"key outside any section", "bare = false\n"},
		{"unterminated quoted value", "[user]\n\tname = \"unclosed\n"},
		{"empty section name", "[]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseGitConfig(strings.NewReader(tt.input)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestFirstConfigValue(t *testing.T) {
	entries := []configEntry{
		{"remote.origin.url", "first"},
		{"core.bare", "false"},
		{"remote.origin.url", "second"},
	}

	t.Run("returns the first match, matching git remote get-url", func(t *testing.T) {
		got, ok := firstConfigValue(entries, "remote.origin.url")
		if !ok {
			t.Fatal("expected the key to be found")
		}
		if got != "first" {
			t.Errorf("got %q, want %q — git remote get-url returns the first URL", got, "first")
		}
	})

	t.Run("reports a missing key", func(t *testing.T) {
		if _, ok := firstConfigValue(entries, "remote.upstream.url"); ok {
			t.Error("expected the key to be absent")
		}
	})
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -run 'TestParseGitConfig|TestFirstConfigValue' ./...`
Expected: compile error, `undefined: parseGitConfig`, `undefined: configEntry`.

- [ ] **Step 3: Write the parser**

Create `gitfile.go`:

```go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// configEntry is one key/value pair from a git config file. Entries are kept in
// file order because order is significant: `git remote get-url` returns the
// first url value for a remote, while `git config --get` returns the last.
type configEntry struct {
	key   string
	value string
}

// parseGitConfig reads git's INI-like config format and returns every entry in
// file order. Keys are flattened to section.subsection.name; section and name
// are lowercased, a quoted subsection keeps its case.
//
// Malformed input is an error rather than a partial result: the caller falls
// back to the git binary instead of acting on a half-understood file.
func parseGitConfig(r io.Reader) ([]configEntry, error) {
	var (
		entries []configEntry
		section string // already lowercased, subsection appended, "" until the first header
	)

	sc := bufio.NewScanner(r)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' {
			s, err := parseSectionHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			section = s
			continue
		}

		if section == "" {
			return nil, fmt.Errorf("line %d: key %q outside of any section", lineNo, line)
		}

		// A value ending in a backslash continues on the next line.
		for strings.HasSuffix(line, `\`) && !strings.HasSuffix(line, `\\`) {
			if !sc.Scan() {
				return nil, fmt.Errorf("line %d: dangling line continuation", lineNo)
			}
			lineNo++
			line = line[:len(line)-1] + strings.TrimSpace(sc.Text())
		}

		name, value, err := parseKeyValue(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		entries = append(entries, configEntry{key: section + "." + name, value: value})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading git config: %w", err)
	}
	return entries, nil
}

// parseSectionHeader turns "[remote \"origin\"]" or "[branch.Main]" into the
// flattened prefix "remote.origin" / "branch.main".
func parseSectionHeader(line string) (string, error) {
	if !strings.HasSuffix(line, "]") {
		return "", errors.New("unterminated section header")
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	if inner == "" {
		return "", errors.New("empty section name")
	}

	// Quoted subsection: [section "SubSection"] — case is preserved.
	if i := strings.IndexByte(inner, '"'); i >= 0 {
		if !strings.HasSuffix(inner, `"`) || i == len(inner)-1 {
			return "", errors.New("unterminated subsection name")
		}
		name := strings.ToLower(strings.TrimSpace(inner[:i]))
		sub := unescapeSubsection(inner[i+1 : len(inner)-1])
		if name == "" {
			return "", errors.New("empty section name")
		}
		return name + "." + sub, nil
	}

	// Dotted short form: [branch.Main] — the subsection is lowercased.
	return strings.ToLower(inner), nil
}

// unescapeSubsection handles the only two escapes git allows in a subsection
// name: \" and \\.
func unescapeSubsection(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseKeyValue splits "name = value" into its parts. A bare name is an
// implicit boolean true, which is how git reads it.
func parseKeyValue(line string) (name, value string, err error) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		name = strings.ToLower(strings.TrimSpace(stripInlineComment(line)))
		if name == "" {
			return "", "", errors.New("empty key name")
		}
		return name, "true", nil
	}

	name = strings.ToLower(strings.TrimSpace(line[:eq]))
	if name == "" {
		return "", "", errors.New("empty key name")
	}
	value, err = parseValue(strings.TrimSpace(line[eq+1:]))
	if err != nil {
		return "", "", err
	}
	return name, value, nil
}

// stripInlineComment drops everything from the first unquoted # or ;.
func stripInlineComment(s string) string {
	if i := strings.IndexAny(s, "#;"); i >= 0 {
		return s[:i]
	}
	return s
}

// parseValue interprets a config value: quoted spans keep their whitespace and
// comment characters, backslash escapes are expanded, and an unquoted trailing
// comment is dropped.
func parseValue(s string) (string, error) {
	var (
		b       strings.Builder
		inQuote bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == '\\':
			if i+1 >= len(s) {
				return "", errors.New("dangling escape in value")
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case '"', '\\':
				b.WriteByte(s[i])
			default:
				return "", fmt.Errorf("unknown escape %q in value", s[i])
			}
		case (c == '#' || c == ';') && !inQuote:
			return strings.TrimRight(b.String(), " \t"), nil
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return "", errors.New("unterminated quoted value")
	}
	return strings.TrimRight(b.String(), " \t"), nil
}

// firstConfigValue returns the first value for key in file order. This matches
// `git remote get-url`, which returns a remote's first url, and deliberately
// differs from `git config --get`, which returns the last.
func firstConfigValue(entries []configEntry, key string) (string, bool) {
	for _, e := range entries {
		if e.key == key {
			return e.value, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -run 'TestParseGitConfig|TestFirstConfigValue' -v ./...`
Expected: every subtest PASS.

- [ ] **Step 5: Cross-check the parser against real git**

The unit tests encode assumptions. Verify them against the real thing:

```bash
D=$(mktemp -d) && cd "$D" && git init -q .
printf '[remote "origin"]\n\turl = first\n\turl = second\n[user]\n\tname = "Ada # Lovelace"\n' >> .git/config
git remote get-url origin        # expect: first
git config --get user.name       # expect: Ada # Lovelace
cd - >/dev/null && rm -rf "$D"
```

If either differs from what the tests assert, the tests are wrong, not git.
Fix the tests and the parser.

- [ ] **Step 6: Lint and commit**

```bash
golangci-lint run
git add gitfile.go gitfile_test.go
git commit -m "feat(git): add an ordered git config parser"
```

---

### Task 3: Read the current branch from HEAD

**Files:**
- Modify: `gitfile.go`
- Test: `gitfile_test.go`

**Interfaces:**
- Consumes: nothing from Task 2.
- Produces: `func branchFromHEAD(gitDir string) (string, error)`. Returns the
  short branch name, or the literal `"HEAD"` when detached, mirroring
  `git rev-parse --abbrev-ref HEAD`.

- [ ] **Step 1: Write the failing tests**

Append to `gitfile_test.go`:

```go
func TestBranchFromHEAD(t *testing.T) {
	tests := []struct {
		name    string
		head    string
		want    string
		wantErr bool
	}{
		{
			name: "simple branch",
			head: "ref: refs/heads/main\n",
			want: "main",
		},
		{
			name: "slashes in the branch name are preserved",
			head: "ref: refs/heads/feature/foo/bar\n",
			want: "feature/foo/bar",
		},
		{
			name: "no trailing newline",
			head: "ref: refs/heads/main",
			want: "main",
		},
		{
			name: "detached HEAD returns the literal HEAD, as git does",
			head: "9f2c1b7e4a8d3f6019b5c2e7a4d8f1b3c6e9a2d5\n",
			want: "HEAD",
		},
		{
			name:    "symref outside refs/heads is not a branch",
			head:    "ref: refs/tags/v1.0.0\n",
			wantErr: true,
		},
		{
			name:    "garbage",
			head:    "not a ref at all\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			head:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte(tt.head), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := branchFromHEAD(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("branchFromHEAD() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("branchFromHEAD() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("missing HEAD file errors", func(t *testing.T) {
		if _, err := branchFromHEAD(t.TempDir()); err == nil {
			t.Error("expected an error for a missing HEAD file")
		}
	})
}
```

Add `"os"` and `"path/filepath"` to the test file imports.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -run TestBranchFromHEAD ./...`
Expected: compile error, `undefined: branchFromHEAD`.

- [ ] **Step 3: Implement it**

Append to `gitfile.go`:

```go
const headRefPrefix = "refs/heads/"

// branchFromHEAD reads gitDir/HEAD and returns the short branch name.
// A detached HEAD yields the literal "HEAD", which is what
// `git rev-parse --abbrev-ref HEAD` prints in that state.
func branchFromHEAD(gitDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}
	head := strings.TrimSpace(string(raw))

	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		branch, ok := strings.CutPrefix(strings.TrimSpace(ref), headRefPrefix)
		if !ok || branch == "" {
			return "", fmt.Errorf("HEAD points at %q, which is not a branch", ref)
		}
		return branch, nil
	}

	if isHexSHA(head) {
		return "HEAD", nil // detached
	}
	return "", fmt.Errorf("unrecognized HEAD content: %q", head)
}

// isHexSHA reports whether s looks like a git object id.
func isHexSHA(s string) bool {
	// 40 hex chars for SHA-1, 64 for SHA-256.
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
```

Add `"os"` and `"path/filepath"` to the `gitfile.go` imports.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -run TestBranchFromHEAD -v ./...`
Expected: every subtest PASS.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run
git add gitfile.go gitfile_test.go
git commit -m "feat(git): read the current branch from .git/HEAD"
```

---

### Task 4: Discover the git directory

Handles the three shapes `.git` can take: a directory (normal repo), or a file
containing `gitdir: <path>` (linked worktree, submodule). In a linked worktree
`HEAD` lives in the worktree's own gitdir but `config` lives in the shared
common dir, so both paths must be returned separately.

**Files:**
- Modify: `gitfile.go`
- Test: `gitfile_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 2-3.
- Produces:
  `func discoverGitDir(start string) (gitDir, commonDir, workTree string, err error)`.
  All three are absolute paths. `workTree` is the directory containing the `.git`
  entry that was found.

- [ ] **Step 1: Write the failing tests**

Append to `gitfile_test.go`:

```go
func TestDiscoverGitDir(t *testing.T) {
	t.Run("plain repo from its root", func(t *testing.T) {
		root := newTmpGitRepo(t)
		gitDir, commonDir, workTree, err := discoverGitDir(root)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		wantGit := filepath.Join(root, ".git")
		if gitDir != wantGit {
			t.Errorf("gitDir = %q, want %q", gitDir, wantGit)
		}
		if commonDir != wantGit {
			t.Errorf("commonDir = %q, want %q", commonDir, wantGit)
		}
		if workTree != root {
			t.Errorf("workTree = %q, want %q", workTree, root)
		}
	})

	t.Run("walks up from a nested subdirectory", func(t *testing.T) {
		root := newTmpGitRepo(t)
		nested := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		_, _, workTree, err := discoverGitDir(nested)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		if workTree != root {
			t.Errorf("workTree = %q, want %q", workTree, root)
		}
	})

	t.Run("linked worktree splits gitDir from commonDir", func(t *testing.T) {
		root := newTmpGitRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		runGit(t, root, "worktree", "add", "-b", "wt-branch", wt)

		gitDir, commonDir, workTree, err := discoverGitDir(wt)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		if workTree != wt {
			t.Errorf("workTree = %q, want %q", workTree, wt)
		}
		if gitDir == commonDir {
			t.Errorf("gitDir and commonDir must differ in a linked worktree, both = %q", gitDir)
		}
		// HEAD lives in the worktree gitdir, config in the common dir.
		assertFileExists(t, filepath.Join(gitDir, "HEAD"))
		assertFileExists(t, filepath.Join(commonDir, "config"))
	})

	t.Run("outside any repo returns an error", func(t *testing.T) {
		if _, _, _, err := discoverGitDir(t.TempDir()); err == nil {
			t.Error("expected an error outside a repository")
		}
	})
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}
```

Also add this helper to `git_test.go`, next to `newTmpGitRepo`, and refactor
`newTmpGitRepo` to use it:

```go
// runGit runs a git command in dir and fails the test if it errors.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -run TestDiscoverGitDir ./...`
Expected: compile error, `undefined: discoverGitDir`.

- [ ] **Step 3: Implement it**

Append to `gitfile.go`:

```go
// discoverGitDir walks up from start until it finds a .git entry.
//
// A .git directory is the normal case. A .git file holds "gitdir: <path>" and
// appears in linked worktrees and submodules; there, HEAD lives in gitDir while
// config lives in the shared commonDir, so the two are returned separately.
func discoverGitDir(start string) (gitDir, commonDir, workTree string, err error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to resolve %q: %w", start, err)
	}

	for {
		candidate := filepath.Join(dir, ".git")
		info, statErr := os.Stat(candidate)
		switch {
		case statErr != nil:
			// keep walking up
		case info.IsDir():
			return candidate, candidate, dir, nil
		default:
			gitDir, err := readGitDirFile(candidate)
			if err != nil {
				return "", "", "", err
			}
			return gitDir, resolveCommonDir(gitDir), dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return "", "", "", errors.New("not in a git repository")
		}
		dir = parent
	}
}

// readGitDirFile parses a ".git" file of the form "gitdir: <path>". A relative
// path is resolved against the directory holding the .git file.
func readGitDirFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("%s is not a valid gitdir pointer", path)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("%s has an empty gitdir pointer", path)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}

// resolveCommonDir returns the shared git directory for a linked worktree.
// Absent a commondir file, gitDir is already the common dir.
func resolveCommonDir(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	common := strings.TrimSpace(string(raw))
	if common == "" {
		return gitDir
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return filepath.Clean(common)
}
```

Add `"errors"` to the `gitfile.go` imports if Task 2 did not already.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -run TestDiscoverGitDir -v ./...`
Expected: every subtest PASS, including the linked-worktree case.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run
git add gitfile.go gitfile_test.go git_test.go
git commit -m "feat(git): discover the git dir, handling worktrees and submodules"
```

---

### Task 5: Detect when the fast path cannot be trusted

`url.<base>.insteadOf` rewrites a remote URL and is common in corporate setups.
Missing it would produce a wrong URL with no warning, which the invariant
forbids. `include`/`includeIf` can pull in a config file we do not read at all.
Detection is a raw substring scan, no parsing, costing a few small file reads
against the ~16.5 ms of a single fork.

**Files:**
- Modify: `gitfile.go`
- Test: `gitfile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func needsGitFallback(commonDir string) bool`.

- [ ] **Step 1: Write the failing tests**

Append to `gitfile_test.go`:

```go
// pinConfigScope isolates a test from the machine's real git configuration.
func pinConfigScope(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")
}

// writeConfig creates a config file in a fresh dir and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNeedsGitFallback(t *testing.T) {
	cleanLocal := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config"),
			[]byte("[remote \"origin\"]\n\turl = https://example.com/r.git\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("clean scope does not need the fallback", func(t *testing.T) {
		pinConfigScope(t)
		if needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = true, want false for a clean scope")
		}
	})

	t.Run("GIT_DIR forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_DIR", "/somewhere/.git")
		if !needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = false, want true when GIT_DIR is set")
		}
	})

	t.Run("GIT_WORK_TREE forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_WORK_TREE", "/somewhere")
		if !needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = false, want true when GIT_WORK_TREE is set")
		}
	})

	t.Run("insteadOf in the global config forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t,
			"[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"))
		if !needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = false, want true when insteadOf is configured")
		}
	})

	t.Run("insteadOf detection is case-insensitive", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t,
			"[url \"x\"]\n\tINSTEADOF = y\n"))
		if !needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = false, want true for uppercase INSTEADOF")
		}
	})

	t.Run("includeIf forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t,
			"[includeIf \"gitdir:~/work/\"]\n\tpath = ~/work/.gitconfig\n"))
		if !needsGitFallback(cleanLocal(t)) {
			t.Error("needsGitFallback() = false, want true when includeIf is configured")
		}
	})

	t.Run("insteadOf in the local config forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config"),
			[]byte("[url \"a\"]\n\tinsteadOf = b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !needsGitFallback(dir) {
			t.Error("needsGitFallback() = false, want true for a local insteadOf")
		}
	})

	t.Run("a missing config file is not a reason to fall back", func(t *testing.T) {
		pinConfigScope(t)
		if needsGitFallback(t.TempDir()) {
			t.Error("needsGitFallback() = true, want false when files are simply absent")
		}
	})
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -run TestNeedsGitFallback ./...`
Expected: compile error, `undefined: needsGitFallback`.

- [ ] **Step 3: Implement it**

Append to `gitfile.go`:

```go
// fallbackMarkers are config directives the fast path does not implement.
// Their mere presence anywhere in scope sends the whole lookup to git, because
// any of them can change the remote URL we would otherwise report.
var fallbackMarkers = []string{"insteadof", "[include", "includeif"}

// needsGitFallback reports whether the pure-Go path must defer to the git
// binary. It is deliberately conservative: a false positive costs one fork, a
// false negative costs a wrong URL.
func needsGitFallback(commonDir string) bool {
	if os.Getenv("GIT_DIR") != "" || os.Getenv("GIT_WORK_TREE") != "" {
		return true
	}
	for _, path := range configScopePaths(commonDir) {
		if fileContainsMarker(path) {
			return true
		}
	}
	return false
}

// configScopePaths lists every config file git would consult, in scope order.
// Paths that do not exist are harmless; the caller just cannot read them.
func configScopePaths(commonDir string) []string {
	var paths []string

	if os.Getenv("GIT_CONFIG_NOSYSTEM") == "" {
		if p := os.Getenv("GIT_CONFIG_SYSTEM"); p != "" {
			paths = append(paths, p)
		} else {
			paths = append(paths, "/etc/gitconfig")
		}
	}

	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		paths = append(paths, p)
	} else {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			paths = append(paths, filepath.Join(xdg, "git", "config"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".gitconfig"))
		}
	}

	if commonDir != "" {
		paths = append(paths, filepath.Join(commonDir, "config"))
	}
	return paths
}

// fileContainsMarker reports whether path holds any fallback marker. An
// unreadable or absent file is not a marker: it simply contributes nothing.
func fileContainsMarker(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(raw))
	for _, m := range fallbackMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -run TestNeedsGitFallback -v ./...`
Expected: every subtest PASS.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run
git add gitfile.go gitfile_test.go
git commit -m "feat(git): detect config directives the fast path cannot honor"
```

---

### Task 6: Assemble the fast path and wire the fallback

This is where the two paths meet. It also fixes the pre-existing symlink
inconsistency: `getRepoRoot()` returns a symlink-resolved path while
`targetPath` from `resolvePath()` is not resolved, which breaks `relPath` on
macOS. The fast path walks up from `targetPath` so both live in the same
namespace; the fallback branch now resolves `targetPath` to match.

**Files:**
- Modify: `gitfile.go`
- Modify: `git.go:59-102` (`getRepoContext`)
- Test: `gitfile_test.go`, `git_test.go`

**Interfaces:**
- Consumes: `parseGitConfig`, `firstConfigValue`, `branchFromHEAD`,
  `discoverGitDir`, `needsGitFallback` from Tasks 2-5; `convertToHTTPS` from
  `git.go`.
- Produces:
  - `func readRepoContextFromDisk(targetPath, remoteName string) (repoContext, error)`
  - `func repoContextViaGit(targetPath, remoteName string) (repoContext, error)`
    (the current subprocess body, extracted verbatim plus the EvalSymlinks fix)
  - `getRepoContext` keeps its existing signature and behaviour.

- [ ] **Step 1: Write the failing test**

Append to `gitfile_test.go`:

```go
func TestReadRepoContextFromDisk(t *testing.T) {
	pinConfigScope(t)
	root := newTmpGitRepo(t)
	runGit(t, root, "remote", "add", "origin", "git@github.com:example/repo.git")

	t.Run("repo root yields an empty relPath", func(t *testing.T) {
		ctx, err := readRepoContextFromDisk(root, "origin")
		if err != nil {
			t.Fatalf("readRepoContextFromDisk() error = %v", err)
		}
		if ctx.relPath != "" {
			t.Errorf("relPath = %q, want empty", ctx.relPath)
		}
		if ctx.baseURL != "https://github.com/example/repo" {
			t.Errorf("baseURL = %q, want the HTTPS form", ctx.baseURL)
		}
		if ctx.branch == "" {
			t.Error("branch is empty")
		}
	})

	t.Run("nested file yields the right relPath without symlink munging", func(t *testing.T) {
		nested := filepath.Join(root, "pkg", "util")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(nested, "helper.go")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		ctx, err := readRepoContextFromDisk(file, "origin")
		if err != nil {
			t.Fatalf("readRepoContextFromDisk() error = %v", err)
		}
		want := filepath.Join("pkg", "util", "helper.go")
		if ctx.relPath != want {
			t.Errorf("relPath = %q, want %q", ctx.relPath, want)
		}
		if strings.Contains(ctx.relPath, "..") {
			t.Errorf("relPath %q escapes the repo root: symlink namespaces disagree", ctx.relPath)
		}
	})

	t.Run("unknown remote errors", func(t *testing.T) {
		if _, err := readRepoContextFromDisk(root, "nope"); err == nil {
			t.Error("expected an error for an unknown remote")
		}
	})

	t.Run("outside a repo errors", func(t *testing.T) {
		if _, err := readRepoContextFromDisk(t.TempDir(), "origin"); err == nil {
			t.Error("expected an error outside a repository")
		}
	})
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test -run TestReadRepoContextFromDisk ./...`
Expected: compile error, `undefined: readRepoContextFromDisk`.

- [ ] **Step 3: Implement the fast path**

Append to `gitfile.go`:

```go
// readRepoContextFromDisk builds a repoContext by reading .git directly, with
// no subprocess. It errors rather than guessing whenever the on-disk state is
// not something it fully understands, so the caller can fall back to git.
func readRepoContextFromDisk(targetPath, remoteName string) (repoContext, error) {
	dir, err := containingDir(targetPath)
	if err != nil {
		return repoContext{}, err
	}

	gitDir, commonDir, workTree, err := discoverGitDir(dir)
	if err != nil {
		return repoContext{}, err
	}

	branch, err := branchFromHEAD(gitDir)
	if err != nil {
		return repoContext{}, err
	}

	f, err := os.Open(filepath.Join(commonDir, "config"))
	if err != nil {
		return repoContext{}, fmt.Errorf("failed to open git config: %w", err)
	}
	defer f.Close()

	entries, err := parseGitConfig(f)
	if err != nil {
		return repoContext{}, err
	}

	remoteURL, ok := firstConfigValue(entries, "remote."+remoteName+".url")
	if !ok {
		return repoContext{}, fmt.Errorf("no URL configured for remote %q", remoteName)
	}

	relPath, err := relativeToRoot(workTree, targetPath)
	if err != nil {
		return repoContext{}, err
	}

	return repoContext{
		baseURL: convertToHTTPS(remoteURL),
		branch:  branch,
		relPath: relPath,
	}, nil
}

// containingDir returns targetPath itself when it is a directory, otherwise its
// parent.
func containingDir(targetPath string) (string, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() {
		return targetPath, nil
	}
	return filepath.Dir(targetPath), nil
}

// relativeToRoot computes the repo-relative path, mapping the root itself to "".
func relativeToRoot(root, targetPath string) (string, error) {
	rel, err := filepath.Rel(root, targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test -run TestReadRepoContextFromDisk -v ./...`
Expected: every subtest PASS.

- [ ] **Step 5: Restructure `getRepoContext` into an orchestrator**

In `git.go`, replace the whole body of `getRepoContext` (lines 59-102) with the
dispatcher below, and move the old body into `repoContextViaGit`. Note the two
changes to the old body: it now uses the shared `containingDir` and
`relativeToRoot` helpers, and it resolves symlinks on `targetPath` so its
namespace matches what `git rev-parse --show-toplevel` returns.

```go
// getRepoContext collects all git information needed to build the web URL.
//
// It prefers reading .git directly, which avoids four subprocess forks, and
// defers to the git binary whenever the fast path cannot be certain of the
// result. Correctness always wins over speed: never return a URL that differs
// from what git would have produced.
func getRepoContext(targetPath, remoteName string) (repoContext, error) {
	dir, err := containingDir(targetPath)
	if err != nil {
		return repoContext{}, err
	}

	// commonDir is best-effort here: if discovery fails, configScopePaths still
	// covers the global and system scopes, and the fast path will fail anyway.
	_, commonDir, _, discoverErr := discoverGitDir(dir)

	if discoverErr == nil && !needsGitFallback(commonDir) {
		if ctx, err := readRepoContextFromDisk(targetPath, remoteName); err == nil {
			return ctx, nil
		}
		// Fall through: an unparsable repo is git's problem, not ours.
	}
	return repoContextViaGit(targetPath, remoteName)
}

// repoContextViaGit is the subprocess fallback: four git invocations.
func repoContextViaGit(targetPath, remoteName string) (repoContext, error) {
	dir, err := containingDir(targetPath)
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

	// git reports a symlink-resolved root, so resolve the target too or the two
	// paths end up in different namespaces and relPath fills with "..".
	// On macOS /var is a symlink to /private/var, which makes this routine.
	resolvedTarget := targetPath
	if r, err := filepath.EvalSymlinks(targetPath); err == nil {
		resolvedTarget = r
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
```

Remove the now-unused `"fmt"` usages only if the compiler complains; `git.go`
still needs `errors`, `fmt`, `os/exec`, `path/filepath` and `strings`. Drop
`"os"` from the imports if `os.Stat` was its only use, since `containingDir`
now lives in `gitfile.go`.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./... && golangci-lint run`
Expected: all PASS, no lint findings. The existing `TestGetRepoContext`
(`git_test.go:213`) must still pass unchanged.

- [ ] **Step 7: Commit**

```bash
git add git.go gitfile.go gitfile_test.go
git commit -m "refactor(git): read repo context from disk with a git fallback"
```

---

### Task 7: The differential test

This is the safety net that actually enforces the invariant. Asserting
hardcoded values only proves the fast path matches what the plan author
imagined; asserting equality with the subprocess path proves it matches **git**.
Every fixture goes through both paths and the results must be identical.

**Files:**
- Modify: `gitfile_test.go`

**Interfaces:**
- Consumes: `readRepoContextFromDisk`, `repoContextViaGit` from Task 6;
  `newTmpGitRepo`, `runGit` from `git_test.go`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the differential test**

Append to `gitfile_test.go`:

```go
// TestDifferential_FastPathMatchesGit is the core guarantee of this design:
// for every repository shape gopen supports, reading .git directly must yield
// exactly what shelling out to git yields. A divergence here is a bug that
// would send the user to the wrong URL.
func TestDifferential_FastPathMatchesGit(t *testing.T) {
	type fixture struct {
		name   string
		build  func(t *testing.T) (targetPath string)
		remote string
	}

	fixtures := []fixture{
		{
			name:   "plain repo, HTTPS remote, root",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				return root
			},
		},
		{
			name:   "plain repo, SSH remote, root",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "git@github.com:example/repo.git")
				return root
			},
		},
		{
			name:   "nested file",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				nested := filepath.Join(root, "pkg", "util")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(nested, "helper.go")
				if err := os.WriteFile(file, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return file
			},
		},
		{
			name:   "branch name containing slashes",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "checkout", "-b", "feature/foo/bar")
				return root
			},
		},
		{
			name:   "detached HEAD",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "checkout", "--detach", "HEAD")
				return root
			},
		},
		{
			name:   "non-default remote name",
			remote: "upstream",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/fork.git")
				runGit(t, root, "remote", "add", "upstream", "https://gitlab.com/example/up.git")
				return root
			},
		},
		{
			name:   "remote with several url lines, first must win",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/first.git")
				runGit(t, root, "remote", "set-url", "--add", "origin", "https://github.com/example/second.git")
				return root
			},
		},
		{
			name:   "linked worktree",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				wt := filepath.Join(t.TempDir(), "wt")
				runGit(t, root, "worktree", "add", "-b", "wt-branch", wt)
				return wt
			},
		},
		{
			name:   "file inside a linked worktree",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				wt := filepath.Join(t.TempDir(), "wt")
				runGit(t, root, "worktree", "add", "-b", "wt2", wt)
				file := filepath.Join(wt, "inside.go")
				if err := os.WriteFile(file, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return file
			},
		},
		{
			name:   "submodule",
			remote: "origin",
			build: func(t *testing.T) string {
				inner := newTmpGitRepo(t)
				runGit(t, inner, "remote", "add", "origin", "https://github.com/example/inner.git")

				outer := newTmpGitRepo(t)
				runGit(t, outer, "remote", "add", "origin", "https://github.com/example/outer.git")
				runGit(t, outer, "-c", "protocol.file.allow=always",
					"submodule", "add", inner, "vendor/inner")
				return filepath.Join(outer, "vendor", "inner")
			},
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			pinConfigScope(t)
			target := f.build(t)

			fast, fastErr := readRepoContextFromDisk(target, f.remote)
			slow, slowErr := repoContextViaGit(target, f.remote)

			if (fastErr == nil) != (slowErr == nil) {
				t.Fatalf("error disagreement:\n  fast path: %v\n  git path:  %v", fastErr, slowErr)
			}
			if fastErr != nil {
				return // both failed, which is consistent
			}
			if fast != slow {
				t.Errorf("fast path diverges from git:\n  fast: %+v\n  git:  %+v", fast, slow)
			}
		})
	}
}

// TestDifferential_ErrorCases checks the two paths agree on failure too.
func TestDifferential_ErrorCases(t *testing.T) {
	t.Run("unknown remote", func(t *testing.T) {
		pinConfigScope(t)
		root := newTmpGitRepo(t)
		runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
		_, fastErr := readRepoContextFromDisk(root, "nope")
		_, slowErr := repoContextViaGit(root, "nope")
		if (fastErr == nil) != (slowErr == nil) {
			t.Errorf("disagreement: fast=%v git=%v", fastErr, slowErr)
		}
	})

	t.Run("outside any repository", func(t *testing.T) {
		pinConfigScope(t)
		dir := t.TempDir()
		_, fastErr := readRepoContextFromDisk(dir, "origin")
		_, slowErr := repoContextViaGit(dir, "origin")
		if (fastErr == nil) != (slowErr == nil) {
			t.Errorf("disagreement: fast=%v git=%v", fastErr, slowErr)
		}
	})
}
```

- [ ] **Step 2: Run the differential test**

Run: `go test -run TestDifferential -v ./...`
Expected: every fixture PASS.

If the submodule fixture fails because git refuses a local-file submodule, keep
the `-c protocol.file.allow=always` flag shown above; it is required on git
2.38 and newer. If it still fails for an unrelated reason, skip that fixture
with `t.Skip` and a comment naming the reason, rather than deleting it.

- [ ] **Step 3: Investigate any divergence before touching the test**

A failure here means the fast path is wrong. Fix `gitfile.go`, never the
assertion. The only case where the test is at fault is when git's own behaviour
was mis-stated; verify with a real command before concluding that.

- [ ] **Step 4: Add the benchmark**

Append to `gitfile_test.go`:

```go
func BenchmarkGetRepoContext(b *testing.B) {
	// A throwaway repo shared by both variants.
	dir := b.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "bench@test.com"},
		{"config", "user.name", "Bench"},
		{"commit", "--allow-empty", "-m", "init"},
		{"remote", "add", "origin", "https://github.com/example/repo.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	b.Run("pure-go", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := readRepoContextFromDisk(dir, "origin"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("subprocess", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := repoContextViaGit(dir, "origin"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
```

Add `"os/exec"` to the `gitfile_test.go` imports.

- [ ] **Step 5: Run the benchmark and record the numbers**

Run:
```bash
go test -bench=BenchmarkGetRepoContext -benchmem -count=10 -run='^$' ./... \
  | tee /private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad/bench.txt
```
Expected: `pure-go` in the tens of microseconds, `subprocess` in the tens of
milliseconds. Record both, with allocation counts.

- [ ] **Step 6: Run the full suite with the race detector, as CI does**

Run: `go test -race ./... && golangci-lint run`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add gitfile_test.go
git commit -m "test(git): assert the fast path matches git on every repo shape"
```

---

### Task 8: Final measurements and documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: the baseline captured in Task 1 Step 13, the benchmark from Task 7.
- Produces: the comparison report.

- [ ] **Step 1: Build the new release binary and measure its size**

```bash
SCRATCH=/private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$SCRATCH/gopen-new" .
echo "baseline: $(cat "$SCRATCH/size-baseline.txt") bytes"
echo "new:      $(wc -c < "$SCRATCH/gopen-new") bytes"
go tool nm -size -sort size "$SCRATCH/gopen-new" > "$SCRATCH/nm-new.txt"
diff <(cut -c1-40 "$SCRATCH/nm-baseline.txt") <(cut -c1-40 "$SCRATCH/nm-new.txt") | head -40
```

Alert threshold: a growth above **+50 KB** means something unexpected got
linked in. Use the `nm` diff to find it before proceeding.

- [ ] **Step 2: Measure end-to-end with hyperfine**

```bash
SCRATCH=/private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad
hyperfine --warmup 20 --min-runs 200 \
  --export-markdown "$SCRATCH/hyperfine-compare.md" \
  -n baseline "$SCRATCH/gopen-baseline --print README.md" \
  -n pure-go  "$SCRATCH/gopen-new --print README.md"
```

Both binaries must be invoked from this repository so they see the same repo.

- [ ] **Step 3: Non-regression on real repositories**

```bash
SCRATCH=/private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad
WT=$(mktemp -d)/wt
git worktree add -b tmp-measure-wt "$WT"

for target in . README.md git.go docs; do
  a=$("$SCRATCH/gopen-baseline" --print "$target")
  b=$("$SCRATCH/gopen-new" --print "$target")
  [ "$a" = "$b" ] && echo "OK   $target  $b" || echo "DIFF $target
  baseline: $a
  new:      $b"
done

cd "$WT"
a=$("$SCRATCH/gopen-baseline" --print .)
b=$("$SCRATCH/gopen-new" --print .)
[ "$a" = "$b" ] && echo "OK   worktree  $b" || echo "DIFF worktree
  baseline: $a
  new:      $b"
cd -

git worktree remove --force "$WT" && git branch -D tmp-measure-wt
```

Every line must read `OK`. A `DIFF` is a release blocker.

- [ ] **Step 4: Verify the fallback actually triggers**

The fallback is only useful if it fires. Prove it does:

```bash
SCRATCH=/private/tmp/claude-503/-Users-jeremy-Documents-perso-git-gopen/f48e98cd-b3bc-443f-bc51-f46d4096d7bf/scratchpad
CFG=$(mktemp)
printf '[url "git@github.com:"]\n\tinsteadOf = https://github.com/\n' > "$CFG"
GIT_CONFIG_GLOBAL="$CFG" "$SCRATCH/gopen-new" --print README.md
GIT_CONFIG_GLOBAL="$CFG" "$SCRATCH/gopen-baseline" --print README.md
rm -f "$CFG"
```

Both must print the same URL. If they differ, `needsGitFallback` is not
catching the case.

- [ ] **Step 5: Update the README**

Add a short note that gopen reads `.git` directly and falls back to the `git`
binary for configurations it does not implement, so no behaviour changes. Keep
it to two or three sentences; the README is user-facing, not a design doc.

- [ ] **Step 6: Update CLAUDE.md**

In the Architecture section, add `gitfile.go` to the file table:

| `gitfile.go` | Pure-Go `.git` reader: `parseGitConfig()`, `branchFromHEAD()`, `discoverGitDir()`, `needsGitFallback()`, `readRepoContextFromDisk()` |

Update the `git.go` row to mention it now holds the subprocess fallback, and
add a Gotchas entry:

> - **Two paths in `getRepoContext()`**: the fast path reads `.git` directly;
>   `repoContextViaGit()` is the subprocess fallback, used when
>   `GIT_DIR`/`GIT_WORK_TREE` is set, when a config in scope mentions
>   `insteadOf`/`include`/`includeIf`, or when parsing fails. Any change must
>   keep `TestDifferential_FastPathMatchesGit` green: the two paths must never
>   disagree.
> - **First url wins**: `git remote get-url` returns a remote's *first* `url`
>   line, whereas `git config --get` returns the *last*. `firstConfigValue()`
>   matches `get-url`. Do not "fix" it into last-wins.

- [ ] **Step 7: Commit the docs**

```bash
git add README.md CLAUDE.md
git commit -m "docs: describe the pure-Go git reader and its fallback"
```

- [ ] **Step 8: Report the comparison**

Produce a table with baseline vs new for: binary size in bytes and delta,
`hyperfine` mean with standard deviation and speedup factor, and the Go
benchmark ns/op with allocations for both variants. State plainly whether the
+50 KB threshold was respected and whether every non-regression line read `OK`.

---

## Self-Review

Checked against the spec:

- Spec coverage: `discoverGitDir` Task 4, `parseGitConfig` Task 2,
  `remoteURLFromConfig` folded into `firstConfigValue` Task 2,
  `branchFromHEAD` Task 3, `needsGitFallback` Task 5, symlink fix Task 6,
  `--print` Task 1, all four measurements Tasks 1 and 8, every test fixture
  named in the spec appears in Task 7.
- Naming note: the spec called it `remoteURLFromConfig`; the plan implements
  the more general `firstConfigValue(entries, key)` and builds the key at the
  call site. Same behaviour, one fewer function.
- Type consistency: `configEntry`, `parseGitConfig`, `firstConfigValue`,
  `branchFromHEAD`, `discoverGitDir`, `needsGitFallback`,
  `readRepoContextFromDisk`, `repoContextViaGit`, `containingDir`,
  `relativeToRoot` are each defined once and used with matching signatures.
- `containingDir` and `relativeToRoot` are defined in Task 6 in `gitfile.go`
  and consumed by `git.go` in the same task, so no task references an
  undefined symbol.
