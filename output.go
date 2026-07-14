package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

func openBrowser(url string) error {
	cmd, err := buildOpenCmd(url, runtime.GOOS)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// buildOpenCmd returns the OS-appropriate command to open url in a browser.
func buildOpenCmd(url, goos string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		return exec.Command("open", url), nil
	case "linux":
		return exec.Command("xdg-open", url), nil
	case "windows":
		return exec.Command("cmd", "/c", "start", url), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", goos)
	}
}

func copyToClipboard(text string) error {
	cmd, err := buildClipboardCmd(runtime.GOOS, exec.LookPath)
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start clipboard command: %w", err)
	}
	if _, err := stdin.Write([]byte(text)); err != nil {
		return fmt.Errorf("failed to write to clipboard: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("failed to close stdin: %w", err)
	}
	return cmd.Wait()
}

// buildClipboardCmd returns the OS-appropriate command to write to the clipboard via stdin.
// lookPath is injected to allow testing the Linux fallback chain without system dependencies.
func buildClipboardCmd(goos string, lookPath func(string) (string, error)) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		return exec.Command("pbcopy"), nil
	case "linux":
		if _, err := lookPath("wl-copy"); err == nil {
			return exec.Command("wl-copy"), nil
		} else if _, err := lookPath("xclip"); err == nil {
			return exec.Command("xclip", "-selection", "clipboard"), nil
		} else if _, err := lookPath("xsel"); err == nil {
			return exec.Command("xsel", "--clipboard", "--input"), nil
		}
		return nil, errors.New("no clipboard utility found (install wl-copy, xclip, or xsel)")
	case "windows":
		return exec.Command("clip"), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", goos)
	}
}
