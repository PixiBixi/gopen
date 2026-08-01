package main

import (
	"os"
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
