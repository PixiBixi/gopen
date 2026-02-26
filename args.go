package main

import (
	"fmt"
	"os"
	"strings"
)

type config struct {
	version    bool
	remoteName string
	copy       bool
	line       string
	commit     string
	paths      []string
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: gopen [flags] [path]

Open a Git repository path in the browser at the current branch.

Flags:
  -v, --version        Print version information
  -c, --copy           Copy URL to clipboard instead of opening browser
  -r, --remote <name>  Git remote to use (default: origin)
  -l, --line <n[-m]>   Highlight line or range (e.g. 42 or 42-50)
      --commit <hash>  Open a specific commit or file at that commit

Examples:
  gopen                        # current directory
  gopen main.go                # file on current branch
  gopen main.go -l 42          # file at line 42
  gopen --commit abc1234       # commit page
  gopen --commit abc1234 -c    # copy commit URL
`)
}

// parseArgs parses flags and positional arguments in any order.
// Supports: --flag value, --flag=value, -f value, -fvalue (for -l/-r).
func parseArgs(args []string) (config, error) {
	cfg := config{remoteName: "origin"}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// nextVal advances i and returns the next argument as a flag value.
		nextVal := func() (string, error) {
			i++
			if i >= len(args) {
				return "", fmt.Errorf("flag %s requires a value", arg)
			}
			return args[i], nil
		}

		switch arg {
		case "-v", "--version":
			cfg.version = true
		case "-c", "--copy":
			cfg.copy = true
		case "-r", "--remote":
			v, err := nextVal()
			if err != nil {
				return cfg, err
			}
			cfg.remoteName = v
		case "-l", "--line":
			v, err := nextVal()
			if err != nil {
				return cfg, err
			}
			cfg.line = v
		case "--commit":
			v, err := nextVal()
			if err != nil {
				return cfg, err
			}
			cfg.commit = v
		case "--":
			cfg.paths = append(cfg.paths, args[i+1:]...)
			return cfg, nil
		default:
			switch {
			case strings.HasPrefix(arg, "--remote="):
				cfg.remoteName = arg[len("--remote="):]
			case strings.HasPrefix(arg, "--line="):
				cfg.line = arg[len("--line="):]
			case strings.HasPrefix(arg, "--commit="):
				cfg.commit = arg[len("--commit="):]
			case len(arg) > 2 && arg[0] == '-' && arg[1] == 'r':
				cfg.remoteName = arg[2:] // -rorigin
			case len(arg) > 2 && arg[0] == '-' && arg[1] == 'l':
				cfg.line = arg[2:] // -l42
			case strings.HasPrefix(arg, "-"):
				return cfg, fmt.Errorf("unknown flag: %s", arg)
			default:
				cfg.paths = append(cfg.paths, arg)
			}
		}
	}
	return cfg, nil
}
