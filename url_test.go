package main

import "testing"

// --- convertToHTTPS ---

func TestConvertToHTTPS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// SSH shorthand: git@host:user/repo
		{"git@github.com:user/repo.git", "https://github.com/user/repo"},
		{"git@gitlab.com:group/sub/repo.git", "https://gitlab.com/group/sub/repo"},
		{"git@bitbucket.org:user/repo.git", "https://bitbucket.org/user/repo"},
		// ssh:// scheme
		{"ssh://git@github.com/user/repo.git", "https://github.com/user/repo"},
		// git:// scheme
		{"git://github.com/user/repo.git", "https://github.com/user/repo"},
		// Already HTTPS — passthrough
		{"https://github.com/user/repo", "https://github.com/user/repo"},
		// HTTPS with .git suffix stripped
		{"https://github.com/user/repo.git", "https://github.com/user/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := convertToHTTPS(tt.input)
			if got != tt.want {
				t.Errorf("convertToHTTPS(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- pathJoin ---

func TestPathJoin(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{"https://github.com/u/r", "tree", "main", ""}, "https://github.com/u/r/tree/main"},
		{[]string{"base", "", "middle", ""}, "base/middle"},
		{[]string{"a", "b", "c"}, "a/b/c"},
		{[]string{"a"}, "a"},
		{[]string{""}, ""},
		{[]string{"", ""}, ""},
	}

	for _, tt := range tests {
		got := pathJoin(tt.parts...)
		if got != tt.want {
			t.Errorf("pathJoin(%v) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

// --- Line anchors ---

func TestAnchorLN(t *testing.T) {
	tests := []struct{ start, end, want string }{
		{"", "", ""},
		{"42", "", "#L42"},
		{"42", "50", "#L42-L50"},
	}
	for _, tt := range tests {
		if got := anchorLN(tt.start, tt.end); got != tt.want {
			t.Errorf("anchorLN(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestAnchorGL(t *testing.T) {
	tests := []struct{ start, end, want string }{
		{"", "", ""},
		{"42", "", "#L42"},
		{"42", "50", "#L42-50"}, // GitLab: no second "L"
	}
	for _, tt := range tests {
		if got := anchorGL(tt.start, tt.end); got != tt.want {
			t.Errorf("anchorGL(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestAnchorBB(t *testing.T) {
	tests := []struct{ start, end, want string }{
		{"", "", ""},
		{"42", "", "#lines-42"},
		{"42", "50", "#lines-42:50"},
	}
	for _, tt := range tests {
		if got := anchorBB(tt.start, tt.end); got != tt.want {
			t.Errorf("anchorBB(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestAnchorADO(t *testing.T) {
	tests := []struct{ start, end, want string }{
		{"", "", ""},
		{"42", "", "&line=42&lineEnd=42&lineStartColumn=1&lineEndColumn=1"},
		{"42", "50", "&line=42&lineEnd=50&lineStartColumn=1&lineEndColumn=1"},
	}
	for _, tt := range tests {
		if got := anchorADO(tt.start, tt.end); got != tt.want {
			t.Errorf("anchorADO(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

// --- buildWebURL ---

func TestBuildWebURL(t *testing.T) {
	tests := []struct {
		name       string
		ctx        repoContext
		lineNumber string
		commitHash string
		want       string
	}{
		// GitHub
		{
			name: "github/root",
			ctx:  repoContext{baseURL: "https://github.com/user/repo", branch: "main"},
			want: "https://github.com/user/repo/tree/main",
		},
		{
			name: "github/file",
			ctx:  repoContext{baseURL: "https://github.com/user/repo", branch: "main", relPath: "main.go"},
			want: "https://github.com/user/repo/tree/main/main.go",
		},
		{
			name:       "github/file+line",
			ctx:        repoContext{baseURL: "https://github.com/user/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42",
			want:       "https://github.com/user/repo/tree/main/main.go#L42",
		},
		{
			name:       "github/file+range",
			ctx:        repoContext{baseURL: "https://github.com/user/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42-50",
			want:       "https://github.com/user/repo/tree/main/main.go#L42-L50",
		},
		{
			name:       "github/commit-page",
			ctx:        repoContext{baseURL: "https://github.com/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://github.com/user/repo/commit/abc1234",
		},
		{
			name:       "github/file-at-commit",
			ctx:        repoContext{baseURL: "https://github.com/user/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			want:       "https://github.com/user/repo/blob/abc1234/main.go",
		},
		{
			name:       "github/file-at-commit+line",
			ctx:        repoContext{baseURL: "https://github.com/user/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			lineNumber: "10",
			want:       "https://github.com/user/repo/blob/abc1234/main.go#L10",
		},

		// GitLab
		{
			name: "gitlab/root",
			ctx:  repoContext{baseURL: "https://gitlab.com/user/repo", branch: "main"},
			want: "https://gitlab.com/user/repo/-/tree/main",
		},
		{
			name: "gitlab/file",
			ctx:  repoContext{baseURL: "https://gitlab.com/user/repo", branch: "main", relPath: "main.go"},
			want: "https://gitlab.com/user/repo/-/tree/main/main.go",
		},
		{
			name:       "gitlab/file+range",
			ctx:        repoContext{baseURL: "https://gitlab.com/user/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42-50",
			want:       "https://gitlab.com/user/repo/-/tree/main/main.go#L42-50",
		},
		{
			name:       "gitlab/commit-page",
			ctx:        repoContext{baseURL: "https://gitlab.com/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://gitlab.com/user/repo/-/commit/abc1234",
		},
		{
			name:       "gitlab/file-at-commit",
			ctx:        repoContext{baseURL: "https://gitlab.com/user/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			want:       "https://gitlab.com/user/repo/-/blob/abc1234/main.go",
		},
		// Self-hosted GitLab (contains "gitlab" but not "gitlab.com")
		{
			name: "gitlab/self-hosted",
			ctx:  repoContext{baseURL: "https://git.mycompany.com/gitlab/user/repo", branch: "feat"},
			want: "https://git.mycompany.com/gitlab/user/repo/-/tree/feat",
		},

		// Bitbucket
		{
			name: "bitbucket/root",
			ctx:  repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main"},
			want: "https://bitbucket.org/user/repo/src/main",
		},
		{
			name: "bitbucket/file",
			ctx:  repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main", relPath: "main.go"},
			want: "https://bitbucket.org/user/repo/src/main/main.go",
		},
		{
			name:       "bitbucket/file+line",
			ctx:        repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42",
			want:       "https://bitbucket.org/user/repo/src/main/main.go#lines-42",
		},
		{
			name:       "bitbucket/file+range",
			ctx:        repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42-50",
			want:       "https://bitbucket.org/user/repo/src/main/main.go#lines-42:50",
		},
		{
			name:       "bitbucket/commit-page",
			ctx:        repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://bitbucket.org/user/repo/commits/abc1234",
		},
		{
			name:       "bitbucket/file-at-commit",
			ctx:        repoContext{baseURL: "https://bitbucket.org/user/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			want:       "https://bitbucket.org/user/repo/src/abc1234/main.go",
		},

		// Azure DevOps
		{
			name: "azure/root",
			ctx:  repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main"},
			want: "https://dev.azure.com/org/proj/_git/repo?version=GBmain",
		},
		{
			name: "azure/file",
			ctx:  repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main", relPath: "main.go"},
			want: "https://dev.azure.com/org/proj/_git/repo?version=GBmain&path=/main.go",
		},
		{
			name:       "azure/file+line",
			ctx:        repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42",
			want:       "https://dev.azure.com/org/proj/_git/repo?version=GBmain&path=/main.go&line=42&lineEnd=42&lineStartColumn=1&lineEndColumn=1",
		},
		{
			name:       "azure/file+range",
			ctx:        repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42-50",
			want:       "https://dev.azure.com/org/proj/_git/repo?version=GBmain&path=/main.go&line=42&lineEnd=50&lineStartColumn=1&lineEndColumn=1",
		},
		{
			name:       "azure/commit-page",
			ctx:        repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://dev.azure.com/org/proj/_git/repo/commit/abc1234",
		},
		{
			name:       "azure/file-at-commit",
			ctx:        repoContext{baseURL: "https://dev.azure.com/org/proj/_git/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			want:       "https://dev.azure.com/org/proj/_git/repo?version=GCabc1234&path=/main.go",
		},

		// Gitea
		{
			name: "gitea/root",
			ctx:  repoContext{baseURL: "https://gitea.example.com/user/repo", branch: "main"},
			want: "https://gitea.example.com/user/repo/src/branch/main",
		},
		{
			name: "gitea/file",
			ctx:  repoContext{baseURL: "https://gitea.example.com/user/repo", branch: "main", relPath: "main.go"},
			want: "https://gitea.example.com/user/repo/src/branch/main/main.go",
		},
		{
			name:       "gitea/commit-page",
			ctx:        repoContext{baseURL: "https://gitea.example.com/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://gitea.example.com/user/repo/commit/abc1234",
		},
		{
			name:       "gitea/file-at-commit",
			ctx:        repoContext{baseURL: "https://gitea.example.com/user/repo", branch: "main", relPath: "main.go"},
			commitHash: "abc1234",
			want:       "https://gitea.example.com/user/repo/src/commit/abc1234/main.go",
		},

		// Gogs
		{
			name: "gogs/root",
			ctx:  repoContext{baseURL: "https://gogs.example.com/user/repo", branch: "main"},
			want: "https://gogs.example.com/user/repo/src/main",
		},
		{
			name:       "gogs/commit-page",
			ctx:        repoContext{baseURL: "https://gogs.example.com/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://gogs.example.com/user/repo/commit/abc1234",
		},

		// AWS CodeCommit
		{
			name: "codecommit/root",
			ctx:  repoContext{baseURL: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo", branch: "main"},
			want: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo/browse/refs/heads/main/--/",
		},
		{
			name: "codecommit/file",
			ctx:  repoContext{baseURL: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo", branch: "main", relPath: "main.go"},
			want: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo/browse/refs/heads/main/--/main.go",
		},
		{
			name:       "codecommit/commit-page",
			ctx:        repoContext{baseURL: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo/commit/abc1234",
		},
		{
			name:       "codecommit/line-ignored",
			ctx:        repoContext{baseURL: "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo", branch: "main", relPath: "main.go"},
			lineNumber: "42",
			want:       "https://console.aws.amazon.com/codesuite/codecommit/repositories/repo/browse/refs/heads/main/--/main.go",
		},

		// Default fallback
		{
			name: "default/root",
			ctx:  repoContext{baseURL: "https://custom.git.host/user/repo", branch: "main"},
			want: "https://custom.git.host/user/repo/tree/main",
		},
		{
			name:       "default/commit-page",
			ctx:        repoContext{baseURL: "https://custom.git.host/user/repo", branch: "main"},
			commitHash: "abc1234",
			want:       "https://custom.git.host/user/repo/commit/abc1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildWebURL(tt.ctx, tt.lineNumber, tt.commitHash)
			if got != tt.want {
				t.Errorf("buildWebURL()\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}
