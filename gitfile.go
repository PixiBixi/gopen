package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

	// Real git accepts any amount of whitespace (including none at all)
	// between "ref:" and the path, so trim rather than match a literal
	// "ref: " prefix.
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		branch, ok := strings.CutPrefix(ref, headRefPrefix)
		// A bare prefix match isn't enough: anything trailing the ref on
		// the same line (extra tokens, embedded whitespace) or spilling
		// onto another line would otherwise be swallowed into what looks
		// like a valid branch name. git itself refuses to treat such a
		// HEAD as a ref at all, so reject it here too rather than risk
		// silently returning a name git would never produce.
		if !ok || !isValidBranchName(branch) {
			return "", fmt.Errorf("HEAD points at %q, which is not a branch", ref)
		}
		return branch, nil
	}

	if isHexSHA(head) {
		return "HEAD", nil // detached
	}
	return "", fmt.Errorf("unrecognized HEAD content: %q", head)
}

// isValidBranchName reports whether s is safe to return as a branch name.
// It rejects the empty string plus whitespace and other ASCII control
// characters (including DEL), which is the minimum git itself enforces on
// ref names. Failing this check means the HEAD content isn't fully
// understood, so the caller errors out and falls back to the git binary
// instead of risking a value git would never produce.
func isValidBranchName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] == 0x7f {
			return false
		}
	}
	return true
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

// gitDiscoveryEnvVars are the environment variables that make git skip or bound
// the .git walk entirely. Reproducing each of them is more surface than the
// fast path is worth, so their mere presence disqualifies it: the caller falls
// back to the git binary, which applies them correctly.
//
// GIT_PREFIX is deliberately absent — git sets it for `!alias` commands (which
// is how gopen is usually invoked) and it does not affect discovery.
var gitDiscoveryEnvVars = []string{
	"GIT_DIR",
	"GIT_COMMON_DIR",
	"GIT_WORK_TREE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_CEILING_DIRECTORIES",
}

// gitDiscoveryEnvOverride returns the name of the first variable from
// gitDiscoveryEnvVars that is set, or "" when none is. It is the single home
// for that check: both the walk and needsGitFallback ask it rather than each
// testing its own subset.
func gitDiscoveryEnvOverride() string {
	for _, name := range gitDiscoveryEnvVars {
		if os.Getenv(name) != "" {
			return name
		}
	}
	return ""
}

// safeRepoExtensions lists the extensions.* keys that cannot change how the
// branch, the remote URL or the work tree are resolved. Anything else — most
// importantly extensions.refstorage=reftable, where .git/HEAD is the decoy
// "ref: refs/heads/.invalid" — has to be refused. extensions.worktreeConfig is
// handled separately, by reading the per-worktree config it enables.
var safeRepoExtensions = map[string]bool{
	"objectformat":       true, // sha1 or sha256; both are plain hex in HEAD
	"compatobjectformat": true,
	"preciousobjects":    true,
	"partialclone":       true,
	"relativeworktrees":  true,
	"noop":               true,
	"noop-v1":            true,
}

// discoverGitDir walks up from start until it finds a .git entry, and returns
// paths only if it is certain they name the same repository `git rev-parse`
// would name.
//
// A .git directory is the normal case. A .git file holds "gitdir: <path>" and
// appears in linked worktrees and submodules; there, HEAD lives in gitDir while
// config lives in the shared commonDir, so the two are returned separately.
//
// workTree is the directory holding the .git entry. Symlinks are deliberately
// left unresolved so that workTree and the caller's target path stay in the
// same namespace; `git rev-parse --show-toplevel` reports the real path, so the
// two can differ textually while naming the same directory.
//
// Known and accepted gap: git stops the walk at a filesystem boundary unless
// GIT_DISCOVERY_ACROSS_FILESYSTEM is set, and this walk does not, because
// st_dev is not portable through the standard library. It only matters when a
// mount point sits between the target and the repository root.
func discoverGitDir(start string) (gitDir, commonDir, workTree string, err error) {
	l, err := discoverRepoLayout(start)
	if err != nil {
		return "", "", "", err
	}
	return l.gitDir, l.commonDir, l.workTree, nil
}

// repoLayout is everything the walk learned about a repository, including the
// shared config it had to parse anyway to vet it. Carrying the entries here is
// what keeps the config file parsed once per run instead of once per consumer.
type repoLayout struct {
	gitDir    string
	commonDir string
	workTree  string
	config    []configEntry // <commonDir>/config, in file order
}

// discoverRepoLayout is the walk itself; see discoverGitDir for the contract.
func discoverRepoLayout(start string) (repoLayout, error) {
	if name := gitDiscoveryEnvOverride(); name != "" {
		return repoLayout{}, fmt.Errorf("%s is set, cannot reproduce git's repository discovery", name)
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return repoLayout{}, fmt.Errorf("failed to resolve %q: %w", start, err)
	}

	for {
		candidate := filepath.Join(dir, ".git")
		info, statErr := os.Stat(candidate)
		switch {
		case statErr != nil:
			// keep walking up, but see the self test below first
		case info.IsDir():
			return describeRepo(candidate, dir)
		default:
			target, readErr := readGitDirFile(candidate)
			if readErr != nil {
				// git treats an unreadable .git *file* as fatal rather than
				// walking past it, so stopping here matches.
				return repoLayout{}, readErr
			}
			return describeRepo(target, dir)
		}

		// git tries, at every level and in this order, <dir>/.git as a file,
		// <dir>/.git as a directory, then <dir> itself as a git directory.
		// That last test is how it finds a bare repository, a submodule gitdir
		// or a linked-worktree gitdir used as the working directory. Skipping
		// it would let the walk sail past a real git directory and latch onto
		// an enclosing repository, which is the one unacceptable outcome, so
		// the walk stops here even though a work tree cannot be derived.
		if isGitDirItself(dir) {
			return repoLayout{}, fmt.Errorf("%s is a git directory, not a work tree", dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return repoLayout{}, errors.New("not in a git repository")
		}
		dir = parent
	}
}

// isGitDirItself reports whether dir is a git directory rather than a work
// tree, which is git's third test at every level of the walk.
//
// The HEAD pre-check keeps the cost to one Lstat for the overwhelmingly common
// case of an ordinary parent directory, and — more importantly — keeps a stray
// file named "commondir" somewhere up the tree from aborting the whole walk.
func isGitDirItself(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, "HEAD")); err != nil {
		return false
	}
	commonDir, err := resolveCommonDir(dir)
	if err != nil {
		// A gitdir whose commondir pointer cannot be read is still a gitdir.
		return true
	}
	return checkGitDirLayout(dir, commonDir) == nil
}

// describeRepo completes and vets a discovery hit.
//
// Every check below refuses rather than guesses. That cuts both ways on
// purpose: git skips a .git directory that fails its own validity test and
// keeps walking, but silently continuing here would risk naming an outer
// repository whenever this check is stricter than git's, which is the one
// unacceptable outcome. An error only costs the caller a fallback to the git
// binary.
func describeRepo(gitDir, workTree string) (repoLayout, error) {
	commonDir, err := resolveCommonDir(gitDir)
	if err != nil {
		return repoLayout{}, err
	}
	if err := checkGitDirLayout(gitDir, commonDir); err != nil {
		return repoLayout{}, fmt.Errorf("%s is not a usable git directory: %w", gitDir, err)
	}
	entries, err := readConfigFile(filepath.Join(commonDir, "config"))
	if err != nil {
		return repoLayout{}, fmt.Errorf("%s: %w", gitDir, err)
	}
	if err := checkRepoConfig(entries, gitDir, commonDir, workTree); err != nil {
		return repoLayout{}, fmt.Errorf("%s: %w", gitDir, err)
	}
	return repoLayout{gitDir: gitDir, commonDir: commonDir, workTree: workTree, config: entries}, nil
}

const (
	gitFilePrefix = "gitdir: "
	// refPrefix is the namespace every ref name lives under; HEAD must point
	// somewhere inside it for a directory to be a repository at all.
	refPrefix = "refs/"
)

// readGitDirFile parses a ".git" file of the form "gitdir: <path>". A relative
// path is resolved against the directory holding the .git file.
//
// The format is matched as strictly as git's read_gitfile_gently(): the prefix
// includes exactly one space, and only trailing newlines are stripped. Trimming
// more would accept files git rejects and, worse, turn a padded pointer into a
// path git never resolves to.
func readGitDirFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	content := strings.TrimRight(string(raw), "\r\n")

	target, ok := strings.CutPrefix(content, gitFilePrefix)
	if !ok || target == "" {
		return "", fmt.Errorf("invalid gitfile format: %s", path)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), nil
}

// resolveCommonDir returns the shared git directory for a linked worktree,
// whose gitdir holds only HEAD and its own refs while config, objects and refs
// stay in the common dir. Absent a commondir file, gitDir is already the
// common dir.
func resolveCommonDir(gitDir string) (string, error) {
	path := filepath.Join(gitDir, "commondir")
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return gitDir, nil
	case err != nil:
		// git dies on an unreadable commondir; guessing gitDir here would
		// silently point config lookups at the wrong file.
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}

	common := strings.TrimRight(string(raw), "\r\n")
	if common == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return filepath.Clean(common), nil
}

// checkGitDirLayout mirrors git's is_git_directory(): a valid HEAD in the
// gitdir, plus objects/ and refs/ in the common dir. Without it, any directory
// that merely happens to be named .git would be taken for a repository.
func checkGitDirLayout(gitDir, commonDir string) error {
	if err := validateHeadRef(filepath.Join(gitDir, "HEAD")); err != nil {
		return err
	}
	for _, name := range []string{"objects", "refs"} {
		path := filepath.Join(commonDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("missing %s: %w", name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
	}
	return nil
}

// validateHeadRef mirrors git's validate_headref(): HEAD must be a symlink into
// refs/, a "ref:" pointing into refs/, or a raw object id. It only decides
// whether the directory is a repository at all — branchFromHEAD is the stricter
// gate on the value actually returned to the caller.
func validateHeadRef(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("missing HEAD: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("failed to read the HEAD symlink: %w", err)
		}
		if !strings.HasPrefix(target, refPrefix) {
			return fmt.Errorf("HEAD symlinks to %q, which is not a ref", target)
		}
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read HEAD: %w", err)
	}
	head := string(raw)
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		if strings.HasPrefix(strings.TrimLeft(ref, " \t\n\v\f\r"), refPrefix) {
			return nil
		}
	}
	if isHexSHA(strings.TrimSpace(head)) {
		return nil
	}
	return fmt.Errorf("HEAD is neither a ref nor an object id: %q", strings.TrimSpace(head))
}

// checkRepoConfig refuses the repository shapes where the paths found by the
// walk would not be the ones git uses: an unknown on-disk format, a bare
// repository (no work tree at all), or a relocated work tree.
func checkRepoConfig(entries []configEntry, gitDir, commonDir, workTree string) error {
	if version, ok := lastConfigValue(entries, "core.repositoryformatversion"); ok && version != "0" && version != "1" {
		return fmt.Errorf("unsupported repository format version %q", version)
	}

	worktreeConfig := false
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.key, "extensions.")
		if !ok {
			continue
		}
		switch {
		case name == "refstorage":
			if !strings.EqualFold(e.value, "files") {
				return fmt.Errorf("unsupported ref storage %q", e.value)
			}
		case name == "worktreeconfig":
			// Rejecting on the key alone would be too blunt: `git
			// sparse-checkout set`, `scalar clone` and `scalar register` all
			// turn this on permanently, so every sparse-checkout and Scalar
			// user would lose the fast path for good. Read the extra config
			// file instead.
			on, known := configBool(e.value)
			if !known {
				return fmt.Errorf("unrecognized extensions.worktreeConfig value %q", e.value)
			}
			worktreeConfig = on
		case !safeRepoExtensions[name]:
			return fmt.Errorf("unsupported repository extension %q", name)
		}
	}

	if worktreeConfig {
		// The per-worktree file lives in the *current* worktree's gitdir, which
		// for the main worktree is the common dir. Checked against git 2.54: a
		// linked worktree honours its own worktrees/<n>/config.worktree for
		// both core.bare and core.worktree, and the main worktree's
		// .git/config.worktree does not leak into it.
		wtEntries, err := readOptionalConfigFile(filepath.Join(gitDir, "config.worktree"))
		if err != nil {
			return err
		}
		if err := checkWorkTreeKeys(wtEntries, gitDir, workTree); err != nil {
			return err
		}
	}

	// With the extension off, the shared config's core.bare and core.worktree
	// are honoured only in the main worktree: inside a linked worktree git
	// ignores them (checked against git 2.54, including a worktree of a
	// submodule, whose shared config always carries core.worktree).
	//
	// This shortcut is coupled to extensions.worktreeConfig and must not be
	// read on its own. Turning the extension on flips the behaviour: git 2.54
	// then honours the *shared* core.bare and core.worktree in a linked
	// worktree too, so a shared core.worktree really does relocate
	// --show-toplevel there. Skipping the check in that state would report a
	// work tree git does not use.
	if !worktreeConfig && gitDir != commonDir {
		return nil
	}
	return checkWorkTreeKeys(entries, gitDir, workTree)
}

// checkWorkTreeKeys refuses the core.bare / core.worktree settings that would
// make git report a work tree other than the one the walk found.
//
// Each config file is vetted on its own rather than merged first. That is
// stricter than git, which lets config.worktree override the shared config, but
// the extra strictness can only cost a fallback, never a wrong answer.
func checkWorkTreeKeys(entries []configEntry, gitDir, workTree string) error {
	if value, ok := lastConfigValue(entries, "core.bare"); ok {
		bare, known := configBool(value)
		if !known {
			return fmt.Errorf("unrecognized core.bare value %q", value)
		}
		if bare {
			return errors.New("repository is bare, it has no work tree")
		}
	}

	// core.worktree relocates the work tree. Submodules set it to the directory
	// that holds the .git file, which is exactly what the walk found; anything
	// else means git would report a different toplevel. A relative value is
	// resolved against the gitdir, including for a linked worktree.
	value, ok := lastConfigValue(entries, "core.worktree")
	if !ok {
		return nil
	}
	configured := value
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(gitDir, configured)
	}
	if filepath.Clean(configured) != workTree {
		return fmt.Errorf("core.worktree points at %q, not at %q", filepath.Clean(configured), workTree)
	}
	return nil
}

// readConfigFile parses a git config file from disk.
func readConfigFile(path string) ([]configEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	entries, err := parseGitConfig(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return entries, nil
}

// readOptionalConfigFile is readConfigFile for a file git tolerates the absence
// of. Only a missing file is benign: an unreadable or malformed one is an error,
// because its contents could have changed the answer.
func readOptionalConfigFile(path string) ([]configEntry, error) {
	entries, err := readConfigFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

// lastConfigValue returns the last value for key in file order, which is what
// `git config --get` resolves a repeated key to. It is the counterpart of
// firstConfigValue, which matches `git remote get-url` instead.
func lastConfigValue(entries []configEntry, key string) (string, bool) {
	value, found := "", false
	for _, e := range entries {
		if e.key == key {
			value, found = e.value, true
		}
	}
	return value, found
}

// configBool interprets a git boolean. The second result reports whether the
// value was understood at all: git also accepts any non-zero integer, so an
// unrecognized value must not be assumed false.
func configBool(value string) (result, known bool) {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0", "":
		return false, true
	}
	return false, false
}

// fallbackMarkers are config directives the fast path does not implement.
// Their mere presence anywhere in scope sends the whole lookup to git, because
// any of them can change the remote URL that would otherwise be reported.
//
// "insteadof" also catches pushInsteadOf, and "[include" also catches
// "[include \"x\"]". Both over-match, which costs at most one fork.
var fallbackMarkers = []string{"insteadof", "[include", "includeif"}

// needsGitFallback reports whether the pure-Go path must defer to the git
// binary. It is deliberately conservative: a false positive costs one fork, a
// false negative costs a wrong URL.
//
// The scan is a raw lowercase substring match, not a parse. A config file that
// merely mentions one of the markers in a comment or a remote URL loses the
// fast path, which is the cheap side of the trade.
func needsGitFallback(gitDir, commonDir string) bool {
	if gitDiscoveryEnvOverride() != "" {
		return true
	}
	// GIT_CONFIG_COUNT/KEY/VALUE inject config straight from the environment,
	// including url.*.insteadOf, and no file scan can see it.
	if os.Getenv("GIT_CONFIG_COUNT") != "" {
		return true
	}
	return slices.ContainsFunc(configScopePaths(gitDir, commonDir), fileContainsMarker)
}

// configScopePaths lists the config files git would consult, in scope order.
// Paths that do not exist are harmless; they simply contribute nothing.
//
// Known and accepted gap: the system config path is compiled into the git
// binary (ETC_GITCONFIG), so it can only be guessed. systemConfigPaths covers
// the standard location and the one implied by where git sits on PATH, which
// together cover Homebrew, the usual Linux packages and Git for Windows. A git
// built with an unusual prefix would have its system config missed.
func configScopePaths(gitDir, commonDir string) []string {
	var paths []string

	// git_env_bool: only a true-ish value suppresses the system config, and an
	// uninterpretable one is treated as "read it", which is the safe side here.
	if on, known := configBool(os.Getenv("GIT_CONFIG_NOSYSTEM")); !on || !known {
		if p := os.Getenv("GIT_CONFIG_SYSTEM"); p != "" {
			paths = append(paths, p)
		} else {
			paths = append(paths, systemConfigPaths()...)
		}
	}

	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		paths = append(paths, p)
	} else {
		// git reads both the XDG file and ~/.gitconfig, and falls back to
		// ~/.config/git/config when XDG_CONFIG_HOME is unset.
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			paths = append(paths, filepath.Join(xdg, "git", "config"))
		} else if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".config", "git", "config"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".gitconfig"))
		}
	}

	if commonDir != "" {
		paths = append(paths, filepath.Join(commonDir, "config"))
	}
	// The per-worktree file is read whenever extensions.worktreeConfig is on.
	// Scanning it unconditionally is cheaper than deciding whether it applies,
	// and a stale file that git ignores can only cost a fork.
	if gitDir != "" {
		paths = append(paths, filepath.Join(gitDir, "config.worktree"))
	}
	if commonDir != "" && commonDir != gitDir {
		paths = append(paths, filepath.Join(commonDir, "config.worktree"))
	}
	return paths
}

// systemConfigPaths guesses where git's system-wide config lives.
func systemConfigPaths() []string {
	paths := []string{filepath.Join("/etc", "gitconfig")}

	// A prefixed install keeps it next to the binary: /opt/homebrew/bin/git
	// implies /opt/homebrew/etc/gitconfig, C:\Program Files\Git\cmd\git.exe
	// implies C:\Program Files\Git\etc\gitconfig.
	if bin, err := exec.LookPath("git"); err == nil {
		if abs, err := filepath.Abs(bin); err == nil {
			prefix := filepath.Dir(filepath.Dir(abs))
			paths = append(paths, filepath.Join(prefix, "etc", "gitconfig"))
			paths = append(paths, filepath.Join(prefix, "mingw64", "etc", "gitconfig"))
		}
	}
	// Git for Windows also honours a machine-wide file under ProgramData.
	if pd := os.Getenv("ProgramData"); pd != "" {
		paths = append(paths, filepath.Join(pd, "Git", "config"))
	}
	return paths
}

// fileContainsMarker reports whether path holds any fallback marker. An absent
// file is not a marker. An unreadable one is: git may well be able to read what
// this process cannot, and its contents could change the URL.
func fileContainsMarker(path string) bool {
	raw, err := os.ReadFile(path) // #nosec G304 -- paths come from git's own config scopes
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	lower := strings.ToLower(string(raw))
	for _, m := range fallbackMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// readRepoContextFromDisk builds a repoContext by reading .git directly, with
// no subprocess. It errors rather than guessing whenever the on-disk state is
// not something it fully understands, so the caller can fall back to git.
func readRepoContextFromDisk(targetPath, remoteName string) (repoContext, error) {
	dir, err := containingDir(targetPath)
	if err != nil {
		return repoContext{}, err
	}

	layout, err := discoverRepoLayout(dir)
	if err != nil {
		return repoContext{}, err
	}

	// The walk only vets the repository's shape. This is the second gate, on
	// the configuration that could rewrite the URL out from under us.
	if needsGitFallback(layout.gitDir, layout.commonDir) {
		return repoContext{}, errors.New("configuration in scope can rewrite the remote URL")
	}

	branch, err := branchFromHEAD(layout.gitDir)
	if err != nil {
		return repoContext{}, err
	}

	// firstConfigValue, not lastConfigValue: `git remote get-url` returns a
	// remote's *first* url line.
	remoteURL, ok := firstConfigValue(layout.config, "remote."+remoteName+".url")
	if !ok {
		return repoContext{}, fmt.Errorf("no URL configured for remote %q", remoteName)
	}

	relPath, err := relativeToRoot(layout.workTree, targetPath)
	if err != nil {
		return repoContext{}, err
	}

	return repoContext{
		baseURL: convertToHTTPS(remoteURL),
		branch:  branch,
		relPath: relPath,
	}, nil
}

// containingDir returns targetPath itself when it is a directory, otherwise its
// parent.
func containingDir(targetPath string) (string, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() {
		return targetPath, nil
	}
	return filepath.Dir(targetPath), nil
}

// relativeToRoot computes the repo-relative path, mapping the root itself to "".
func relativeToRoot(root, targetPath string) (string, error) {
	rel, err := filepath.Rel(root, targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}
