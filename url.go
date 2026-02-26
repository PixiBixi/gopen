package main

import (
	"fmt"
	"regexp"
	"strings"
)

// provider defines how to build URLs for a specific git hosting platform.
type provider struct {
	match      func(baseURL string) bool
	treeURL    func(base, ref, path string) string
	commitURL  func(base, hash, path string) string
	lineAnchor func(start, end string) string
}

// pathJoin builds a slash-joined URL, skipping empty segments.
func pathJoin(parts ...string) string {
	var segments []string
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	return strings.Join(segments, "/")
}

// Line anchor helpers — return a fragment or query suffix for line highlighting.

func anchorLN(start, end string) string { // GitHub, Gitea, default: #L42 or #L42-L50
	if start == "" {
		return ""
	}
	if end == "" {
		return "#L" + start
	}
	return "#L" + start + "-L" + end
}

func anchorGL(start, end string) string { // GitLab: #L42 or #L42-50
	if start == "" {
		return ""
	}
	if end == "" {
		return "#L" + start
	}
	return "#L" + start + "-" + end
}

func anchorBB(start, end string) string { // Bitbucket: #lines-42 or #lines-42:50
	if start == "" {
		return ""
	}
	if end == "" {
		return "#lines-" + start
	}
	return "#lines-" + start + ":" + end
}

func anchorADO(start, end string) string { // Azure DevOps: query params
	if start == "" {
		return ""
	}
	if end == "" {
		end = start
	}
	return fmt.Sprintf("&line=%s&lineEnd=%s&lineStartColumn=1&lineEndColumn=1", start, end)
}

var providers = []provider{
	{
		match: func(u string) bool { return strings.Contains(u, "github.com") },
		treeURL: func(base, ref, path string) string {
			return pathJoin(base, "tree", ref, path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commit", hash)
			}
			return pathJoin(base, "blob", hash, path)
		},
		lineAnchor: anchorLN,
	},
	{
		match: func(u string) bool {
			return strings.Contains(u, "gitlab.com") || strings.Contains(u, "gitlab")
		},
		treeURL: func(base, ref, path string) string {
			return pathJoin(base, "-/tree", ref, path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "-/commit", hash)
			}
			return pathJoin(base, "-/blob", hash, path)
		},
		lineAnchor: anchorGL,
	},
	{
		match: func(u string) bool { return strings.Contains(u, "bitbucket.org") },
		treeURL: func(base, ref, path string) string {
			return pathJoin(base, "src", ref, path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commits", hash)
			}
			return pathJoin(base, "src", hash, path)
		},
		lineAnchor: anchorBB,
	},
	{
		match: func(u string) bool {
			return strings.Contains(u, "dev.azure.com") || strings.Contains(u, "visualstudio.com")
		},
		treeURL: func(base, ref, path string) string {
			if path == "" {
				return base + "?version=GB" + ref
			}
			return base + "?version=GB" + ref + "&path=/" + path
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commit", hash)
			}
			return base + "?version=GC" + hash + "&path=/" + path
		},
		lineAnchor: anchorADO,
	},
	{
		match: func(u string) bool { return strings.Contains(u, "gitea") },
		treeURL: func(base, ref, path string) string {
			return pathJoin(base, "src/branch", ref, path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commit", hash)
			}
			return pathJoin(base, "src/commit", hash, path)
		},
		lineAnchor: anchorLN,
	},
	{
		match: func(u string) bool { return strings.Contains(u, "gogs") },
		treeURL: func(base, ref, path string) string {
			return pathJoin(base, "src", ref, path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commit", hash)
			}
			return pathJoin(base, "src", hash, path)
		},
		lineAnchor: anchorLN,
	},
	{
		match: func(u string) bool {
			return strings.Contains(u, "console.aws.amazon.com") || strings.Contains(u, "codecommit")
		},
		treeURL: func(base, ref, path string) string {
			if path == "" {
				return pathJoin(base, "browse/refs/heads", ref, "--") + "/"
			}
			return pathJoin(base, "browse/refs/heads", ref, "--", path)
		},
		commitURL: func(base, hash, path string) string {
			if path == "" {
				return pathJoin(base, "commit", hash)
			}
			return pathJoin(base, "browse", hash, "--", path)
		},
		lineAnchor: func(_, _ string) string { return "" }, // not supported
	},
}

// defaultProvider uses GitHub-style URLs as a fallback.
var defaultProvider = provider{
	treeURL: func(base, ref, path string) string {
		return pathJoin(base, "tree", ref, path)
	},
	commitURL: func(base, hash, path string) string {
		if path == "" {
			return pathJoin(base, "commit", hash)
		}
		return pathJoin(base, "blob", hash, path)
	},
	lineAnchor: anchorLN,
}

func detectProvider(baseURL string) provider {
	for _, p := range providers {
		if p.match(baseURL) {
			return p
		}
	}
	return defaultProvider
}

func buildWebURL(ctx repoContext, lineNumber, commitHash string) string {
	var startLine, endLine string
	if lineNumber != "" {
		parts := strings.SplitN(lineNumber, "-", 2)
		startLine = parts[0]
		if len(parts) > 1 {
			endLine = parts[1]
		}
	}

	p := detectProvider(ctx.baseURL)

	var url string
	if commitHash != "" {
		url = p.commitURL(ctx.baseURL, commitHash, ctx.relPath)
	} else {
		url = p.treeURL(ctx.baseURL, ctx.branch, ctx.relPath)
	}

	return url + p.lineAnchor(startLine, endLine)
}

func convertToHTTPS(url string) string {
	url = strings.TrimSuffix(url, ".git")

	sshRegex := regexp.MustCompile(`^git@([^:]+):(.+)$`)
	if matches := sshRegex.FindStringSubmatch(url); matches != nil {
		return fmt.Sprintf("https://%s/%s", matches[1], matches[2])
	}

	if strings.HasPrefix(url, "ssh://") {
		url = strings.TrimPrefix(url, "ssh://")
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return "https://" + url
	}

	if strings.HasPrefix(url, "git://") {
		return strings.Replace(url, "git://", "https://", 1)
	}

	return url
}
