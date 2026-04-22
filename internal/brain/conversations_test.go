package brain

import (
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *ConversationStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_conversations.db")
	store, err := NewConversationStore(dbPath)
	if err != nil {
		t.Fatalf("NewConversationStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveTurnAndLoadRecent(t *testing.T) {
	store := newTestStore(t)

	// Save 5 turns
	for i, pair := range [][2]string{
		{"user", "hello"},
		{"assistant", "hi there"},
		{"user", "how are you?"},
		{"assistant", "doing great!"},
		{"user", "bye"},
	} {
		if err := store.SaveTurn("cli", pair[0], pair[1]); err != nil {
			t.Fatalf("SaveTurn %d: %v", i, err)
		}
	}

	// Load last 3 — should be the final 3 in ASC order
	msgs, err := store.LoadRecent("cli", 3)
	if err != nil {
		t.Fatalf("LoadRecent: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Oldest of the 3 should be "how are you?"
	if msgs[0].Content != "how are you?" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "how are you?")
	}
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[2].Content != "bye" {
		t.Errorf("msgs[2].Content = %q, want %q", msgs[2].Content, "bye")
	}
}

func TestLoadRecentEmpty(t *testing.T) {
	store := newTestStore(t)

	msgs, err := store.LoadRecent("cli", 10)
	if err != nil {
		t.Fatalf("LoadRecent: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages from empty DB, got %d", len(msgs))
	}
}

func TestChannelIsolation(t *testing.T) {
	store := newTestStore(t)

	store.SaveTurn("cli", "user", "cli message")
	store.SaveTurn("telegram", "user", "telegram message")
	store.SaveTurn("cli", "assistant", "cli response")

	cliMsgs, _ := store.LoadRecent("cli", 10)
	tgMsgs, _ := store.LoadRecent("telegram", 10)

	if len(cliMsgs) != 2 {
		t.Errorf("cli: expected 2 messages, got %d", len(cliMsgs))
	}
	if len(tgMsgs) != 1 {
		t.Errorf("telegram: expected 1 message, got %d", len(tgMsgs))
	}
	if tgMsgs[0].Content != "telegram message" {
		t.Errorf("telegram msg content = %q, want %q", tgMsgs[0].Content, "telegram message")
	}
}

func TestConcurrentSaves(t *testing.T) {
	store := newTestStore(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store.SaveTurn("cli", "user", "concurrent message")
		}(i)
	}
	wg.Wait()

	msgs, err := store.LoadRecent("cli", 100)
	if err != nil {
		t.Fatalf("LoadRecent: %v", err)
	}
	if len(msgs) != 50 {
		t.Errorf("expected 50 messages, got %d", len(msgs))
	}
}
