package main

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr bool
	}{
		// Defaults
		{
			name: "no args",
			args: nil,
			want: config{remoteName: "origin"},
		},

		// Boolean flags
		{
			name: "version short",
			args: []string{"-v"},
			want: config{remoteName: "origin", version: true},
		},
		{
			name: "version long",
			args: []string{"--version"},
			want: config{remoteName: "origin", version: true},
		},
		{
			name: "copy short",
			args: []string{"-c"},
			want: config{remoteName: "origin", copy: true},
		},
		{
			name: "copy long",
			args: []string{"--copy"},
			want: config{remoteName: "origin", copy: true},
		},

		// --remote / -r
		{
			name: "remote short",
			args: []string{"-r", "upstream"},
			want: config{remoteName: "upstream"},
		},
		{
			name: "remote long",
			args: []string{"--remote", "upstream"},
			want: config{remoteName: "upstream"},
		},
		{
			name: "remote attached short",
			args: []string{"-rupstream"},
			want: config{remoteName: "upstream"},
		},
		{
			name: "remote equals long",
			args: []string{"--remote=upstream"},
			want: config{remoteName: "upstream"},
		},

		// --line / -l
		{
			name: "line short",
			args: []string{"-l", "42"},
			want: config{remoteName: "origin", line: "42"},
		},
		{
			name: "line long",
			args: []string{"--line", "42"},
			want: config{remoteName: "origin", line: "42"},
		},
		{
			name: "line attached short",
			args: []string{"-l42"},
			want: config{remoteName: "origin", line: "42"},
		},
		{
			name: "line equals long",
			args: []string{"--line=42"},
			want: config{remoteName: "origin", line: "42"},
		},
		{
			name: "line range",
			args: []string{"-l", "42-50"},
			want: config{remoteName: "origin", line: "42-50"},
		},

		// --commit
		{
			name: "commit long",
			args: []string{"--commit", "abc1234"},
			want: config{remoteName: "origin", commit: "abc1234"},
		},
		{
			name: "commit equals",
			args: []string{"--commit=abc1234"},
			want: config{remoteName: "origin", commit: "abc1234"},
		},

		// --completion
		{
			name: "completion auto (no shell arg)",
			args: []string{"--completion"},
			want: config{remoteName: "origin", completion: "auto"},
		},
		{
			name: "completion bash",
			args: []string{"--completion", "bash"},
			want: config{remoteName: "origin", completion: "bash"},
		},
		{
			name: "completion zsh",
			args: []string{"--completion", "zsh"},
			want: config{remoteName: "origin", completion: "zsh"},
		},
		{
			name: "completion fish",
			args: []string{"--completion", "fish"},
			want: config{remoteName: "origin", completion: "fish"},
		},
		{
			name: "completion equals bash",
			args: []string{"--completion=bash"},
			want: config{remoteName: "origin", completion: "bash"},
		},
		{
			name: "completion unknown shell becomes auto and arg treated as path",
			args: []string{"--completion", "csh"},
			want: config{remoteName: "origin", completion: "auto", paths: []string{"csh"}},
		},

		// Positional args
		{
			name: "single path",
			args: []string{"main.go"},
			want: config{remoteName: "origin", paths: []string{"main.go"}},
		},
		{
			name: "path before flags",
			args: []string{"main.go", "-l", "42", "-c"},
			want: config{remoteName: "origin", paths: []string{"main.go"}, line: "42", copy: true},
		},
		{
			name: "flags before path",
			args: []string{"-l", "42", "-c", "main.go"},
			want: config{remoteName: "origin", paths: []string{"main.go"}, line: "42", copy: true},
		},
		{
			name: "flags interleaved with path",
			args: []string{"-c", "main.go", "--commit", "abc"},
			want: config{remoteName: "origin", paths: []string{"main.go"}, copy: true, commit: "abc"},
		},

		// Double dash separator
		{
			name: "double dash passes remaining as paths",
			args: []string{"--", "-notaflag", "file.go"},
			want: config{remoteName: "origin", paths: []string{"-notaflag", "file.go"}},
		},
		{
			name: "flags before double dash are parsed",
			args: []string{"-c", "--", "file.go"},
			want: config{remoteName: "origin", copy: true, paths: []string{"file.go"}},
		},

		// Errors
		{
			name:    "unknown flag",
			args:    []string{"--unknown"},
			wantErr: true,
		},
		{
			name:    "missing value for -l",
			args:    []string{"-l"},
			wantErr: true,
		},
		{
			name:    "missing value for -r",
			args:    []string{"-r"},
			wantErr: true,
		},
		{
			name:    "missing value for --commit",
			args:    []string{"--commit"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseArgs(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseArgs(%v)\n  got  %+v\n  want %+v", tt.args, got, tt.want)
			}
		})
	}
}

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
