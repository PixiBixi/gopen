package main

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildOpenCmd(t *testing.T) {
	const url = "https://github.com/example/repo"
	tests := []struct {
		goos      string
		wantErr   bool
		wantFirst string
	}{
		{"darwin", false, "open"},
		{"linux", false, "xdg-open"},
		{"windows", false, "cmd"},
		{"plan9", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			cmd, err := buildOpenCmd(url, tt.goos)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "unsupported platform") {
					t.Errorf("error %q missing 'unsupported platform'", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("buildOpenCmd(%q) error = %v", tt.goos, err)
			}
			if cmd.Path == "" {
				t.Error("cmd.Path is empty")
			}
			if !strings.Contains(cmd.Path, tt.wantFirst) {
				t.Errorf("cmd.Path = %q, want to contain %q", cmd.Path, tt.wantFirst)
			}
		})
	}
}

func TestBuildClipboardCmd(t *testing.T) {
	alwaysFound := func(string) (string, error) { return "/usr/bin/found", nil }
	neverFound := func(string) (string, error) { return "", errors.New("not found") }
	firstFound := func() func(string) (string, error) {
		calls := 0
		return func(name string) (string, error) {
			calls++
			if calls == 1 {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}

	tests := []struct {
		name       string
		goos       string
		lookPath   func(string) (string, error)
		wantErr    bool
		wantBinary string
	}{
		{"darwin", "darwin", alwaysFound, false, "pbcopy"},
		{"windows", "windows", alwaysFound, false, "clip"},
		{"unsupported", "plan9", alwaysFound, true, ""},
		{"linux wl-copy found", "linux", alwaysFound, false, "wl-copy"},
		{"linux xclip fallback", "linux", firstFound(), false, "wl-copy"},
		{"linux no tools found", "linux", neverFound, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := buildClipboardCmd(tt.goos, tt.lookPath)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildClipboardCmd(%q) error = %v", tt.goos, err)
			}
			if cmd == nil {
				t.Error("expected non-nil cmd")
			}
			if tt.wantBinary != "" && !strings.Contains(cmd.Path, tt.wantBinary) {
				t.Errorf("cmd.Path = %q, want to contain %q", cmd.Path, tt.wantBinary)
			}
		})
	}
}
