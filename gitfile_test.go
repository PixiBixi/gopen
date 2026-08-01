package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
		// The continuation cases below assert the exact bytes git 2.54 produces;
		// each was verified against `git config --file <f> --list -z` before
		// being written down. git's parse_value trims only *trailing*
		// whitespace, so a continuation line's leading whitespace is part of
		// the value.
		{
			name:  "continuation preserves the next line's leading whitespace",
			input: "[x]\n\ty = one\\\n   two\n",
			want:  []configEntry{{"x.y", "one   two"}},
		},
		{
			name:  "continuation inside a quoted value preserves leading whitespace",
			input: "[x]\n\ty = \"one\\\n   two\"\n",
			want:  []configEntry{{"x.y", "one   two"}},
		},
		{
			name:  "continuation preserves leading tabs in a URL",
			input: "[remote \"origin\"]\n\turl = https://example.com/a/\\\n\t\tb.git\n",
			want:  []configEntry{{"remote.origin.url", "https://example.com/a/\t\tb.git"}},
		},
		{
			// Trailing run of 3: one escaped pair plus a continuation marker.
			name:  "odd backslash run of three continues the line",
			input: "[x]\n\ty = a\\\\\\\nb\n",
			want:  []configEntry{{"x.y", "a\\b"}},
		},
		{
			// Trailing run of 5: two escaped pairs plus a continuation marker.
			name:  "odd backslash run of five continues the line",
			input: "[x]\n\ty = a\\\\\\\\\\\nb\n",
			want:  []configEntry{{"x.y", "a\\\\b"}},
		},
		{
			// Even run: no continuation, the next line is an ordinary key.
			name:  "even backslash run does not continue the line",
			input: "[x]\n\ty = a\\\\\n\tb = c\n",
			want: []configEntry{
				{"x.y", "a\\"},
				{"x.b", "c"},
			},
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

// --- discoverGitDir ---

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func mkdirAll(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path, content string) {
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
		{"core.bare", "false"},
		{"remote.origin.url", "a"},
		{"core.bare", "true"},
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

// pinConfigScope isolates a test from the machine's real git configuration.
// GIT_CONFIG_NOSYSTEM is deliberately left unset so that the system scope is
// still exercised, pinned to an empty file through GIT_CONFIG_SYSTEM.
func pinConfigScope(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	writeFile(t, empty, "")
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	t.Setenv("GIT_CONFIG_COUNT", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // os.UserHomeDir on Windows
	for _, name := range gitDiscoveryEnvVars {
		t.Setenv(name, "")
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
		if needsGitFallback(dir, dir) {
			t.Error("needsGitFallback() = true, want false for a clean scope")
		}
	})

	t.Run("a missing config file is not a reason to fall back", func(t *testing.T) {
		pinConfigScope(t)
		dir := t.TempDir()
		if needsGitFallback(dir, dir) {
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
				if !needsGitFallback(dir, dir) {
					t.Errorf("needsGitFallback() = false, want true when %s is set", name)
				}
			})
		}
	})

	t.Run("GIT_CONFIG_COUNT forces the fallback", func(t *testing.T) {
		pinConfigScope(t)
		dir := localScope(t, cleanConfig)
		t.Setenv("GIT_CONFIG_COUNT", "1")
		if !needsGitFallback(dir, dir) {
			t.Error("needsGitFallback() = false, want true when config comes from the environment")
		}
	})

	markers := []struct{ name, content string }{
		{"insteadOf", "[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"},
		{"uppercase INSTEADOF", "[url \"x\"]\n\tINSTEADOF = y\n"},
		{"pushInsteadOf", "[url \"x\"]\n\tpushInsteadOf = y\n"},
		{"include", "[include]\n\tpath = /other/config\n"},
		{"includeIf", "[includeIf \"gitdir:~/work/\"]\n\tpath = ~/work/.gitconfig\n"},
	}

	for _, m := range markers {
		t.Run(m.name+" in the global config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			t.Setenv("GIT_CONFIG_GLOBAL", writeConfig(t, m.content))
			dir := localScope(t, cleanConfig)
			if !needsGitFallback(dir, dir) {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})

		t.Run(m.name+" in the system config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			t.Setenv("GIT_CONFIG_SYSTEM", writeConfig(t, m.content))
			dir := localScope(t, cleanConfig)
			if !needsGitFallback(dir, dir) {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})

		t.Run(m.name+" in the local config forces the fallback", func(t *testing.T) {
			pinConfigScope(t)
			dir := localScope(t, m.content)
			if !needsGitFallback(dir, dir) {
				t.Errorf("needsGitFallback() = false, want true for %s", m.name)
			}
		})
	}

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
				if !needsGitFallback(gitDir, commonDir) {
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
				if got := needsGitFallback(dir, dir); got != tc.want {
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
		if !needsGitFallback(dir, dir) {
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
		if !needsGitFallback(dir, dir) {
			t.Error("needsGitFallback() = false, want true for an insteadOf in ~/.gitconfig")
		}
	})
}

// TestSystemConfigPaths_CoversTheRealOne guards the one guess in
// configScopePaths that cannot be derived from the environment: git's
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
