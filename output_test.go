package main

import (
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
	tests := []struct {
		goos    string
		wantErr bool
	}{
		{"darwin", false},
		{"windows", false},
		{"plan9", true},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			cmd, err := buildClipboardCmd(tt.goos)
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
		})
	}
}
