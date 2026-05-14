package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *ConversationStore {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "test_conversations.jsonl")
	store, err := NewConversationStore(storePath)
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
		t.Fatalf("expected 0 messages from empty store, got %d", len(msgs))
	}
}

func TestChannelIsolation(t *testing.T) {
	store := newTestStore(t)

	_ = store.SaveTurn("cli", "user", "cli message")
	_ = store.SaveTurn("telegram", "user", "telegram message")
	_ = store.SaveTurn("cli", "assistant", "cli response")

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
			_ = store.SaveTurn("cli", "user", "concurrent message")
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

// writeTurnsAtTimes seeds the store with raw timestamped JSONL entries so
// Search's since/until predicates can be exercised deterministically.
func writeTurnsAtTimes(t *testing.T, store *ConversationStore, turns []conversationTurn) {
	t.Helper()
	f, err := os.OpenFile(store.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, turn := range turns {
		data, err := json.Marshal(turn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearch_RespectsSinceAndUntilWindow(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	writeTurnsAtTimes(t, store, []conversationTurn{
		{Channel: "unified", Role: "user", Content: "two days ago", Timestamp: now.AddDate(0, 0, -2)},
		{Channel: "unified", Role: "user", Content: "yesterday msg", Timestamp: now.AddDate(0, 0, -1)},
		{Channel: "unified", Role: "user", Content: "today msg", Timestamp: now},
	})

	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startYesterday := startToday.AddDate(0, 0, -1)

	// "yesterday" window: since=startYesterday, until=startToday — must
	// exclude today.
	msgs, err := store.Search("unified", "", startYesterday, startToday, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "yesterday msg" {
		t.Errorf("expected only yesterday msg, got %v", msgs)
	}
}

func TestSearch_QueryFilter(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	writeTurnsAtTimes(t, store, []conversationTurn{
		{Channel: "unified", Role: "user", Content: "remind me about the planner", Timestamp: now},
		{Channel: "unified", Role: "user", Content: "check the deploy", Timestamp: now},
		{Channel: "unified", Role: "user", Content: "another planner question", Timestamp: now},
	})
	msgs, err := store.Search("unified", "planner", time.Time{}, time.Time{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 planner matches, got %d (%v)", len(msgs), msgs)
	}
	for _, m := range msgs {
		if !strings.Contains(strings.ToLower(m.Content), "planner") {
			t.Errorf("unexpected match: %q", m.Content)
		}
	}
}

func TestSearch_ChannelFilter(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	writeTurnsAtTimes(t, store, []conversationTurn{
		{Channel: "unified", Role: "user", Content: "in unified", Timestamp: now},
		{Channel: "tui", Role: "user", Content: "in tui", Timestamp: now},
	})
	msgs, err := store.Search("unified", "", time.Time{}, time.Time{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "in unified" {
		t.Errorf("expected only unified channel, got %v", msgs)
	}
}

func TestSearch_LimitTrailingWindow(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	turns := make([]conversationTurn, 0, 10)
	for i := 0; i < 10; i++ {
		turns = append(turns, conversationTurn{
			Channel:   "unified",
			Role:      "user",
			Content:   string(rune('a' + i)),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	writeTurnsAtTimes(t, store, turns)
	msgs, err := store.Search("unified", "", time.Time{}, time.Time{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	// Trailing 3: h, i, j
	if len(msgs) != 3 || msgs[0].Content != "h" || msgs[2].Content != "j" {
		t.Errorf("expected trailing [h,i,j], got %v", msgs)
	}
}
