package main

import (
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
