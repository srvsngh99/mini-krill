package brain

import (
	"strings"
	"testing"
)

func TestBuildRecoveryContextEmpty(t *testing.T) {
	store := newTestStore(t)
	got := BuildRecoveryContext(store, "cli", 10)
	if got != "" {
		t.Errorf("expected empty string for empty DB, got %q", got)
	}
}

func TestBuildRecoveryContextNilStore(t *testing.T) {
	got := BuildRecoveryContext(nil, "cli", 10)
	if got != "" {
		t.Errorf("expected empty string for nil store, got %q", got)
	}
}

func TestBuildRecoveryContextFormatting(t *testing.T) {
	store := newTestStore(t)
	store.SaveTurn("cli", "user", "hello there")
	store.SaveTurn("cli", "assistant", "greetings!")
	store.SaveTurn("cli", "user", "what is krill?")

	got := BuildRecoveryContext(store, "cli", 10)

	if !strings.Contains(got, "User: hello there") {
		t.Errorf("missing user message, got:\n%s", got)
	}
	if !strings.Contains(got, "Krill: greetings!") {
		t.Errorf("missing krill message, got:\n%s", got)
	}
	if !strings.Contains(got, "User: what is krill?") {
		t.Errorf("missing second user message, got:\n%s", got)
	}
}

func TestBuildRecoveryContextTruncation(t *testing.T) {
	store := newTestStore(t)
	longMsg := strings.Repeat("x", 500)
	store.SaveTurn("cli", "user", longMsg)

	got := BuildRecoveryContext(store, "cli", 10)

	// Should be truncated to maxTurnPreview + "..."
	if strings.Contains(got, strings.Repeat("x", 400)) {
		t.Error("long message was not truncated")
	}
	if !strings.Contains(got, "...") {
		t.Error("truncated message missing ellipsis")
	}
}

func TestBuildEnrichedSystemPromptEmpty(t *testing.T) {
	base := "You are a krill."
	got := BuildEnrichedSystemPrompt(base, "")
	if got != base {
		t.Errorf("expected base prompt unchanged, got %q", got)
	}
}

func TestBuildEnrichedSystemPrompt(t *testing.T) {
	base := "You are a krill."
	recovery := "User: hi\nKrill: hello\n"
	got := BuildEnrichedSystemPrompt(base, recovery)

	if !strings.HasPrefix(got, base) {
		t.Error("enriched prompt should start with base prompt")
	}
	if !strings.Contains(got, "## Recent Conversation") {
		t.Error("missing recovery section header")
	}
	if !strings.Contains(got, recovery) {
		t.Error("missing recovery content")
	}
}
