package llm

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

func TestRenderCLIPrompt(t *testing.T) {
	got := renderCLIPrompt([]core.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Content: "default role"},
	}, "system prompt")

	for _, want := range []string{
		"System:\nsystem prompt",
		"User:\nhello",
		"Assistant:\nhi",
		"User:\ndefault role",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderCLIPrompt missing %q in:\n%s", want, got)
		}
	}
}

func TestRedactCLIError(t *testing.T) {
	input := strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
		"line 7",
	}, "\n")

	got := redactCLIError(input)
	if strings.Contains(got, "line 7") {
		t.Fatalf("redactCLIError kept too many lines: %q", got)
	}
	if !strings.Contains(got, "line 6") {
		t.Fatalf("redactCLIError dropped expected line: %q", got)
	}
}

func TestRedactCLIErrorTruncatesLongOutput(t *testing.T) {
	got := redactCLIError(strings.Repeat("x", 800))
	if len(got) > 703 {
		t.Fatalf("redactCLIError length = %d, want <= 703", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("redactCLIError should mark truncated output, got %q", got)
	}
}

func TestTitleRole(t *testing.T) {
	tests := map[string]string{
		"":          "User",
		"user":      "User",
		"assistant": "Assistant",
		"system":    "System",
	}
	for input, want := range tests {
		if got := titleRole(input); got != want {
			t.Fatalf("titleRole(%q) = %q, want %q", input, got, want)
		}
	}
}

// The runCLI tests below exercise the concurrent idle-monitor path
// (readers + single timer owner + Wait/drain ordering). Run them under
// `go test -race` to validate the data-race fixes from the PR review.

func skipIfNoShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("runCLI shell tests require a POSIX sh")
	}
}

// A subprocess that finishes quickly returns its stdout and no error.
func TestRunCLINormalExit(t *testing.T) {
	skipIfNoShell(t)
	out, err := runCLIWithIdle(context.Background(), "sh",
		[]string{"-c", "printf hello"}, "", time.Minute)
	if err != nil {
		t.Fatalf("runCLIWithIdle: unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("runCLIWithIdle out = %q, want %q", out, "hello")
	}
}

// A subprocess silent past the idle window is killed and reported as idle.
func TestRunCLIIdleKill(t *testing.T) {
	skipIfNoShell(t)
	start := time.Now()
	_, err := runCLIWithIdle(context.Background(), "sh",
		[]string{"-c", "sleep 5"}, "", 150*time.Millisecond)
	if err == nil {
		t.Fatal("runCLIWithIdle: expected idle-kill error, got nil")
	}
	if !strings.Contains(err.Error(), "idle for") {
		t.Fatalf("runCLIWithIdle error = %v, want idle-kill message", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("idle kill took %v, expected to fire near the 150ms window", elapsed)
	}
}

// A silent subprocess that forks a grandchild must still be reclaimed
// within the idle window. The compound command stops `sh` from
// exec-replacing into `sleep`, so `sleep` is a forked grandchild that
// inherits the pipe write ends. Killing only the direct child (the
// pre-fix behaviour) would orphan `sleep`, keep the pipes open, and hang
// <-done for the full 5s — the round-4 CI regression. The process-group
// kill must take the whole tree down promptly.
func TestRunCLIIdleKillReclaimsChildTree(t *testing.T) {
	skipIfNoShell(t)
	start := time.Now()
	_, err := runCLIWithIdle(context.Background(), "sh",
		[]string{"-c", "sleep 5; :"}, "", 150*time.Millisecond)
	if err == nil {
		t.Fatal("runCLIWithIdle: expected idle-kill error, got nil")
	}
	if !strings.Contains(err.Error(), "idle for") {
		t.Fatalf("runCLIWithIdle error = %v, want idle-kill message", err)
	}
	// Must fire near the 150ms window, not the grandchild's 5s lifetime.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("idle kill took %v — orphaned grandchild not reclaimed", elapsed)
	}
}

// A subprocess that keeps streaming output, each chunk within the idle
// window, must run to completion across many idle windows without being
// killed — the exact multi-step-plan scenario the PR targets.
func TestRunCLISlowStreamSurvives(t *testing.T) {
	skipIfNoShell(t)
	// 10 chunks ~50ms apart (~500ms total), idle window 200ms: every gap
	// is well under the window, so the timer should keep resetting.
	out, err := runCLIWithIdle(context.Background(), "sh",
		[]string{"-c", "for i in 1 2 3 4 5 6 7 8 9 10; do printf x; sleep 0.05; done"},
		"", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("runCLIWithIdle: slow stream should survive, got error: %v", err)
	}
	if string(out) != "xxxxxxxxxx" {
		t.Fatalf("runCLIWithIdle out = %q, want 10 x's", out)
	}
}

// Deliberate parent-context cancellation surfaces ctx.Err(), not a
// generic "<killed>" failure string.
func TestRunCLIParentCancel(t *testing.T) {
	skipIfNoShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := runCLIWithIdle(ctx, "sh", []string{"-c", "sleep 5"}, "", time.Minute)
	if err == nil {
		t.Fatal("runCLIWithIdle: expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCLIWithIdle error = %v, want errors.Is(context.Canceled)", err)
	}
}
