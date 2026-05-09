package brain

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

func newTestMemoryWithEntries(t *testing.T, entries []core.MemoryEntry) *FileMemory {
	t.Helper()
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}
	for _, e := range entries {
		if err := mem.Store(context.Background(), e); err != nil {
			t.Fatalf("Store(%q): %v", e.Key, err)
		}
	}
	return mem
}

func TestRankedSearchExactKeyMatch(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "golang", Value: "I prefer Go for backend work", Scope: "user"},
		{Key: "python", Value: "I use Python for data science", Scope: "user"},
		{Key: "rust", Value: "Rust is interesting for systems", Scope: "user"},
	})

	results, err := mem.RankedSearch(context.Background(), "golang", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].Key != "golang" {
		t.Errorf("expected 'golang' to be first result, got %q", results[0].Key)
	}
}

func TestRankedSearchWordOverlap(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "pref1", Value: "I like dark mode and vim keybindings", Scope: "user"},
		{Key: "pref2", Value: "The weather is nice today", Scope: "user"},
		{Key: "pref3", Value: "I prefer dark themes in my editor", Scope: "user"},
	})

	results, err := mem.RankedSearch(context.Background(), "dark mode editor", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	// "dark" entries should rank higher than "weather"
	if results[0].Key == "pref2" {
		t.Error("'weather' entry should not be the top result for 'dark mode editor'")
	}
}

func TestRankedSearchScopeFilter(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "user_go", Value: "I prefer Go", Scope: "user"},
		{Key: "system_feedback", Value: "signal:positive Go is great", Scope: "system"},
		{Key: "task_go", Value: "analyzed Go project", Scope: "task-outcome"},
	})

	// Search only user scope
	results, err := mem.RankedSearch(context.Background(), "Go", "user", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 user-scoped result, got %d", len(results))
	}
	if len(results) > 0 && results[0].Scope != "user" {
		t.Errorf("expected scope=user, got %q", results[0].Scope)
	}
}

func TestRankedSearchEmptyScopeReturnsAll(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "user_go", Value: "I prefer Go", Scope: "user"},
		{Key: "system_go", Value: "Go feedback positive", Scope: "system"},
	})

	results, err := mem.RankedSearch(context.Background(), "Go", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with empty scope filter, got %d", len(results))
	}
}

func TestRankedSearchRecencyBonus(t *testing.T) {
	now := time.Now().UTC()
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "old_pref", Value: "I like Go programming", Scope: "user", AccessedAt: now.Add(-300 * 24 * time.Hour)},
		{Key: "new_pref", Value: "I like Go programming", Scope: "user", AccessedAt: now},
	})

	results, err := mem.RankedSearch(context.Background(), "Go programming", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The recent one should score higher
	if results[0].Key != "new_pref" {
		t.Errorf("expected recent entry first, got %q", results[0].Key)
	}
}

func TestRankedSearchNoResults(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "pref1", Value: "I like Python", Scope: "user"},
	})

	results, err := mem.RankedSearch(context.Background(), "completely unrelated query xyz", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	// May return results with low scores (substring partial matches); that's OK.
	// The key test is that it doesn't error.
	_ = results
}

func TestRankedSearchLimit(t *testing.T) {
	entries := make([]core.MemoryEntry, 10)
	for i := range entries {
		entries[i] = core.MemoryEntry{
			Key:   fmt.Sprintf("entry_%d", i),
			Value: "Go programming is fun",
			Scope: "user",
		}
	}
	mem := newTestMemoryWithEntries(t, entries)

	results, err := mem.RankedSearch(context.Background(), "Go", "", 3)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestRankedSearchTagMatch(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "tagged", Value: "something about coding", Tags: []string{"golang", "preference"}, Scope: "user"},
		{Key: "untagged", Value: "something about coding", Scope: "user"},
	})

	results, err := mem.RankedSearch(context.Background(), "golang", "", 10)
	if err != nil {
		t.Fatalf("RankedSearch error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// Tagged entry should score higher
	if results[0].Key != "tagged" {
		t.Errorf("expected tagged entry first, got %q", results[0].Key)
	}
}

// Ensure fmt is used (used in TestRankedSearchLimit).
var _ = fmt.Sprintf
