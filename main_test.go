package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestVersionFlag verifies the binary exits cleanly with --version.
func TestVersionFlag(t *testing.T) {
	gopen, err := exec.LookPath("./gopen")
	if err != nil {
		t.Skip("gopen binary not built — run `go build -o gopen .` first")
	}
	out, err := exec.Command(gopen, "--version").Output()
	if err != nil {
		t.Fatalf("gopen --version: %v", err)
	}
	if !strings.HasPrefix(string(out), "gopen ") {
		t.Errorf("unexpected output: %q", string(out))
	}
}
