package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// configEntry is one key/value pair from a git config file. Entries are kept in
// file order because order is significant: `git remote get-url` returns the
// first url value for a remote, while `git config --get` returns the last.
type configEntry struct {
	key   string
	value string
}

// parseGitConfig reads git's INI-like config format and returns every entry in
// file order. Keys are flattened to section.subsection.name; section and name
// are lowercased, a quoted subsection keeps its case.
//
// Malformed input is an error rather than a partial result: the caller falls
// back to the git binary instead of acting on a half-understood file.
func parseGitConfig(r io.Reader) ([]configEntry, error) {
	var (
		entries []configEntry
		section string // already lowercased, subsection appended, "" until the first header
	)

	sc := bufio.NewScanner(r)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' {
			s, err := parseSectionHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			section = s
			continue
		}

		if section == "" {
			return nil, fmt.Errorf("line %d: key %q outside of any section", lineNo, line)
		}

		// A value continues on the next line when its trailing backslash run has
		// odd length: the last backslash is the continuation marker and the ones
		// before it pair up into escaped backslashes.
		//
		// The lines are joined verbatim. git's parse_value only trims *trailing*
		// whitespace of the finished value, so leading whitespace on a
		// continuation line is part of the value and must be preserved.
		for trailingBackslashes(line)%2 == 1 {
			if !sc.Scan() {
				return nil, fmt.Errorf("line %d: dangling line continuation", lineNo)
			}
			lineNo++
			line = line[:len(line)-1] + sc.Text()
		}

		name, value, err := parseKeyValue(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		entries = append(entries, configEntry{key: section + "." + name, value: value})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading git config: %w", err)
	}
	return entries, nil
}

// trailingBackslashes counts the backslash run at the end of s. Its parity is
// what decides a line continuation: an odd run ends in an unescaped backslash.
func trailingBackslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// parseSectionHeader turns "[remote \"origin\"]" or "[branch.Main]" into the
// flattened prefix "remote.origin" / "branch.main".
func parseSectionHeader(line string) (string, error) {
	if !strings.HasSuffix(line, "]") {
		return "", errors.New("unterminated section header")
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	if inner == "" {
		return "", errors.New("empty section name")
	}

	// Quoted subsection: [section "SubSection"] — case is preserved.
	if i := strings.IndexByte(inner, '"'); i >= 0 {
		if !strings.HasSuffix(inner, `"`) || i == len(inner)-1 {
			return "", errors.New("unterminated subsection name")
		}
		name := strings.ToLower(strings.TrimSpace(inner[:i]))
		sub := unescapeSubsection(inner[i+1 : len(inner)-1])
		if name == "" {
			return "", errors.New("empty section name")
		}
		return name + "." + sub, nil
	}

	// Dotted short form: [branch.Main] — the subsection is lowercased.
	return strings.ToLower(inner), nil
}

// unescapeSubsection handles the only two escapes git allows in a subsection
// name: \" and \\.
func unescapeSubsection(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseKeyValue splits "name = value" into its parts. A bare name is an
// implicit boolean true, which is how git reads it.
func parseKeyValue(line string) (name, value string, err error) {
	rawName, rawValue, hasValue := strings.Cut(line, "=")
	if !hasValue {
		name = strings.ToLower(strings.TrimSpace(stripInlineComment(line)))
		if name == "" {
			return "", "", errors.New("empty key name")
		}
		return name, "true", nil
	}

	name = strings.ToLower(strings.TrimSpace(rawName))
	if name == "" {
		return "", "", errors.New("empty key name")
	}
	value, err = parseValue(strings.TrimSpace(rawValue))
	if err != nil {
		return "", "", err
	}
	return name, value, nil
}

// stripInlineComment drops everything from the first unquoted # or ;.
func stripInlineComment(s string) string {
	if i := strings.IndexAny(s, "#;"); i >= 0 {
		return s[:i]
	}
	return s
}

// parseValue interprets a config value: quoted spans keep their whitespace and
// comment characters, backslash escapes are expanded, and an unquoted trailing
// comment is dropped.
func parseValue(s string) (string, error) {
	var (
		b       strings.Builder
		inQuote bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == '\\':
			if i+1 >= len(s) {
				return "", errors.New("dangling escape in value")
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case '"', '\\':
				b.WriteByte(s[i])
			default:
				return "", fmt.Errorf("unknown escape %q in value", s[i])
			}
		case (c == '#' || c == ';') && !inQuote:
			return strings.TrimRight(b.String(), " \t"), nil
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return "", errors.New("unterminated quoted value")
	}
	return strings.TrimRight(b.String(), " \t"), nil
}

// firstConfigValue returns the first value for key in file order. This matches
// `git remote get-url`, which returns a remote's first url, and deliberately
// differs from `git config --get`, which returns the last.
func firstConfigValue(entries []configEntry, key string) (string, bool) {
	for _, e := range entries {
		if e.key == key {
			return e.value, true
		}
	}
	return "", false
}

const headRefPrefix = "refs/heads/"

// branchFromHEAD reads gitDir/HEAD and returns the short branch name.
// A detached HEAD yields the literal "HEAD", which is what
// `git rev-parse --abbrev-ref HEAD` prints in that state.
func branchFromHEAD(gitDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}
	head := strings.TrimSpace(string(raw))

	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		branch, ok := strings.CutPrefix(strings.TrimSpace(ref), headRefPrefix)
		if !ok || branch == "" {
			return "", fmt.Errorf("HEAD points at %q, which is not a branch", ref)
		}
		return branch, nil
	}

	if isHexSHA(head) {
		return "HEAD", nil // detached
	}
	return "", fmt.Errorf("unrecognized HEAD content: %q", head)
}

// isHexSHA reports whether s looks like a git object id.
func isHexSHA(s string) bool {
	// 40 hex chars for SHA-1, 64 for SHA-256.
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
