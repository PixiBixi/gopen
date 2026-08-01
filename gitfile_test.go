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
