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
