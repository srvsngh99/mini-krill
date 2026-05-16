// Package plugin — self_eyes.go provides read-only skills that let the agent
// inspect its own source code and runtime logs. These are the foundation for
// trustworthy autonomy: when the user asks "why did you do X", the agent can
// quote actual file lines and log entries instead of confabulating.
//
// All file access is sandboxed:
//   - Code reads are restricted to the repo root (resolved via go.mod walk
//     or the MINIKRILL_REPO env var).
//   - Log reads are restricted to ~/.mini-krill/logs/.
//   - Both refuse paths that look like secrets (config.yaml, .env, *.key).
//
// No write surface. Self-modification — even under the future "evolve"
// autonomy floor — must go through a separate self:propose-patch skill that
// writes diffs to a review directory, never to the repo.
package plugin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// secretPathPatterns are file/path fragments that are always refused.
// Belt-and-suspenders: even inside the repo root, never read these.
var secretPathPatterns = []string{
	"config.yaml", ".env", "credentials", ".pem", ".key", "secrets.",
	"private_key", "service_account",
}

// secretMatch reports whether abs is a secret-bearing path that must never be
// read, returning the pattern that matched. PR #32 nit: the old check was a
// raw substring over the whole path, so a benign file like
// "notconfig.yaml.txt" was wrongly refused (it contains "config.yaml"). This
// anchors each pattern to a real filename boundary instead:
//   - extension patterns (".env", ".pem", ".key") match a true suffix;
//   - prefix patterns ("secrets.") match the start of the basename;
//   - name patterns ("config.yaml", "credentials", "private_key",
//     "service_account") match the whole basename, a "<name>.bak"-style
//     variant, or a full path component — never a mid-name substring.
func secretMatch(abs string) (string, bool) {
	lowerAbs := strings.ToLower(abs)
	base := strings.ToLower(filepath.Base(abs))
	sep := string(filepath.Separator)
	for _, pat := range secretPathPatterns {
		switch {
		case strings.HasPrefix(pat, "."): // extension-like
			if strings.HasSuffix(base, pat) {
				return pat, true
			}
		case strings.HasSuffix(pat, "."): // prefix-like ("secrets.")
			if strings.HasPrefix(base, pat) {
				return pat, true
			}
		default: // whole-name / path-component token
			if base == pat ||
				strings.HasPrefix(base, pat+".") ||
				strings.Contains(lowerAbs, sep+pat+sep) ||
				strings.HasSuffix(lowerAbs, sep+pat) {
				return pat, true
			}
		}
	}
	return "", false
}

// resolveRepoRoot finds the repo root using two strategies:
//  1. MINIKRILL_REPO env var if set and points to a real dir
//  2. Walk up from runtime.Caller's source file looking for go.mod
//
// Returns "" if neither works (likely a `go install`-installed binary on a
// machine without the source).
func resolveRepoRoot() string {
	if env := os.Getenv("MINIKRILL_REPO"); env != "" {
		if info, err := os.Stat(env); err == nil && info.IsDir() {
			return env
		}
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// safeRepoPath returns the absolute, sandboxed path or an error if the input
// escapes the repo root or matches a secret pattern.
func safeRepoPath(rel string) (string, error) {
	root := resolveRepoRoot()
	if root == "" {
		return "", fmt.Errorf("repo source not visible — install via `git clone` or set MINIKRILL_REPO")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative to the repo root")
	}
	full := filepath.Join(root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootAbs, _ := filepath.Abs(root)
	if !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) && abs != rootAbs {
		return "", fmt.Errorf("path escapes the repo root")
	}
	if pat, hit := secretMatch(abs); hit {
		return "", fmt.Errorf("refusing to read paths matching %q (secret allowlist)", pat)
	}
	return abs, nil
}

// safeLogsPath restricts to ~/.mini-krill/logs/.
func safeLogsPath(name string) (string, error) {
	dir := filepath.Join(config.DataDir(), "logs")
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || strings.Contains(clean, "..") {
		return "", fmt.Errorf("log path must be a bare filename")
	}
	full := filepath.Join(dir, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	dirAbs, _ := filepath.Abs(dir)
	if !strings.HasPrefix(abs, dirAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the logs dir")
	}
	return abs, nil
}

// ---------------------------------------------------------------------------
// self:read-code
// ---------------------------------------------------------------------------

type selfReadCodeSkill struct{}

func (s *selfReadCodeSkill) Name() string { return "self:read-code" }
func (s *selfReadCodeSkill) Description() string {
	return "Read or grep the agent's own source code (read-only)"
}

// Execute parses commands of the form:
//
//	read <path>
//	grep <pattern> <glob>
//	tree <dir>
func (s *selfReadCodeSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return "Usage: read <path> | grep <pattern> <glob> | tree <dir>", nil
	}
	switch parts[0] {
	case "read":
		if len(parts) < 2 {
			return "Usage: read <path>", nil
		}
		path, err := safeRepoPath(parts[1])
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		const maxBytes = 50 * 1024
		if len(data) > maxBytes {
			data = append(data[:maxBytes], []byte("\n... (truncated at 50KB)")...)
		}
		return fmt.Sprintf("// %s\n%s", parts[1], string(data)), nil
	case "grep":
		if len(parts) < 3 {
			return "Usage: grep <pattern> <glob>", nil
		}
		return repoGrep(parts[1], strings.Join(parts[2:], " "))
	case "tree":
		dir := "."
		if len(parts) >= 2 {
			dir = parts[1]
		}
		return repoTree(dir)
	}
	return "Usage: read <path> | grep <pattern> <glob> | tree <dir>", nil
}

func repoGrep(pattern, glob string) (string, error) {
	root := resolveRepoRoot()
	if root == "" {
		return "", fmt.Errorf("repo source not visible — install via `git clone` or set MINIKRILL_REPO")
	}
	matches, err := filepath.Glob(filepath.Join(root, glob))
	if err != nil {
		return "", err
	}
	var hits []string
	plower := strings.ToLower(pattern)
	for _, path := range matches {
		// re-check sandbox per file
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if _, err := safeRepoPath(rel); err != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), plower) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNum, line))
				if len(hits) >= 100 {
					break
				}
			}
		}
		f.Close()
		if len(hits) >= 100 {
			break
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No matches for %q in %s.", pattern, glob), nil
	}
	return strings.Join(hits, "\n"), nil
}

func repoTree(rel string) (string, error) {
	root := resolveRepoRoot()
	if root == "" {
		return "", fmt.Errorf("repo source not visible")
	}
	abs, err := safeRepoPath(rel)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name()+"/")
		} else {
			out = append(out, e.Name())
		}
	}
	return strings.Join(out, "\n"), nil
}

// ---------------------------------------------------------------------------
// self:read-logs
// ---------------------------------------------------------------------------

type selfReadLogsSkill struct{}

func (s *selfReadLogsSkill) Name() string { return "self:read-logs" }
func (s *selfReadLogsSkill) Description() string {
	return "Tail or grep the agent's own log files (read-only)"
}

// Execute parses commands of the form:
//
//	tail <n>                       (default file: krill.log)
//	tail <file> <n>
//	grep <pattern>                 (default file: krill.log)
//	grep <file> <pattern>
//	errors                         (default file: krill.log; matches WARN/ERROR/panic)
func (s *selfReadLogsSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return "Usage: tail <n> | tail <file> <n> | grep <pattern> | grep <file> <pattern> | errors", nil
	}
	switch parts[0] {
	case "tail":
		file, n := "krill.log", 50
		switch len(parts) {
		case 2:
			_, _ = fmt.Sscanf(parts[1], "%d", &n)
		case 3:
			file = parts[1]
			_, _ = fmt.Sscanf(parts[2], "%d", &n)
		}
		return logTail(file, n)
	case "grep":
		file := "krill.log"
		pattern := ""
		if len(parts) == 2 {
			pattern = parts[1]
		} else if len(parts) >= 3 {
			file = parts[1]
			pattern = strings.Join(parts[2:], " ")
		}
		return logGrep(file, pattern, time.Time{})
	case "errors":
		file := "krill.log"
		if len(parts) >= 2 {
			file = parts[1]
		}
		matches := []string{}
		out, err := logGrepMulti(file, []string{"level=ERROR", "level=WARN", "panic", "FAIL"})
		if err != nil {
			return "", err
		}
		matches = append(matches, out...)
		if len(matches) == 0 {
			return "No errors or warnings in " + file + ".", nil
		}
		return strings.Join(matches, "\n"), nil
	}
	return "Usage: tail <n> | grep <pattern> | errors", nil
}

func logTail(file string, n int) (string, error) {
	abs, err := safeLogsPath(file)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func logGrep(file, pattern string, _ time.Time) (string, error) {
	abs, err := safeLogsPath(file)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	plower := strings.ToLower(pattern)
	var hits []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(strings.ToLower(line), plower) {
			hits = append(hits, line)
			if len(hits) >= 100 {
				break
			}
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No matches for %q in %s.", pattern, file), nil
	}
	return strings.Join(hits, "\n"), nil
}

func logGrepMulti(file string, patterns []string) ([]string, error) {
	abs, err := safeLogsPath(file)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hits []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		for _, p := range patterns {
			if strings.Contains(line, p) {
				hits = append(hits, line)
				break
			}
		}
		if len(hits) >= 100 {
			break
		}
	}
	return hits, nil
}

// RegisterSelfEyes adds the read-code and read-logs skills to a registry.
// Called from cmd wiring.
func (r *SkillRegistryImpl) RegisterSelfEyes() {
	skills := []core.Skill{
		&selfReadCodeSkill{},
		&selfReadLogsSkill{},
	}
	for _, s := range skills {
		_ = r.Register(s)
		r.categories[s.Name()] = "Self-Awareness"
	}
}
