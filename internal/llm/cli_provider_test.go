package llm

import (
	"strings"
	"testing"

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
