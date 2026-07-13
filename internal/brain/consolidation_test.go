package brain

import (
	"context"
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

func TestConsolidateNoDuplicates(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "a", Value: "completely different topic one", Scope: "user"},
		{Key: "b", Value: "unrelated subject matter here", Scope: "user"},
	})

	c := NewConsolidator(mem, nil)
	merged, removed, err := c.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("Consolidate error: %v", err)
	}
	if merged != 0 || removed != 0 {
		t.Errorf("expected no consolidation, got merged=%d removed=%d", merged, removed)
	}
}

func TestConsolidateDuplicates(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "pref1", Value: "I prefer Go for backend development work", Scope: "user", Tags: []string{"preference"}},
		{Key: "pref2", Value: "I prefer Go for backend services and APIs", Scope: "user", Tags: []string{"preference"}},
		{Key: "pref3", Value: "I like dark mode and vim", Scope: "user", Tags: []string{"ui"}},
	})

	c := NewConsolidator(mem, nil)
	merged, removed, err := c.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("Consolidate error: %v", err)
	}

	// The two Go-related entries should be merged
	if merged == 0 {
		t.Error("expected at least 1 merge")
	}
	if removed < 2 {
		t.Errorf("expected at least 2 entries removed, got %d", removed)
	}

	// Verify remaining entries
	entries, _ := mem.List(context.Background())
	// Should have the merged entry + the dark mode entry
	if len(entries) > 3 {
		t.Errorf("expected fewer entries after consolidation, got %d", len(entries))
	}
}

func TestConsolidateCrossScope(t *testing.T) {
	mem := newTestMemoryWithEntries(t, []core.MemoryEntry{
		{Key: "user_go", Value: "I prefer Go for backend development work", Scope: "user"},
		{Key: "sys_go", Value: "I prefer Go for backend development work", Scope: "system"},
	})

	c := NewConsolidator(mem, nil)
	merged, _, err := c.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("Consolidate error: %v", err)
	}
	// Different scopes should NOT be merged
	if merged != 0 {
		t.Errorf("cross-scope entries should not be merged, got merged=%d", merged)
	}
}

func TestConsolidateMergeTags(t *testing.T) {
	tags := mergeTags([]core.MemoryEntry{
		{Tags: []string{"a", "b"}},
		{Tags: []string{"b", "c"}},
		{Tags: []string{"d"}},
	})

	if len(tags) != 4 {
		t.Errorf("expected 4 unique tags, got %d: %v", len(tags), tags)
	}
}

func TestWordOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
	}{
		{"I like Go", "I like Go", 1.0},
		{"I like Go", "I prefer Python", 0.1}, // "I" is shared
		{"completely different", "no match here", 0.0},
		{"Go backend development", "Go backend services", 0.5},
	}
	for _, tc := range cases {
		got := wordOverlap(tc.a, tc.b)
		if got < tc.min {
			t.Errorf("wordOverlap(%q, %q) = %f, want >= %f", tc.a, tc.b, got, tc.min)
		}
	}
}

func TestReflectOnTaskNoLLM(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}

	c := NewConsolidator(mem, nil)
	err = c.ReflectOnTask(context.Background(), "Files found: main.go, go.mod, README.md", "check the repo")
	if err != nil {
		t.Fatalf("ReflectOnTask error: %v", err)
	}

	// Verify a reflection was stored
	entries, _ := mem.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("expected 1 reflection entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Scope != "task-outcome" {
		t.Errorf("scope = %q, want 'task-outcome'", entry.Scope)
	}
	if entry.Source != "reflection" {
		t.Errorf("source = %q, want 'reflection'", entry.Source)
	}
	if !strings.Contains(entry.Value, "check the repo") {
		t.Errorf("reflection should contain task description, got: %s", entry.Value)
	}
}

func TestStoreDefaultScope(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}

	// Store without scope should default to "system"
	err = mem.Store(context.Background(), core.MemoryEntry{
		Key:   "test",
		Value: "test value",
	})
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}

	entry, err := mem.Recall(context.Background(), "test")
	if err != nil {
		t.Fatalf("Recall error: %v", err)
	}
	if entry.Scope != "system" {
		t.Errorf("default scope = %q, want 'system'", entry.Scope)
	}
}

func TestRecallIncrementsAccessCount(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}

	err = mem.Store(context.Background(), core.MemoryEntry{
		Key:   "test",
		Value: "test value",
	})
	if err != nil {
		t.Fatalf("Store error: %v", err)
	}

	// First recall
	entry, _ := mem.Recall(context.Background(), "test")
	if entry.AccessCount != 1 {
		t.Errorf("AccessCount after 1 recall = %d, want 1", entry.AccessCount)
	}

	// Second recall
	entry, _ = mem.Recall(context.Background(), "test")
	if entry.AccessCount != 2 {
		t.Errorf("AccessCount after 2 recalls = %d, want 2", entry.AccessCount)
	}
}
