package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	tests := []struct {
		name  string
		shell string
		want  string
	}{
		{"bash full path", "/bin/bash", "bash"},
		{"zsh full path", "/usr/bin/zsh", "zsh"},
		{"fish full path", "/usr/local/bin/fish", "fish"},
		{"unknown shell defaults to bash", "/bin/sh", "bash"},
		{"empty SHELL defaults to bash", "", "bash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			got := detectShell()
			if got != tt.want {
				t.Errorf("detectShell() = %q, want %q (SHELL=%q)", got, tt.want, tt.shell)
			}
		})
	}
}

func TestPrintCompletion(t *testing.T) {
	tests := []struct {
		shell   string
		contain string
	}{
		{"bash", "_gopen"},
		{"zsh", "compdef _gopen gopen"},
		{"fish", "complete -c gopen"},
		{"unknown", "_gopen"}, // defaults to bash
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			out := captureStdout(t, func() { printCompletion(tt.shell) })
			if !strings.Contains(out, tt.contain) {
				t.Errorf("printCompletion(%q) output missing %q", tt.shell, tt.contain)
			}
		})
	}
}

// captureStdout redirects os.Stdout to a pipe, calls f, and returns the output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
