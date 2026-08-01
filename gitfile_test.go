package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			want:  []configEntry{{key: "core.bare", value: "false"}},
		},
		{
			name:  "quoted subsection preserves case",
			input: "[remote \"Origin\"]\n\turl = https://example.com/r.git\n",
			want:  []configEntry{{key: "remote.Origin.url", value: "https://example.com/r.git"}},
		},
		{
			name:  "dotted short-form subsection is lowercased",
			input: "[branch.Main]\n\tremote = origin\n",
			want:  []configEntry{{key: "branch.main.remote", value: "origin"}},
		},
		{
			name:  "section and key names are case-insensitive",
			input: "[CORE]\n\tBare = true\n",
			want:  []configEntry{{key: "core.bare", value: "true"}},
		},
		{
			name:  "hash and semicolon comments are ignored",
			input: "# lead\n[core]\n; mid\n\tbare = false # trail\n",
			want:  []configEntry{{key: "core.bare", value: "false"}},
		},
		{
			name:  "quoted value keeps inner spaces and hash",
			input: "[user]\n\tname = \"Ada # Lovelace\"\n",
			want:  []configEntry{{key: "user.name", value: "Ada # Lovelace"}},
		},
		{
			name:  "escape sequences inside a quoted value",
			input: "[x]\n\ty = \"a\\tb\\nc\\\\d\\\"e\"\n",
			want:  []configEntry{{key: "x.y", value: "a\tb\nc\\d\"e"}},
		},
		{
			name:  "line continuation joins values",
			input: "[x]\n\ty = one\\\ntwo\n",
			want:  []configEntry{{key: "x.y", value: "onetwo"}},
		},
		// The continuation cases below assert the exact bytes git 2.54 produces;
		// each was verified against `git config --file <f> --list -z` before
		// being written down. git's parse_value trims only *trailing*
		// whitespace, so a continuation line's leading whitespace is part of
		// the value.
		{
			name:  "continuation preserves the next line's leading whitespace",
			input: "[x]\n\ty = one\\\n   two\n",
			want:  []configEntry{{key: "x.y", value: "one   two"}},
		},
		{
			name:  "continuation inside a quoted value preserves leading whitespace",
			input: "[x]\n\ty = \"one\\\n   two\"\n",
			want:  []configEntry{{key: "x.y", value: "one   two"}},
		},
		{
			name:  "continuation preserves leading tabs in a URL",
			input: "[remote \"origin\"]\n\turl = https://example.com/a/\\\n\t\tb.git\n",
			want:  []configEntry{{key: "remote.origin.url", value: "https://example.com/a/\t\tb.git"}},
		},
		{
			// Trailing run of 3: one escaped pair plus a continuation marker.
			name:  "odd backslash run of three continues the line",
			input: "[x]\n\ty = a\\\\\\\nb\n",
			want:  []configEntry{{key: "x.y", value: "a\\b"}},
		},
		{
			// Trailing run of 5: two escaped pairs plus a continuation marker.
			name:  "odd backslash run of five continues the line",
			input: "[x]\n\ty = a\\\\\\\\\\\nb\n",
			want:  []configEntry{{key: "x.y", value: "a\\\\b"}},
		},
		{
			// Even run: no continuation, the next line is an ordinary key.
			name:  "even backslash run does not continue the line",
			input: "[x]\n\ty = a\\\\\n\tb = c\n",
			want: []configEntry{
				{key: "x.y", value: "a\\"},
				{key: "x.b", value: "c"},
			},
		},
		{
			name:  "multiple values for the same key keep file order",
			input: "[remote \"origin\"]\n\turl = first\n\tfetch = f\n\turl = second\n",
			want: []configEntry{
				{key: "remote.origin.url", value: "first"},
				{key: "remote.origin.fetch", value: "f"},
				{key: "remote.origin.url", value: "second"},
			},
		},
		{
			name:  "repeated sections are concatenated in order",
			input: "[remote \"origin\"]\n\turl = a\n[core]\n\tbare = false\n[remote \"origin\"]\n\turl = b\n",
			want: []configEntry{
				{key: "remote.origin.url", value: "a"},
				{key: "core.bare", value: "false"},
				{key: "remote.origin.url", value: "b"},
			},
		},
		{
			name:  "blank lines and stray whitespace",
			input: "\n  [core]  \n\n   bare   =   false   \n\n",
			want:  []configEntry{{key: "core.bare", value: "false"}},
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

		// git's iskeychar() is ASCII alphanumerics plus '-', and the name must
		// start with a letter. Each of these makes git 2.54 abort the whole
		// command with "fatal: bad config line N", verified by appending it to
		// a real .git/config and running `git rev-parse --git-dir`.
		{"underscore in a key name", "[foo]\n\tbad_key = 1\n"},
		{"dot in a key name", "[foo]\n\tbad.key = 1\n"},
		{"space inside a key name", "[foo]\n\tbad key = 1\n"},
		{"key starting with a digit", "[foo]\n\t1abc = 2\n"},
		{"key starting with a dash", "[foo]\n\t-abc = 2\n"},
		{"comment after a valueless key", "[foo]\n\tsomething # hi\n"},
		{"quote after a valueless key", "[foo]\n\tsomething \"x\"\n"},
		{"line starting with an equals sign", "[foo]\n\t= 1\n"},

		// A bare key is legal git syntax — it records a NULL value — but git
		// dies with "missing value for '<key>'" as soon as anything reads it as
		// a string, and `git remote get-url` reads *every* remote.*.url that
		// way. Refusing the file is far less surface than tracking which keys
		// are strings, and git never writes a bare key itself.
		{"valueless key", "[core]\n\tbare\n"},
		{"valueless key in a remote section", "[remote \"x\"]\n\turl\n"},
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
		{key: "remote.origin.url", value: "first"},
		{key: "core.bare", value: "false"},
		{key: "remote.origin.url", value: "second"},
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
		{
			// Confirmed against real git 2.54: `git rev-parse --abbrev-ref
			// HEAD` and `git symbolic-ref HEAD` both fail on this HEAD, so
			// silently returning "main garbage" (the pre-fix behavior)
			// would be a wrong answer, not just an unexpected one.
			name:    "trailing garbage on the ref line is rejected",
			head:    "ref: refs/heads/main garbage\n",
			wantErr: true,
		},
		{
			// Confirmed against real git 2.54: fails the same way. The
			// pre-fix code returned "main\nextra garbage" here because
			// strings.TrimSpace only trims the outer edges of the file,
			// not the embedded newline.
			name:    "a second line after the ref is rejected",
			head:    "ref: refs/heads/main\nextra garbage\n",
			wantErr: true,
		},
		{
			// Confirmed against real git 2.54: an embedded tab in the ref
			// line also makes git refuse to treat HEAD as a valid ref.
			name:    "an embedded tab in the branch token is rejected",
			head:    "ref: refs/heads/main\tfoo\n",
			wantErr: true,
		},
		{
			// Confirmed against real git 2.54: an embedded carriage
			// return (not part of a trailing CRLF line ending) also makes
			// git refuse the ref.
			name:    "an embedded carriage return in the branch token is rejected",
			head:    "ref: refs/heads/main\rgarbage\n",
			wantErr: true,
		},
		{
			// Confirmed against real git 2.54: a trailing CRLF line
			// ending (as opposed to an embedded CR) is fine; git strips
			// it and returns "main". Guards against isValidBranchName
			// rejecting a case strings.TrimSpace already cleans up.
			name: "a trailing CRLF line ending still resolves",
			head: "ref: refs/heads/main\r\n",
			want: "main",
		},
		{
			// Confirmed against real git 2.54: any whitespace (or none)
			// between "ref:" and the path works, not just a single
			// space, so a tab here still resolves to "main".
			name: "a tab between ref: and the path still resolves",
			head: "ref:\trefs/heads/main\n",
			want: "main",
		},

		// Ref grammar. Every rejection below was checked with
		// `git check-ref-format refs/heads/<name>`, which refuses each one, so
		// no repository can ever legitimately have HEAD pointing there.
		{name: "double dot", head: "ref: refs/heads/a..b\n", wantErr: true},
		{name: "trailing dot", head: "ref: refs/heads/foo.\n", wantErr: true},
		{name: "trailing slash", head: "ref: refs/heads/foo/\n", wantErr: true},
		{name: "empty path component", head: "ref: refs/heads/a//b\n", wantErr: true},
		{name: "component starting with a dot", head: "ref: refs/heads/x/.y\n", wantErr: true},
		{name: "lock suffix", head: "ref: refs/heads/end.lock\n", wantErr: true},
		{name: "reflog syntax", head: "ref: refs/heads/a@{b\n", wantErr: true},
		{name: "tilde", head: "ref: refs/heads/til~de\n", wantErr: true},
		{name: "caret", head: "ref: refs/heads/car^et\n", wantErr: true},
		{name: "colon", head: "ref: refs/heads/co:lon\n", wantErr: true},
		{name: "question mark", head: "ref: refs/heads/qu?mark\n", wantErr: true},
		{name: "asterisk", head: "ref: refs/heads/star*\n", wantErr: true},
		{name: "open bracket", head: "ref: refs/heads/brack[et\n", wantErr: true},
		{name: "backslash", head: "ref: refs/heads/back\\slash\n", wantErr: true},
		// ...and the names git *does* accept must still come through.
		{name: "leading dash is a legal branch name", head: "ref: refs/heads/-dash\n", want: "-dash"},
		{name: "a lone at-sign is a legal branch name", head: "ref: refs/heads/@\n", want: "@"},
		{name: "non-ASCII is a legal branch name", head: "ref: refs/heads/café\n", want: "café"},
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

	// core.preferSymlinkRefs. os.ReadFile follows the link and hands back the
	// loose ref's 40 hex characters, which used to read as a detached HEAD and
	// return "HEAD" where git returns the real branch name.
	t.Run("a symlinked HEAD errors instead of reading the ref file", func(t *testing.T) {
		dir := t.TempDir()
		mkdirAll(t, filepath.Join(dir, "refs", "heads"))
		writeFile(t, filepath.Join(dir, "refs", "heads", "main"),
			"9f2c1b7e4a8d3f6019b5c2e7a4d8f1b3c6e9a2d5\n")
		if err := os.Symlink(filepath.Join("refs", "heads", "main"), filepath.Join(dir, "HEAD")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if got, err := branchFromHEAD(dir); err == nil {
			t.Errorf("branchFromHEAD() = %q, want an error so the caller falls back to git", got)
		}
	})
}

func TestBranchIsBorn(t *testing.T) {
	const sha = "9f2c1b7e4a8d3f6019b5c2e7a4d8f1b3c6e9a2d5"

	t.Run("loose ref", func(t *testing.T) {
		dir := t.TempDir()
		mkdirAll(t, filepath.Join(dir, "refs", "heads", "feature"))
		writeFile(t, filepath.Join(dir, "refs", "heads", "feature", "x"), sha+"\n")
		if !branchIsBorn(dir, "feature/x") {
			t.Error("a loose ref must count as born")
		}
		if branchIsBorn(dir, "feature/y") {
			t.Error("an absent ref must not count as born")
		}
	})

	t.Run("packed ref", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "packed-refs"),
			"# pack-refs with: peeled fully-peeled sorted \n"+
				sha+" refs/heads/main\n"+
				sha+" refs/tags/v1\n^"+sha+"\n")
		if !branchIsBorn(dir, "main") {
			t.Error("a packed ref must count as born")
		}
		if branchIsBorn(dir, "v1") {
			t.Error("a tag must not satisfy a branch lookup")
		}
		if branchIsBorn(dir, "other") {
			t.Error("an absent ref must not count as born")
		}
	})

	t.Run("nothing at all", func(t *testing.T) {
		if branchIsBorn(t.TempDir(), "main") {
			t.Error("a fresh git init has no refs, so no branch is born")
		}
	})

	t.Run("zero-byte loose ref", func(t *testing.T) {
		dir := t.TempDir()
		mkdirAll(t, filepath.Join(dir, "refs", "heads"))
		writeFile(t, filepath.Join(dir, "refs", "heads", "empty"), "")
		if branchIsBorn(dir, "empty") {
			t.Error("a zero-byte loose ref is not a real commit; it must not count as born")
		}
	})

	t.Run("directory named like the branch", func(t *testing.T) {
		dir := t.TempDir()
		mkdirAll(t, filepath.Join(dir, "refs", "heads", "feature", "x"))
		if branchIsBorn(dir, "feature") {
			t.Error("a directory shadowing the ref path is not a loose ref; it must not count as born")
		}
	})
}

// --- discoverGitDir ---

// discoverGitDir flattens discoverRepoLayout to the three paths these tests
// assert on. It lives here rather than in gitfile.go so the binary does not
// ship an adapter only the tests call.
func discoverGitDir(start string) (gitDir, commonDir, workTree string, err error) {
	l, err := discoverRepoLayout(start)
	if err != nil {
		return "", "", "", err
	}
	return l.gitDir, l.commonDir, l.workTree, nil
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func mkdirAll(t testing.TB, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertMatchesGit is the real assertion of this file: whatever discoverGitDir
// answers must name the same repository the git binary names. Paths are
// compared symlink-resolved because git reports real paths while the fast path
// deliberately stays in the caller's namespace.
func assertMatchesGit(t *testing.T, start string) {
	t.Helper()
	gitDir, commonDir, workTree, err := discoverGitDir(start)
	if err != nil {
		t.Fatalf("discoverGitDir(%q) error = %v", start, err)
	}

	// --git-common-dir may come back relative to the process cwd, which is
	// `start` for gitOut.
	wantCommon := gitOut(t, start, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(wantCommon) {
		wantCommon = filepath.Join(start, wantCommon)
	}

	for _, c := range []struct{ name, got, want string }{
		{"gitDir", gitDir, gitOut(t, start, "rev-parse", "--absolute-git-dir")},
		{"commonDir", commonDir, wantCommon},
		{"workTree", workTree, gitOut(t, start, "rev-parse", "--show-toplevel")},
	} {
		if realPath(t, c.got) != realPath(t, c.want) {
			t.Errorf("%s = %q, git says %q", c.name, c.got, c.want)
		}
	}
}

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
		assertMatchesGit(t, root)
	})

	t.Run("walks up from a nested subdirectory", func(t *testing.T) {
		root := newTmpGitRepo(t)
		nested := mkdirAll(t, filepath.Join(root, "a", "b", "c"))
		_, _, workTree, err := discoverGitDir(nested)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		if workTree != root {
			t.Errorf("workTree = %q, want %q", workTree, root)
		}
		assertMatchesGit(t, nested)
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
		assertMatchesGit(t, wt)
		assertMatchesGit(t, mkdirAll(t, filepath.Join(wt, "x", "y")))
	})

	t.Run("submodule resolves its relative gitdir pointer", func(t *testing.T) {
		_, sub := newTmpSubmodule(t)
		gitDir, commonDir, workTree, err := discoverGitDir(sub)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		// A submodule gitdir is a full repository, not a worktree of one.
		if gitDir != commonDir {
			t.Errorf("gitDir = %q, commonDir = %q, want them equal", gitDir, commonDir)
		}
		if workTree != sub {
			t.Errorf("workTree = %q, want %q", workTree, sub)
		}
		assertMatchesGit(t, sub)
		assertMatchesGit(t, mkdirAll(t, filepath.Join(sub, "deep", "er")))
	})

	t.Run("worktree of a submodule", func(t *testing.T) {
		_, sub := newTmpSubmodule(t)
		wt := filepath.Join(t.TempDir(), "subwt")
		runGit(t, sub, "worktree", "add", "-b", "sub-wt", wt)
		assertMatchesGit(t, wt)
	})

	t.Run("linked worktree ignores core.bare and core.worktree", func(t *testing.T) {
		// git honours those two keys in the main worktree only, so a linked
		// worktree of a repo that sets them must still resolve normally.
		root := newTmpGitRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		runGit(t, root, "worktree", "add", "-b", "ignored", wt)
		runGit(t, root, "config", "core.worktree", t.TempDir())
		runGit(t, root, "config", "core.bare", "true")
		assertMatchesGit(t, wt)
	})

	t.Run("outside any repo returns an error", func(t *testing.T) {
		if _, _, _, err := discoverGitDir(t.TempDir()); err == nil {
			t.Error("expected an error outside a repository")
		}
	})

	t.Run("start may be a file inside the repo", func(t *testing.T) {
		root := newTmpGitRepo(t)
		file := filepath.Join(root, "main.go")
		writeFile(t, file, "package main\n")
		_, _, workTree, err := discoverGitDir(file)
		if err != nil {
			t.Fatalf("discoverGitDir() error = %v", err)
		}
		if workTree != root {
			t.Errorf("workTree = %q, want %q", workTree, root)
		}
	})
}

// TestDiscoverGitDir_RefusesWhatItCannotReproduce covers the states where a
// plausible-looking answer would differ from git's. Each case asserts what the
// git binary really does first, so the expectations cannot drift.
func TestDiscoverGitDir_RefusesWhatItCannotReproduce(t *testing.T) {
	t.Run("stray .git directory inside a repo", func(t *testing.T) {
		root := newTmpGitRepo(t)
		stray := mkdirAll(t, filepath.Join(root, "vendor", "thing"))
		mkdirAll(t, filepath.Join(stray, ".git"))

		// git ignores the unusable .git and keeps walking up to the real repo.
		if got, want := realPath(t, gitOut(t, stray, "rev-parse", "--show-toplevel")), realPath(t, root); got != want {
			t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
		}
		// Returning the stray directory would silently name another repo, and
		// skipping it would risk skipping a repo git accepts, so: error.
		if _, _, _, err := discoverGitDir(stray); err == nil {
			t.Error("expected an error for an unusable .git directory")
		}
	})

	t.Run("bogus HEAD in an otherwise complete .git directory", func(t *testing.T) {
		root := newTmpGitRepo(t)
		stray := mkdirAll(t, filepath.Join(root, "broken"))
		mkdirAll(t, filepath.Join(stray, ".git", "objects"))
		mkdirAll(t, filepath.Join(stray, ".git", "refs"))
		writeFile(t, filepath.Join(stray, ".git", "HEAD"), "garbage\n")

		if got, want := realPath(t, gitOut(t, stray, "rev-parse", "--show-toplevel")), realPath(t, root); got != want {
			t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
		}
		if _, _, _, err := discoverGitDir(stray); err == nil {
			t.Error("expected an error for a .git directory with an invalid HEAD")
		}
	})

	t.Run("reftable repository", func(t *testing.T) {
		dir := t.TempDir()
		if err := tryGit(dir, "init", "--ref-format=reftable", "."); err != nil {
			t.Skipf("git does not support --ref-format=reftable: %v", err)
		}
		// The trap: .git/HEAD reads "ref: refs/heads/.invalid" while the real
		// branch lives in .git/reftable. Reading HEAD would look plausible and
		// be wrong, so the whole repository has to be refused.
		head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(head), ".invalid") {
			t.Fatalf("precondition: unexpected reftable HEAD %q", head)
		}
		if _, _, _, err := discoverGitDir(dir); err == nil {
			t.Error("expected an error for a reftable-backed repository")
		}
	})

	t.Run("core.worktree pointing somewhere else", func(t *testing.T) {
		root := newTmpGitRepo(t)
		elsewhere := t.TempDir()
		runGit(t, root, "config", "core.worktree", elsewhere)

		// git relocates the work tree; the directory holding .git is no longer
		// the toplevel, so the walk's answer would be wrong.
		if got, want := realPath(t, gitOut(t, root, "rev-parse", "--show-toplevel")), realPath(t, elsewhere); got != want {
			t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
		}
		if _, _, _, err := discoverGitDir(root); err == nil {
			t.Error("expected an error when core.worktree relocates the work tree")
		}
	})

	t.Run("core.bare true", func(t *testing.T) {
		root := newTmpGitRepo(t)
		runGit(t, root, "config", "core.bare", "true")
		if _, _, _, err := discoverGitDir(root); err == nil {
			t.Error("expected an error for a repository configured bare")
		}
	})

	t.Run("unknown repository extension", func(t *testing.T) {
		root := newTmpGitRepo(t)
		runGit(t, root, "config", "core.repositoryformatversion", "1")
		runGit(t, root, "config", "extensions.thingWeDoNotKnow", "true")
		if _, _, _, err := discoverGitDir(root); err == nil {
			t.Error("expected an error for an unknown repository extension")
		}
	})

	// A git directory is not a work tree. Walking past it would silently name
	// the enclosing repository instead, which is the one unacceptable outcome.
	t.Run("start is itself a git directory", func(t *testing.T) {
		root := newTmpGitRepo(t)
		runGit(t, root, "worktree", "add", "-q", "-b", "wtb", filepath.Join(t.TempDir(), "wt"))
		super, _ := newTmpSubmodule(t)

		// A bare repo nested inside a work tree: the walk used to skip it and
		// report the enclosing repository.
		bare := filepath.Join(root, "nested.git")
		runGit(t, root, "init", "--bare", bare)

		for _, tc := range []struct{ name, start string }{
			{"the gitdir itself", filepath.Join(root, ".git")},
			{"a subdirectory of the gitdir", mkdirAll(t, filepath.Join(root, ".git", "hooks"))},
			{"refs inside the gitdir", filepath.Join(root, ".git", "refs")},
			{"a linked worktree gitdir", filepath.Join(root, ".git", "worktrees", "wt")},
			{"a submodule gitdir", filepath.Join(super, ".git", "modules", "sub")},
			{"a bare repo nested in a work tree", bare},
		} {
			t.Run(tc.name, func(t *testing.T) {
				// git either refuses outright or names a repository that is
				// never the one the naive walk would reach; either way the
				// fast path must not answer.
				if _, _, _, err := discoverGitDir(tc.start); err == nil {
					t.Errorf("discoverGitDir(%q) succeeded, but git refuses it or names another repository", tc.start)
				}
			})
		}
	})

	t.Run("worktree config extension", func(t *testing.T) {
		// extensions.worktreeConfig only matters for what config.worktree
		// actually contains; rejecting on the key alone would permanently
		// disable the fast path for every sparse-checkout and Scalar user.
		enable := func(t *testing.T, root string) {
			t.Helper()
			runGit(t, root, "config", "core.repositoryformatversion", "1")
			runGit(t, root, "config", "extensions.worktreeConfig", "true")
		}

		t.Run("enabled with no config.worktree is fine", func(t *testing.T) {
			root := newTmpGitRepo(t)
			enable(t, root)
			assertMatchesGit(t, root)
		})

		t.Run("disabled is inert", func(t *testing.T) {
			root := newTmpGitRepo(t)
			runGit(t, root, "config", "core.repositoryformatversion", "1")
			runGit(t, root, "config", "extensions.worktreeConfig", "false")
			assertMatchesGit(t, root)
		})

		t.Run("sparse-checkout keeps the fast path", func(t *testing.T) {
			root := newTmpGitRepo(t)
			if err := tryGit(root, "sparse-checkout", "set", "--cone"); err != nil {
				t.Skipf("git sparse-checkout unavailable: %v", err)
			}
			if got := gitOut(t, root, "config", "--local", "extensions.worktreeConfig"); got != "true" {
				t.Fatalf("precondition: extensions.worktreeConfig = %q, want true", got)
			}
			assertMatchesGit(t, root)
		})

		t.Run("unparsable value is refused", func(t *testing.T) {
			root := newTmpGitRepo(t)
			runGit(t, root, "config", "core.repositoryformatversion", "1")
			runGit(t, root, "config", "extensions.worktreeConfig", "perhaps")
			if _, _, _, err := discoverGitDir(root); err == nil {
				t.Error("expected an error for an uninterpretable extensions.worktreeConfig")
			}
		})

		t.Run("main worktree config.worktree relocates the work tree", func(t *testing.T) {
			root := newTmpGitRepo(t)
			enable(t, root)
			elsewhere := t.TempDir()
			writeFile(t, filepath.Join(root, ".git", "config.worktree"),
				"[core]\n\tworktree = "+elsewhere+"\n")

			if got, want := realPath(t, gitOut(t, root, "rev-parse", "--show-toplevel")), realPath(t, elsewhere); got != want {
				t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
			}
			if _, _, _, err := discoverGitDir(root); err == nil {
				t.Error("expected an error when config.worktree relocates the work tree")
			}
		})

		t.Run("main worktree config.worktree can declare the repo bare", func(t *testing.T) {
			root := newTmpGitRepo(t)
			enable(t, root)
			writeFile(t, filepath.Join(root, ".git", "config.worktree"), "[core]\n\tbare = true\n")
			if err := tryGit(root, "rev-parse", "--show-toplevel"); err == nil {
				t.Fatal("precondition: git should refuse a bare repository")
			}
			if _, _, _, err := discoverGitDir(root); err == nil {
				t.Error("expected an error when config.worktree declares the repo bare")
			}
		})

		t.Run("linked worktree honours its own config.worktree", func(t *testing.T) {
			root := newTmpGitRepo(t)
			enable(t, root)
			wt := filepath.Join(t.TempDir(), "wt")
			runGit(t, root, "worktree", "add", "-q", "-b", "wtcfg", wt)
			assertMatchesGit(t, wt) // nothing set yet: the fast path still works

			elsewhere := t.TempDir()
			// The worktree's gitdir is named after the checkout directory, not
			// the branch, so ask git for it rather than guessing.
			writeFile(t, filepath.Join(gitOut(t, wt, "rev-parse", "--absolute-git-dir"), "config.worktree"),
				"[core]\n\tworktree = "+elsewhere+"\n")
			if got, want := realPath(t, gitOut(t, wt, "rev-parse", "--show-toplevel")), realPath(t, elsewhere); got != want {
				t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
			}
			if _, _, _, err := discoverGitDir(wt); err == nil {
				t.Error("expected an error when the worktree's config.worktree relocates the work tree")
			}
		})
	})

	// This pins the pairing that makes the gitDir != commonDir shortcut in
	// checkRepoConfig safe. The shortcut rests on git ignoring the shared
	// core.bare / core.worktree inside a linked worktree — which is only true
	// while extensions.worktreeConfig is off. Verified against git 2.54.
	t.Run("shared core.worktree in a linked worktree depends on worktreeConfig", func(t *testing.T) {
		build := func(t *testing.T, extensionOn bool) (wt, elsewhere string) {
			t.Helper()
			root := newTmpGitRepo(t)
			if extensionOn {
				runGit(t, root, "config", "core.repositoryformatversion", "1")
				runGit(t, root, "config", "extensions.worktreeConfig", "true")
			}
			wt = filepath.Join(t.TempDir(), "wt")
			runGit(t, root, "worktree", "add", "-q", "-b", "shared", wt)
			elsewhere = t.TempDir()
			runGit(t, root, "config", "core.worktree", elsewhere)
			return wt, elsewhere
		}

		t.Run("extension off: git ignores it, so the fast path may answer", func(t *testing.T) {
			wt, _ := build(t, false)
			if got, want := realPath(t, gitOut(t, wt, "rev-parse", "--show-toplevel")), realPath(t, wt); got != want {
				t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
			}
			assertMatchesGit(t, wt)
		})

		t.Run("extension on: git honours it, so the fast path must refuse", func(t *testing.T) {
			wt, elsewhere := build(t, true)
			if got, want := realPath(t, gitOut(t, wt, "rev-parse", "--show-toplevel")), realPath(t, elsewhere); got != want {
				t.Fatalf("precondition: git toplevel = %q, want %q", got, want)
			}
			if _, _, _, err := discoverGitDir(wt); err == nil {
				t.Error("expected an error: the shared core.worktree relocates this worktree")
			}
		})
	})

	t.Run("environment overrides", func(t *testing.T) {
		for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY", "GIT_CEILING_DIRECTORIES"} {
			t.Run(name, func(t *testing.T) {
				root := newTmpGitRepo(t)
				t.Setenv(name, root)
				if _, _, _, err := discoverGitDir(root); err == nil {
					t.Errorf("expected an error when %s is set", name)
				}
			})
		}
	})
}

// TestDiscoverGitDir_GitfilePointer pins the ".git file" format against what
// git's own read_gitfile_gently() accepts: exactly "gitdir: " then the path,
// with only trailing newlines stripped.
func TestDiscoverGitDir_GitfilePointer(t *testing.T) {
	// A real, valid git directory for the pointers to aim at.
	target := filepath.Join(newTmpGitRepo(t), ".git")

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "relative pointer", content: "gitdir: ../real/.git\n"},
		{name: "absolute pointer", content: "gitdir: " + target + "\n"},
		{name: "no trailing newline", content: "gitdir: ../real/.git"},
		{name: "carriage return", content: "gitdir: ../real/.git\r\n"},
		{name: "missing gitdir prefix", content: "hello world\n", wantErr: true},
		{name: "no space after the colon", content: "gitdir:../real/.git\n", wantErr: true},
		{name: "padded path", content: "gitdir:   ../real/.git\n", wantErr: true},
		{name: "trailing space", content: "gitdir: ../real/.git \n", wantErr: true},
		{name: "extra line", content: "gitdir: ../real/.git\nextra\n", wantErr: true},
		{name: "leading blank line", content: "\ngitdir: ../real/.git\n", wantErr: true},
		{name: "empty file", content: "", wantErr: true},
		{name: "only the prefix", content: "gitdir: \n", wantErr: true},
		{name: "points at a non-repository", content: "gitdir: ../empty\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case gets its own tree: <tmp>/real is a repository the
			// relative pointers can reach, <tmp>/empty is not.
			base := t.TempDir()
			runGit(t, mkdirAll(t, filepath.Join(base, "real")), "init", ".")
			mkdirAll(t, filepath.Join(base, "empty"))
			work := mkdirAll(t, filepath.Join(base, "work"))
			writeFile(t, filepath.Join(work, ".git"), tt.content)

			// Assert git's own verdict first so the table cannot drift.
			gitErr := tryGit(work, "rev-parse", "--absolute-git-dir")
			if (gitErr != nil) != tt.wantErr {
				t.Fatalf("precondition: git error = %v, wantErr = %v", gitErr, tt.wantErr)
			}

			_, _, workTree, err := discoverGitDir(work)
			if tt.wantErr {
				if err == nil {
					t.Errorf("discoverGitDir() = %q, want an error (git refuses this file)", workTree)
				}
				return
			}
			if err != nil {
				t.Fatalf("discoverGitDir() error = %v", err)
			}
			if workTree != work {
				t.Errorf("workTree = %q, want %q", workTree, work)
			}
		})
	}
}

func TestLastConfigValue(t *testing.T) {
	entries := []configEntry{
		{key: "core.bare", value: "false"},
		{key: "remote.origin.url", value: "a"},
		{key: "core.bare", value: "true"},
	}
	tests := []struct {
		name      string
		key       string
		want      string
		wantFound bool
	}{
		{name: "repeated key returns the last value", key: "core.bare", want: "true", wantFound: true},
		{name: "single occurrence", key: "remote.origin.url", want: "a", wantFound: true},
		{name: "missing key", key: "core.worktree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := lastConfigValue(entries, tt.key)
			if got != tt.want || found != tt.wantFound {
				t.Errorf("lastConfigValue(%q) = (%q, %v), want (%q, %v)", tt.key, got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestConfigBool(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     bool
		wantKnow bool
	}{
		{name: "true", input: "true", want: true, wantKnow: true},
		{name: "uppercase yes", input: "YES", want: true, wantKnow: true},
		{name: "on", input: "on", want: true, wantKnow: true},
		{name: "one", input: "1", want: true, wantKnow: true},
		{name: "false", input: "false", wantKnow: true},
		{name: "off", input: "off", wantKnow: true},
		{name: "zero", input: "0", wantKnow: true},
		{name: "empty is false", input: "", wantKnow: true},
		{name: "anything else is not understood", input: "maybe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, know := configBool(tt.input)
			if got != tt.want || know != tt.wantKnow {
				t.Errorf("configBool(%q) = (%v, %v), want (%v, %v)", tt.input, got, know, tt.want, tt.wantKnow)
			}
		})
	}
}

// --- needsGitFallback ---

// envTB is testing.TB plus Setenv, which the TB interface itself does not
// carry, so that both tests and benchmarks can pin their environment.
type envTB interface {
	testing.TB
	Setenv(key, value string)
}

// pinConfigScope isolates a test from the machine's real git configuration.
// GIT_CONFIG_NOSYSTEM is deliberately left unset so that the system scope is
// still exercised, pinned to an empty file through GIT_CONFIG_SYSTEM.
func pinConfigScope(t envTB) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	writeFile(t, empty, "")
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // os.UserHomeDir on Windows
	unsetEnv(t, "GIT_CONFIG_NOSYSTEM")
	unsetEnv(t, "GIT_CONFIG_COUNT")
	for _, name := range gitDiscoveryEnvVars {
		unsetEnv(t, name)
	}
}

// unsetEnv removes a variable for the duration of the test. t.Setenv cannot do
// this on its own, and setting it to the empty string is not equivalent: git
// itself rejects an empty GIT_DIR with "The empty string is not a valid path".
func unsetEnv(t envTB, name string) {
	t.Helper()
	t.Setenv(name, "") // registers the restore of the original value
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// writeConfig creates a config file in a fresh dir and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	writeFile(t, p, content)
	return p
}

// localScope builds a directory holding an innocuous local config and returns
// it, standing in for both the gitDir and the commonDir of a plain repository.
func localScope(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config"), content)
	return dir
}

const cleanConfig = "[remote \"origin\"]\n\turl = https://example.com/r.git\n"

func TestNeedsGitFallback(t *testing.T) {
	t.Run("clean scope does not need the fallback", func(t *testing.T) {
		pinConfigScope(t)
		dir := localScope(t, cleanConfig)
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false for a clean scope")
		}
	})

	t.Run("a missing config file is not a reason to fall back", func(t *testing.T) {
		pinConfigScope(t)
		dir := t.TempDir()
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false when files are simply absent")
		}
	})

	// Every discovery variable already disqualifies the walk; needsGitFallback
	// asks the same helper so the two cannot drift apart.
	t.Run("discovery environment variables force the fallback", func(t *testing.T) {
		for _, name := range gitDiscoveryEnvVars {
			t.Run(name, func(t *testing.T) {
				pinConfigScope(t)
				dir := localScope(t, cleanConfig)
				t.Setenv(name, "/somewhere")
				if !needsGitFallback(dir, dir, "origin") {
					t.Errorf("needsGitFallback() = false, want true when %s is set", name)
				}
			})
		}
	})

	t.Run("GIT_CONFIG_COUNT forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		dir := localScope(t, cleanConfig)
		t.Setenv("GIT_CONFIG_COUNT", "1")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true when config comes from the environment")
		}
	})

	markers := []struct{ name, content string }{
		{"insteadOf", "[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"},
		{"uppercase INSTEADOF", "[url \"x\"]\n\tINSTEADOF = y\n"},
		{"pushInsteadOf", "[url \"x\"]\n\tpushInsteadOf = y\n"},
	}

	for _, m := range markers {
		t.Run(m.name+" in the global config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t, m.content))
			dir := localScope(t, cleanConfig)
			if !needsGitFallback(dir, dir, "origin") {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})

		t.Run(m.name+" in the system config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			t.Setenv("GIT_CONFIG_SYSTEM", writeConfig(t, m.content))
			dir := localScope(t, cleanConfig)
			if !needsGitFallback(dir, dir, "origin") {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})

		t.Run(m.name+" in the local config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			dir := localScope(t, m.content)
			if !needsGitFallback(dir, dir, "origin") {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})
	}

	// An outer scope wins for `git remote get-url`, which returns the *first*
	// url across all scopes; the fast path only ever reads the repository's own
	// config. Verified against git 2.54: a global remote.origin.url really does
	// shadow the local one.
	t.Run("remote.<name>.url outside the repository config forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t, "[remote \"origin\"]\n\turl = https://global/g.git\n"))
		dir := localScope(t, cleanConfig)
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true for a global remote.origin.url")
		}
	})

	t.Run("a remote the caller did not ask for is not a reason to fall back", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t, "[remote \"other\"]\n\turl = https://global/g.git\n"))
		dir := localScope(t, cleanConfig)
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false for an unrelated remote")
		}
	})

	// The per-worktree file is read whenever extensions.worktreeConfig is on,
	// and it can carry an insteadOf like any other config file.
	t.Run("insteadOf in config.worktree forces the fallback", func(t *testing.T) {
		for _, where := range []string{"gitDir", "commonDir"} {
			t.Run(where, func(t *testing.T) {
				pinConfigScope(t)
				gitDir := localScope(t, cleanConfig)
				commonDir := localScope(t, cleanConfig)
				target := gitDir
				if where == "commonDir" {
					target = commonDir
				}
				writeFile(t, filepath.Join(target, "config.worktree"), "[url \"a\"]\n\tinsteadOf = b\n")
				if !needsGitFallback(gitDir, commonDir, "origin") {
					t.Error("needsGitFallback() = false, want true for an insteadOf in config.worktree")
				}
			})
		}
	})

	t.Run("GIT_CONFIG_NOSYSTEM only suppresses the system scope when true", func(t *testing.T) {
		for _, tc := range []struct {
			value string
			want  bool
		}{
			{"1", false}, {"true", false}, {"yes", false},
			{"0", true}, {"false", true}, {"", true},
			{"garbage", true}, // uninterpretable: read the file rather than skip it
		} {
			t.Run("value="+tc.value, func(t *testing.T) {
				pinConfigScope(t)
				t.Setenv("GIT_CONFIG_SYSTEM", writeConfig(t, "[url \"a\"]\n\tinsteadOf = b\n"))
				t.Setenv("GIT_CONFIG_NOSYSTEM", tc.value)
				dir := localScope(t, cleanConfig)
				if got := needsGitFallback(dir, dir, "origin"); got != tc.want {
					t.Errorf("needsGitFallback() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	// git reads ~/.config/git/config when XDG_CONFIG_HOME is unset. Missing
	// that fallback would let an insteadOf there produce a wrong URL silently.
	t.Run("the XDG default location is scanned", func(t *testing.T) {
		pinConfigScope(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("GIT_CONFIG_GLOBAL", "")
		mkdirAll(t, filepath.Join(home, ".config", "git"))
		writeFile(t, filepath.Join(home, ".config", "git", "config"), "[url \"a\"]\n\tinsteadOf = b\n")

		dir := localScope(t, cleanConfig)
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true for an insteadOf in ~/.config/git/config")
		}
	})

	t.Run("~/.gitconfig is scanned", func(t *testing.T) {
		pinConfigScope(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("GIT_CONFIG_GLOBAL", "")
		writeFile(t, filepath.Join(home, ".gitconfig"), "[url \"a\"]\n\tinsteadOf = b\n")

		dir := localScope(t, cleanConfig)
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true for an insteadOf in ~/.gitconfig")
		}
	})
}

// TestNeedsGitFallback_Includes covers the point of resolving includes rather
// than refusing on sight: an include only costs the fast path when the file it
// pulls in really does define something the answer depends on. The differential
// test is what proves each verdict matches git; these cases pin the reasoning.
func TestNeedsGitFallback_Includes(t *testing.T) {
	// setup writes the global config and returns the repository scope dir.
	setup := func(t *testing.T, home, global string) (dir string) {
		t.Helper()
		pinConfigScope(t)
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
		writeFile(t, filepath.Join(home, ".gitconfig"), global)
		return localScope(t, cleanConfig)
	}

	t.Run("an include of a harmless file keeps the fast path", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "extra"), "[user]\n\temail = a@b\n")
		dir := setup(t, home, "[include]\n\tpath = extra\n")
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false: the included file changes nothing")
		}
	})

	t.Run("an include that defines insteadOf forces the fallback", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "extra"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := setup(t, home, "[include]\n\tpath = extra\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: the included file rewrites URLs")
		}
	})

	t.Run("an include that defines the remote URL forces the fallback", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "extra"), "[remote \"origin\"]\n\turl = https://x/y.git\n")
		dir := setup(t, home, "[include]\n\tpath = extra\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: the included file defines the remote")
		}
	})

	t.Run("a nested include is followed", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "mid"), "[include]\n\tpath = deep\n")
		writeFile(t, filepath.Join(home, "deep"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := setup(t, home, "[include]\n\tpath = mid\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: the insteadOf is two includes down")
		}
	})

	t.Run("a ~ path is expanded", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "tilde"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := setup(t, home, "[include]\n\tpath = ~/tilde\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: ~/tilde holds an insteadOf")
		}
	})

	t.Run("an include cycle forces the fallback", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "loop"), "[include]\n\tpath = "+filepath.Join(home, ".gitconfig")+"\n")
		dir := setup(t, home, "[include]\n\tpath = loop\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: git aborts on a cycle")
		}
	})

	t.Run("a missing include target changes nothing", func(t *testing.T) {
		home := t.TempDir()
		dir := setup(t, home, "[include]\n\tpath = /nowhere/at/all\n")
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false: git ignores a missing include")
		}
	})

	t.Run("a subsectioned include is ignored, as git ignores it", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "extra"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := setup(t, home, "[include \"x\"]\n\tpath = extra\n")
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false: [include \"x\"] is not an include")
		}
	})

	t.Run("an unknown includeIf condition is always false in git", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "extra"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := setup(t, home, "[includeIf \"nosuchcond:x\"]\n\tpath = extra\n")
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false: git never applies an unknown condition")
		}
	})

	// This is the case the whole change exists for: the developer's real config
	// carries an includeIf whose target holds an insteadOf, but whose gitdir
	// condition does not match this repository.
	t.Run("a gitdir condition that cannot match is skipped", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "work"), "[url \"a\"]\n\tinsteadOf = b\n")
		elsewhere := mkdirAll(t, filepath.Join(home, "elsewhere"))
		dir := setup(t, home, "[includeIf \"gitdir:"+elsewhere+"/\"]\n\tpath = work\n")
		if needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = true, want false: the condition cannot match this gitdir")
		}
	})

	t.Run("a gitdir condition that matches is honoured", func(t *testing.T) {
		pinConfigScope(t)
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "work"), "[url \"a\"]\n\tinsteadOf = b\n")
		dir := localScope(t, cleanConfig)
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
		// The pattern names the directory *containing* the gitdir, with the
		// trailing separator that makes git append its implicit "**".
		writeFile(t, filepath.Join(home, ".gitconfig"),
			"[includeIf \"gitdir:"+filepath.Dir(realPath(t, dir))+"/\"]\n\tpath = work\n")
		if !needsGitFallback(dir, dir, "origin") {
			t.Error("needsGitFallback() = false, want true: the condition matches this gitdir")
		}
	})

	// Only gitdir: is evaluated. The other forms are judged by what they would
	// include, so a harmless target still keeps the fast path.
	t.Run("an unevaluated condition is judged by the file it would include", func(t *testing.T) {
		for _, cond := range []string{"gitdir/i:/x/", "onbranch:main", "hasconfig:remote.*.url:https://x/**"} {
			t.Run(cond, func(t *testing.T) {
				home := t.TempDir()
				writeFile(t, filepath.Join(home, "harmless"), "[user]\n\temail = a@b\n")
				dir := setup(t, home, "[includeIf \""+cond+"\"]\n\tpath = harmless\n")
				if needsGitFallback(dir, dir, "origin") {
					t.Errorf("needsGitFallback() = true, want false for %q with a harmless target", cond)
				}

				writeFile(t, filepath.Join(home, "harmless"), "[url \"a\"]\n\tinsteadOf = b\n")
				if !needsGitFallback(dir, dir, "origin") {
					t.Errorf("needsGitFallback() = false, want true for %q with an insteadOf target", cond)
				}
			})
		}
	})
}

func TestIncludeDirective(t *testing.T) {
	tests := []struct {
		key      string
		wantCond string
		wantOK   bool
	}{
		{"include.path", "", true},
		{"includeif.gitdir:~/work/.path", "gitdir:~/work/", true},
		{"includeif.onbranch:main.path", "onbranch:main", true},
		{"include.x.path", "", false},  // subsectioned include: git ignores it
		{"includeif..path", "", false}, // empty condition
		{"includeif.gitdir:x.other", "", false},
		{"include.pathological", "", false},
		{"remote.origin.url", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			cond, ok := includeDirective(tt.key)
			if ok != tt.wantOK || cond != tt.wantCond {
				t.Errorf("includeDirective(%q) = (%q, %v), want (%q, %v)",
					tt.key, cond, ok, tt.wantCond, tt.wantOK)
			}
		})
	}
}

// TestSystemConfigPaths_CoversTheRealOne guards the one guess in
// outerConfigScopePaths that cannot be derived from the environment: git's
// compiled-in ETC_GITCONFIG. Homebrew puts it under the install prefix rather
// than in /etc, so a hardcoded /etc/gitconfig would silently miss it.
func TestSystemConfigPaths_CoversTheRealOne(t *testing.T) {
	out, err := exec.Command("git", "config", "--list", "--show-origin", "--system").Output()
	if err != nil {
		t.Skip("no system git config on this machine")
	}
	line, _, _ := strings.Cut(string(out), "\n")
	origin, _, ok := strings.Cut(line, "\t")
	if !ok {
		t.Skipf("unexpected --show-origin output: %q", line)
	}
	want := strings.TrimPrefix(origin, "file:")

	// Compare symlink-resolved where possible; a candidate that does not exist
	// simply cannot be the match.
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	for _, p := range systemConfigPaths() {
		if p == want || resolve(p) == resolve(want) {
			return
		}
	}
	t.Errorf("systemConfigPaths() = %v, none of which is git's real system config %q", systemConfigPaths(), want)
}

// --- readRepoContextFromDisk ---

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
		nested := mkdirAll(t, filepath.Join(root, "pkg", "util"))
		file := filepath.Join(nested, "helper.go")
		writeFile(t, file, "")

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

	t.Run("a nonexistent path errors", func(t *testing.T) {
		if _, err := readRepoContextFromDisk(filepath.Join(root, "no-such-file"), "origin"); err == nil {
			t.Error("expected an error for a path that does not exist")
		}
	})

	// The fast path must not answer when something in scope could rewrite the
	// URL, even though the repository itself is perfectly ordinary.
	t.Run("an insteadOf in scope sends the caller to git", func(t *testing.T) {
		pinConfigScope(t)
		t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t,
			"[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"))
		if _, err := readRepoContextFromDisk(root, "origin"); err == nil {
			t.Error("expected an error when insteadOf is configured")
		}
	})
}

// TestGetRepoContext_FallsBackToGit checks the dispatcher actually falls back
// rather than propagating the fast path's refusal.
func TestGetRepoContext_FallsBackToGit(t *testing.T) {
	pinConfigScope(t)
	root := newTmpGitRepo(t)
	runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")

	// insteadOf is the reason needsGitFallback exists: verified against git
	// 2.54, `git remote get-url` really does apply the rewrite, so reading the
	// raw config would report a URL git never uses.
	t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t,
		"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n"))

	const rewritten = "https://gitlab.com/mirror/repo"
	if got := gitOut(t, root, "remote", "get-url", "origin"); got != rewritten+".git" {
		t.Fatalf("precondition: git remote get-url = %q, want the rewritten URL", got)
	}
	if _, err := readRepoContextFromDisk(root, "origin"); err == nil {
		t.Fatal("precondition: the fast path should refuse a scope containing insteadOf")
	}

	got, err := getRepoContext(root, "origin")
	if err != nil {
		t.Fatalf("getRepoContext() error = %v", err)
	}
	if got.baseURL != rewritten {
		t.Errorf("baseURL = %q, want %q — the dispatcher must return git's answer, not the raw config",
			got.baseURL, rewritten)
	}
}

// --- resolveTarget ---

// TestResolveTarget asserts resolveTarget's output directly against an oracle
// independent of gopen's own code: filepath.EvalSymlinks computed in the test
// on the containing directory only, joined with the unresolved
// filepath.Base of the original path. It must not compare the fast path
// against the git fallback — both call resolveTarget, so a symmetric bug in
// it would pass either way. See the case "symlinked file whose destination is
// elsewhere" and "symlink into a different directory tree": a regression that
// resolves the whole targetPath (instead of only its containing directory)
// would follow the link all the way to its destination and rename the
// basename to match, which these cases catch.
func TestResolveTarget(t *testing.T) {
	symlinkOK := runtime.GOOS != "windows"
	skipNoSymlink := func(t *testing.T) {
		t.Helper()
		if !symlinkOK {
			t.Skip("creating symlinks requires elevated privileges on Windows")
		}
	}

	t.Run("plain file in plain directory", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "file.txt"), "hello")
		targetPath := filepath.Join(root, "file.txt")

		gotDir, gotTarget, err := resolveTarget(targetPath)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", targetPath, err)
		}
		wantDir := realPath(t, root)
		wantTarget := filepath.Join(wantDir, "file.txt")
		if gotDir != wantDir {
			t.Errorf("dir = %q, want %q", gotDir, wantDir)
		}
		if gotTarget != wantTarget {
			t.Errorf("target = %q, want %q", gotTarget, wantTarget)
		}
	})

	t.Run("plain directory as target", func(t *testing.T) {
		root := t.TempDir()
		sub := mkdirAll(t, filepath.Join(root, "sub"))

		gotDir, gotTarget, err := resolveTarget(sub)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", sub, err)
		}
		want := realPath(t, sub)
		if gotDir != want || gotTarget != want {
			t.Errorf("dir, target = %q, %q, want %q, %q — a directory target must resolve to itself in both return values",
				gotDir, gotTarget, want, want)
		}
	})

	t.Run("file through a symlinked parent directory", func(t *testing.T) {
		skipNoSymlink(t)
		root := t.TempDir()
		real := mkdirAll(t, filepath.Join(root, "realdir"))
		writeFile(t, filepath.Join(real, "file.txt"), "hello")
		link := filepath.Join(root, "linkdir")
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		targetPath := filepath.Join(link, "file.txt")

		gotDir, gotTarget, err := resolveTarget(targetPath)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", targetPath, err)
		}
		wantDir := realPath(t, real)
		wantTarget := filepath.Join(wantDir, "file.txt")
		if gotDir != wantDir {
			t.Errorf("dir = %q, want %q — a symlinked parent directory (the macOS /var shape) must resolve", gotDir, wantDir)
		}
		if gotTarget != wantTarget {
			t.Errorf("target = %q, want %q", gotTarget, wantTarget)
		}
	})

	t.Run("symlinked file whose destination is elsewhere", func(t *testing.T) {
		skipNoSymlink(t)
		root := t.TempDir()
		destDir := mkdirAll(t, filepath.Join(root, "dest"))
		dest := filepath.Join(destDir, "actual.txt")
		writeFile(t, dest, "hello")
		link := filepath.Join(root, "mylink.txt")
		if err := os.Symlink(dest, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}

		gotDir, gotTarget, err := resolveTarget(link)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", link, err)
		}
		wantDir := realPath(t, root)
		wantTarget := filepath.Join(wantDir, "mylink.txt")
		if gotDir != wantDir {
			t.Errorf("dir = %q, want %q", gotDir, wantDir)
		}
		if gotTarget != wantTarget {
			t.Errorf("target = %q, want %q — the basename must stay the link's own name (mylink.txt), not the destination's (actual.txt)",
				gotTarget, wantTarget)
		}
	})

	t.Run("symlink into a different directory tree", func(t *testing.T) {
		skipNoSymlink(t)
		root1 := t.TempDir()
		root2 := t.TempDir()
		destDir := mkdirAll(t, filepath.Join(root2, "otherrepo"))
		dest := filepath.Join(destDir, "target.txt")
		writeFile(t, dest, "hello")
		link := filepath.Join(root1, "pointer.txt")
		if err := os.Symlink(dest, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}

		gotDir, gotTarget, err := resolveTarget(link)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", link, err)
		}
		wantDir := realPath(t, root1)
		wantTarget := filepath.Join(wantDir, "pointer.txt")
		if gotDir != wantDir {
			t.Errorf("dir = %q, want %q — must stay in the link's own tree (root1), not jump into the target's (root2)",
				gotDir, wantDir)
		}
		if gotTarget != wantTarget {
			t.Errorf("target = %q, want %q", gotTarget, wantTarget)
		}
	})

	t.Run("symlinked directory as the target itself", func(t *testing.T) {
		skipNoSymlink(t)
		root := t.TempDir()
		real := mkdirAll(t, filepath.Join(root, "realdir2"))
		link := filepath.Join(root, "linkdir2")
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}

		gotDir, gotTarget, err := resolveTarget(link)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", link, err)
		}
		want := realPath(t, real)
		if gotDir != want {
			t.Errorf("dir = %q, want %q", gotDir, want)
		}
		if gotTarget != want {
			t.Errorf("target = %q, want %q — a symlinked directory target must resolve to its destination", gotTarget, want)
		}
	})

	t.Run("directory target with a trailing separator", func(t *testing.T) {
		root := t.TempDir()
		sub := mkdirAll(t, filepath.Join(root, "sub"))
		targetPath := sub + string(filepath.Separator)

		gotDir, gotTarget, err := resolveTarget(targetPath)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", targetPath, err)
		}
		want := realPath(t, sub)
		if gotDir != want || gotTarget != want {
			t.Errorf("dir, target = %q, %q, want %q, %q", gotDir, gotTarget, want, want)
		}
	})
}

// --- the differential test ---

// rewriteToMirror is the config body the includeIf fixtures pull in: if git
// reads it, the remote resolves to https://gitlab.com/mirror/repo instead of
// https://github.com/example/repo, so a wrong verdict on the condition shows up
// as a URL divergence rather than a silent pass.
const rewriteToMirror = "[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n"

// TestDifferential_FastPathMatchesGit is the core guarantee of this design:
// for every repository shape gopen supports, reading .git directly must yield
// exactly what shelling out to git yields. A divergence here is a bug that
// would send the user to the wrong URL, so a failure means fixing gitfile.go,
// never the assertion.
func TestDifferential_FastPathMatchesGit(t *testing.T) {
	fixtures := []struct {
		name   string
		remote string
		// fallsBack marks the shapes the fast path is expected to stand down
		// on. Leaving it false is the stronger assertion: the fast path must
		// answer, and answer exactly what git answers. Setting it says the
		// refusal is the point of the fixture.
		fallsBack bool
		// wantGitURL, when set, pins what the git binary itself resolves the
		// remote to, so a fixture built around a URL rewrite cannot quietly
		// become one where nothing is rewritten.
		wantGitURL string
		build      func(t *testing.T) (targetPath string)
	}{
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
				file := filepath.Join(mkdirAll(t, filepath.Join(root, "pkg", "util")), "helper.go")
				writeFile(t, file, "")
				return file
			},
		},
		{
			name:   "nested directory",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				return mkdirAll(t, filepath.Join(root, "a", "b", "c"))
			},
		},
		{
			name:   "branch name containing slashes",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "checkout", "-q", "-b", "feature/foo/bar")
				return root
			},
		},
		{
			name:   "branch name with dots and unicode",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "checkout", "-q", "-b", "release-1.2.x-café")
				return root
			},
		},
		{
			name:   "detached HEAD",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "checkout", "-q", "--detach", "HEAD")
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
			name:   "remote name with mixed case, which git keeps verbatim",
			remote: "MyRemote",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "MyRemote", "https://github.com/example/repo.git")
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
				runGit(t, root, "worktree", "add", "-q", "-b", "wt-branch", wt)
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
				runGit(t, root, "worktree", "add", "-q", "-b", "wt2", wt)
				file := filepath.Join(wt, "inside.go")
				writeFile(t, file, "")
				return file
			},
		},
		{
			name:   "submodule",
			remote: "origin",
			build: func(t *testing.T) string {
				_, sub := newTmpSubmoduleWithRemote(t)
				return sub
			},
		},
		{
			name:   "file inside a submodule",
			remote: "origin",
			build: func(t *testing.T) string {
				_, sub := newTmpSubmoduleWithRemote(t)
				file := filepath.Join(mkdirAll(t, filepath.Join(sub, "deep")), "f.go")
				writeFile(t, file, "")
				return file
			},
		},
		{
			name:   "worktree of a submodule",
			remote: "origin",
			build: func(t *testing.T) string {
				_, sub := newTmpSubmoduleWithRemote(t)
				wt := filepath.Join(t.TempDir(), "subwt")
				runGit(t, sub, "worktree", "add", "-q", "-b", "sub-wt", wt)
				return wt
			},
		},
		{
			// extensions.worktreeConfig is on here, which used to disqualify
			// the fast path outright.
			name:   "sparse-checkout repository",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				if err := tryGit(root, "sparse-checkout", "set", "--cone"); err != nil {
					t.Skipf("git sparse-checkout unavailable: %v", err)
				}
				return root
			},
		},
		{
			// The two paths live in different namespaces here: the fast path
			// keeps the caller's symlinked path, git reports the real one.
			// relPath must still come out identical.
			name:   "target reached through a symlinked parent",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				file := filepath.Join(mkdirAll(t, filepath.Join(root, "pkg")), "x.go")
				writeFile(t, file, "")

				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(root, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return filepath.Join(link, "pkg", "x.go")
			},
		},
		{
			// The target *itself* is the symlink. git resolves the directory it
			// runs in, never the pathspec, so both paths must report the link's
			// own location — b/link.txt, not a/f.txt.
			name:   "target is a symlinked file",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				writeFile(t, filepath.Join(mkdirAll(t, filepath.Join(root, "a")), "f.txt"), "")

				link := filepath.Join(mkdirAll(t, filepath.Join(root, "b")), "link.txt")
				if err := os.Symlink(filepath.Join("..", "a", "f.txt"), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			// Same, but the link leaves the repository entirely. Following it
			// would answer with the *other* repository's remote.
			name:   "target is a symlink into another repository",
			remote: "origin",
			build: func(t *testing.T) string {
				other := newTmpGitRepo(t)
				runGit(t, other, "remote", "add", "origin", "https://github.com/example/other.git")
				writeFile(t, filepath.Join(other, "f.txt"), "")

				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				link := filepath.Join(root, "link.txt")
				if err := os.Symlink(filepath.Join(other, "f.txt"), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			// The mirror image of the two above: when the symlink *is* a
			// directory, git chdirs into it and getcwd() reports the
			// destination, so both paths must name a/sub, not b/link. Discovery
			// has to start from the resolved directory for that to hold.
			name:   "target is a symlinked directory",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				mkdirAll(t, filepath.Join(root, "a", "sub"))

				link := filepath.Join(mkdirAll(t, filepath.Join(root, "b")), "link")
				if err := os.Symlink(filepath.Join("..", "a", "sub"), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			// And the sharp end of it: the link leaves the repository, so the
			// answer is the *other* repository's remote. Discovering from the
			// unresolved path would hand back this repository's URL instead —
			// a plausible URL for the wrong repository.
			name:   "target is a symlinked directory in another repository",
			remote: "origin",
			build: func(t *testing.T) string {
				other := newTmpGitRepo(t)
				runGit(t, other, "remote", "add", "origin", "https://github.com/example/other.git")
				mkdirAll(t, filepath.Join(other, "deep"))

				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				link := filepath.Join(root, "link")
				if err := os.Symlink(filepath.Join(other, "deep"), link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			// core.preferSymlinkRefs makes .git/HEAD a symlink to the loose ref,
			// so reading it yields 40 hex characters. That used to look like a
			// detached HEAD and report the branch as "HEAD" while git reported
			// "main". Deprecated in git 2.54, hence a refusal rather than
			// support.
			name:      "HEAD is a symlink (core.preferSymlinkRefs)",
			remote:    "origin",
			fallsBack: true,
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				runGit(t, root, "config", "core.preferSymlinkRefs", "true")
				runGit(t, root, "checkout", "-q", "-b", "sidebranch")
				runGit(t, root, "checkout", "-q", "-")

				info, err := os.Lstat(filepath.Join(root, ".git", "HEAD"))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					t.Skip("this git does not honour core.preferSymlinkRefs")
				}
				return root
			},
		},
		{
			// `git -c <key>=<value> <alias>` exports GIT_CONFIG_PARAMETERS, not
			// GIT_CONFIG_COUNT, and gopen's documented invocation is a git
			// alias. No file scan can see this, so it has to disqualify the
			// fast path outright.
			name:       "GIT_CONFIG_PARAMETERS carries an insteadOf",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				// The exact quoting git itself exports, verified against 2.54.
				t.Setenv("GIT_CONFIG_PARAMETERS",
					`'url.https://gitlab.com/mirror/.insteadOf'='https://github.com/example/'`)
				return root
			},
		},

		// --- include and includeIf ---
		//
		// These pin the whole point of resolving includes instead of refusing
		// on sight. Each fixture pairs a global config that includes something
		// with a repository, and asserts the fast path lands where git lands —
		// whether that means answering, or refusing and letting git answer.
		{
			name:   "plain include of a harmless file",
			remote: "origin",
			build: func(t *testing.T) string {
				root := repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "extra"), "[user]\n\temail = a@b\n")
					return "[include]\n\tpath = " + filepath.Join(home, "extra") + "\n"
				})
				return root
			},
		},
		{
			name:   "include with a path relative to the including file",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					mkdirAll(t, filepath.Join(home, "conf.d"))
					writeFile(t, filepath.Join(home, "conf.d", "extra"), "[user]\n\temail = a@b\n")
					return "[include]\n\tpath = conf.d/extra\n"
				})
			},
		},
		{
			name:   "include with a ~ path",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "tilde"), "[user]\n\temail = a@b\n")
					return "[include]\n\tpath = ~/tilde\n"
				})
			},
		},
		{
			name:       "include that defines insteadOf",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "rewrite"),
						"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n")
					return "[include]\n\tpath = ~/rewrite\n"
				})
			},
		},
		{
			name:       "include that defines the remote URL itself",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://included.example/from-include",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "remote"),
						"[remote \"origin\"]\n\turl = https://included.example/from-include.git\n")
					return "[include]\n\tpath = ~/remote\n"
				})
			},
		},
		{
			name:       "nested include reaching an insteadOf",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "mid"), "[include]\n\tpath = ~/deep\n")
					writeFile(t, filepath.Join(home, "deep"),
						"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n")
					return "[include]\n\tpath = ~/mid\n"
				})
			},
		},
		{
			// git aborts with "exceeded maximum include depth", so every git
			// command fails and the fast path has to refuse as well.
			name:      "include cycle",
			remote:    "origin",
			fallsBack: true,
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "loop"),
						"[include]\n\tpath = "+filepath.Join(home, ".gitconfig")+"\n")
					return "[include]\n\tpath = ~/loop\n"
				})
			},
		},
		{
			name:   "includeIf gitdir that does not match",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "work"),
						"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n")
					elsewhere := mkdirAll(t, filepath.Join(home, "elsewhere"))
					return "[includeIf \"gitdir:" + elsewhere + "/\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// The mirror image: the same rewrite, but a condition that does
			// match, so git applies it and the fast path must stand down.
			name:       "includeIf gitdir that matches",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "work"),
						"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n")
					return "[includeIf \"gitdir:/\"]\n\tpath = ~/work\n"
				})
			},
		},
		// The gitdir: conditions below pin gitDirMayMatch on real patterns
		// rather than the degenerate "/" above, which only ever exercised the
		// leading-slash test. Each pairs the same insteadOf with a different
		// shape of pattern, so a wrong verdict shows up as a URL divergence.
		{
			name:       "includeIf gitdir naming the git directory exactly",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					return "[includeIf \"gitdir:" + gitDir + "\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// A trailing separator turns the pattern into a "**" prefix match,
			// so the parent directory covers the git dir beneath it.
			name:       "includeIf gitdir with a trailing separator matches a parent",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					return "[includeIf \"gitdir:" + filepath.Dir(filepath.Dir(gitDir)) + "/\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// Without the trailing separator the same parent is an exact-match
			// pattern, which the git directory cannot equal.
			name:   "includeIf gitdir naming a parent without a trailing separator",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					return "[includeIf \"gitdir:" + filepath.Dir(gitDir) + "\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// A sibling under the same parent: the prefix test must not fire.
			name:   "includeIf gitdir naming a sibling directory",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					sibling := filepath.Join(filepath.Dir(filepath.Dir(gitDir)), "sibling")
					mkdirAll(t, sibling)
					return "[includeIf \"gitdir:" + sibling + "/\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// git's realpath of the pattern fails, leaving the condition false.
			name:   "includeIf gitdir naming a path that does not exist",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					return "[includeIf \"gitdir:" + filepath.Join(gitDir, "no", "such", "dir") + "/\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// A wildcard is outside the subset gitDirMayMatch reproduces, so it
			// answers "maybe" and the file is judged on its contents — which
			// here really do rewrite the URL, and git agrees.
			name:       "includeIf gitdir with a wildcard that matches",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				return repoWithGlobalConfigFor(t, func(home, gitDir string) string {
					writeFile(t, filepath.Join(home, "work"), rewriteToMirror)
					return "[includeIf \"gitdir:" + filepath.Dir(gitDir) + "/**\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			// The pattern is written against ~ and must be spliced with the
			// symlink-resolved home, which is what git compares.
			name:       "includeIf gitdir rooted at ~",
			remote:     "origin",
			fallsBack:  true,
			wantGitURL: "https://gitlab.com/mirror/repo",
			build: func(t *testing.T) string {
				home := t.TempDir()
				root := newTmpGitRepoIn(t, mkdirAll(t, filepath.Join(home, "src")))
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				writeFile(t, filepath.Join(home, "work"), rewriteToMirror)

				global := filepath.Join(home, ".gitconfig")
				writeFile(t, global, "[includeIf \"gitdir:~/src/\"]\n\tpath = ~/work\n")
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				t.Setenv("GIT_CONFIG_GLOBAL", global)
				return root
			},
		},
		{
			// A condition git never evaluates to true, on a file that would
			// have changed the answer had it been read.
			name:   "includeIf with an unknown condition",
			remote: "origin",
			build: func(t *testing.T) string {
				return repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "work"),
						"[url \"https://gitlab.com/mirror/\"]\n\tinsteadOf = https://github.com/example/\n")
					return "[includeIf \"nosuchcondition:x\"]\n\tpath = ~/work\n"
				})
			},
		},
		{
			name:   "includeIf onbranch, which is never evaluated here",
			remote: "origin",
			build: func(t *testing.T) string {
				root := repoWithGlobalConfig(t, func(home string) string {
					writeFile(t, filepath.Join(home, "onbranch"), "[user]\n\temail = a@b\n")
					return "[includeIf \"onbranch:main\"]\n\tpath = ~/onbranch\n"
				})
				runGit(t, root, "checkout", "-q", "-b", "main")
				return root
			},
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			pinConfigScope(t)
			target := f.build(t)

			fast, fastErr := readRepoContextFromDisk(target, f.remote)
			slow, slowErr := repoContextViaGit(target, f.remote)

			if f.wantGitURL != "" && slow.baseURL != f.wantGitURL {
				t.Fatalf("precondition: git resolves the remote to %q, want %q (err=%v)",
					slow.baseURL, f.wantGitURL, slowErr)
			}

			if f.fallsBack {
				if fastErr == nil {
					t.Fatalf("fast path answered %+v, but this shape must defer to git (%+v)", fast, slow)
				}
				// Standing down is only correct if the caller still gets git's
				// answer, so check the dispatcher lands there.
				got, gotErr := getRepoContext(target, f.remote)
				if (gotErr == nil) != (slowErr == nil) || got != slow {
					t.Errorf("dispatcher returned (%+v, %v), want git's (%+v, %v)",
						got, gotErr, slow, slowErr)
				}
				return
			}

			// Both paths must *answer*, not merely agree. Accepting a double
			// failure here would let any fixture whose build silently failed to
			// construct the intended shape pass without exercising anything.
			// A shape that is meant to fail belongs in fallsBack or in
			// TestDifferential_ErrorCases.
			if fastErr != nil || slowErr != nil {
				t.Fatalf("both paths must resolve this shape:\n  fast path: %v\n  git path:  %v", fastErr, slowErr)
			}
			if fast != slow {
				t.Errorf("fast path diverges from git:\n  fast: %+v\n  git:  %+v", fast, slow)
			}
		})
	}
}

// repoWithGlobalConfig builds an ordinary repository with an "origin", then
// installs a global config written by makeConfig, which receives the fake HOME
// it can drop included files into.
//
// The repository is created before the config is installed on purpose: a config
// git itself refuses, such as an include cycle, would otherwise break its setup
// instead of the lookup under test.
func repoWithGlobalConfig(t *testing.T, makeConfig func(home string) string) string {
	t.Helper()
	return repoWithGlobalConfigFor(t, func(home, _ string) string { return makeConfig(home) })
}

// repoWithGlobalConfigFor is repoWithGlobalConfig for configs that have to name
// the repository, such as an includeIf gitdir: condition. makeConfig also
// receives the repository's git directory, symlink-resolved because that is the
// text git matches a gitdir: pattern against.
func repoWithGlobalConfigFor(t *testing.T, makeConfig func(home, gitDir string) string) string {
	t.Helper()
	root := newTmpGitRepo(t)
	runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
	gitDir := realPath(t, filepath.Join(root, ".git"))

	home := t.TempDir()
	global := filepath.Join(home, ".gitconfig")
	writeFile(t, global, makeConfig(home, gitDir))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	return root
}

// TestDifferential_ErrorCases checks the two paths agree on failure too.
func TestDifferential_ErrorCases(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		build  func(t *testing.T) string
	}{
		{
			name:   "unknown remote",
			remote: "nope",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				return root
			},
		},
		{
			name:   "no remote at all",
			remote: "origin",
			build:  func(t *testing.T) string { return newTmpGitRepo(t) },
		},
		{
			name:   "outside any repository",
			remote: "origin",
			build:  func(t *testing.T) string { return t.TempDir() },
		},
		{
			name:   "path that does not exist",
			remote: "origin",
			build:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
		},
		{
			// Deliberate choice, not an oversight. `git init` + `git remote add`
			// with no commit leaves HEAD pointing at a branch that does not
			// exist: `git branch --show-current` prints it, but
			// `git rev-parse --abbrev-ref HEAD` exits 128 and rev-parse is what
			// the subprocess path runs. The fast path used to answer "main"
			// here, which was both a silent divergence and a URL no forge would
			// serve, so it now refuses too — the behaviour gopen had before the
			// fast path existed.
			name:   "unborn branch, before the first commit",
			remote: "origin",
			build: func(t *testing.T) string {
				dir := t.TempDir()
				runGit(t, dir, "init")
				runGit(t, dir, "remote", "add", "origin", "https://github.com/example/repo.git")
				return dir
			},
		},
		{
			// git's iskeychar() rejects '_', so this aborts *every* git command
			// in the repository with "fatal: bad config line". The fast path has
			// to refuse the whole file rather than skip the line it cannot read
			// and hand back a URL git never got far enough to report.
			name:   "config key git's parser rejects",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				appendFile(t, filepath.Join(root, ".git", "config"), "[foo]\n\tbad_key = 1\n")
				return root
			},
		},
		{
			// A bare `url` line: git records a NULL value and dies with
			// "missing value for 'remote.x.url'" on any remote lookup, origin's
			// included. Answering with origin's perfectly good URL would still
			// be a divergence — git refuses to run at all.
			name:   "valueless remote url key elsewhere in the config",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
				appendFile(t, filepath.Join(root, ".git", "config"), "[remote \"x\"]\n\turl\n")
				return root
			},
		},
		{
			// The same, on the remote actually being asked for.
			name:   "valueless url on the remote under lookup",
			remote: "origin",
			build: func(t *testing.T) string {
				root := newTmpGitRepo(t)
				appendFile(t, filepath.Join(root, ".git", "config"),
					"[remote \"origin\"]\n\turl\n\turl = https://github.com/example/repo.git\n")
				return root
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pinConfigScope(t)
			target := c.build(t)
			_, fastErr := readRepoContextFromDisk(target, c.remote)
			_, slowErr := repoContextViaGit(target, c.remote)
			if fastErr == nil || slowErr == nil {
				t.Errorf("both paths must refuse this shape:\n  fast path: %v\n  git path:  %v", fastErr, slowErr)
			}
		})
	}
}

// appendFile appends to an existing file, for fixtures that graft extra config
// onto a repository git already created.
func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// newTmpSubmoduleWithRemote is newTmpSubmodule with a web-shaped "origin" on
// the submodule itself, so the differential fixtures have a URL to resolve.
// `submodule add` already creates an origin pointing at the local source, so
// this is a set-url rather than an add.
func newTmpSubmoduleWithRemote(t *testing.T) (super, sub string) {
	t.Helper()
	super, sub = newTmpSubmodule(t)
	runGit(t, sub, "remote", "set-url", "origin", "https://github.com/example/inner.git")
	return super, sub
}

func BenchmarkGetRepoContext(b *testing.B) {
	// Without this the benchmark silently measures the fallback twice: a real
	// ~/.gitconfig containing includeIf (or insteadOf) disqualifies the fast
	// path for every repository on the machine.
	pinConfigScope(b)

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
