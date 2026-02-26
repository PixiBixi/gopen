package main

import (
	"fmt"
	"os"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		usage()
		os.Exit(1)
	}

	if cfg.version {
		fmt.Printf("gopen %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	targetPath, err := resolvePath(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, err := getRepoContext(targetPath, cfg.remoteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	webURL := buildWebURL(ctx, cfg.line, cfg.commit)

	if cfg.copy {
		if err := copyToClipboard(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error copying to clipboard: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("URL copied to clipboard: %s\n", webURL)
	} else {
		fmt.Printf("Opening: %s\n", webURL)
		if err := openBrowser(webURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
			os.Exit(1)
		}
	}
}
