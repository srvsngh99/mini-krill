package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeRepoPath_RejectsEscape covers the load-bearing sandbox: nothing
// outside the repo root can be read, even via traversal tricks.
func TestSafeRepoPath_RejectsEscape(t *testing.T) {
	// Pin a known repo root so the test is hermetic.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIKRILL_REPO", tmp)

	cases := []string{
		"../../etc/passwd",
		"../outside",
		"/etc/passwd",      // absolute
		"/Users/elsewhere", // absolute
	}
	for _, rel := range cases {
		if _, err := safeRepoPath(rel); err == nil {
			t.Errorf("safeRepoPath(%q) should have rejected, got nil err", rel)
		}
	}
}

// TestSafeRepoPath_RejectsSecrets covers the secret allowlist defence-in-depth.
func TestSafeRepoPath_RejectsSecrets(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIKRILL_REPO", tmp)

	cases := []string{
		"config.yaml",
		"some/dir/.env",
		"keys/server.key",
		"creds/private_key.pem",
		"service_account.json",
	}
	for _, rel := range cases {
		if _, err := safeRepoPath(rel); err == nil {
			t.Errorf("safeRepoPath(%q) should be refused by secret allowlist", rel)
		}
	}
}

// TestSafeRepoPath_AcceptsCleanRelative confirms the happy path still works.
func TestSafeRepoPath_AcceptsCleanRelative(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "internal", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "internal", "agent", "agent.go"), []byte("package agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIKRILL_REPO", tmp)

	abs, err := safeRepoPath("internal/agent/agent.go")
	if err != nil {
		t.Fatalf("clean path should resolve, got err: %v", err)
	}
	if !strings.HasSuffix(abs, "internal/agent/agent.go") {
		t.Errorf("expected path to end at agent.go, got %q", abs)
	}
}

// TestSafeRepoPath_NoRoot returns an honest error when the source isn't
// reachable (e.g. installed via `go install` without setting MINIKRILL_REPO).
func TestSafeRepoPath_NoRoot(t *testing.T) {
	t.Setenv("MINIKRILL_REPO", "/nonexistent/path/that/should/not/exist")
	if _, err := safeRepoPath("anything"); err == nil {
		// Test only meaningful if the runtime.Caller-based fallback also
		// can't find go.mod. Skip if it does (developer running tests in repo).
		t.Skip("runtime fallback found a go.mod, skipping no-root case")
	}
}

// ---------------------------------------------------------------------------
// safeLogsPath
// ---------------------------------------------------------------------------

func TestSafeLogsPath_RejectsEscape(t *testing.T) {
	cases := []string{
		"../escape.log",
		"../../etc/passwd",
		"/absolute/path.log",
		"subdir/../../../escape",
	}
	for _, name := range cases {
		if _, err := safeLogsPath(name); err == nil {
			t.Errorf("safeLogsPath(%q) should have rejected, got nil err", name)
		}
	}
}

func TestSafeLogsPath_AcceptsBareFilename(t *testing.T) {
	abs, err := safeLogsPath("krill.log")
	if err != nil {
		t.Fatalf("bare filename should resolve, got err: %v", err)
	}
	if !strings.HasSuffix(abs, "krill.log") {
		t.Errorf("expected path to end at krill.log, got %q", abs)
	}
}
