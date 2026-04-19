package brain

import (
	"context"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// Mock LLM provider for brain tests (no real LLM calls)
// ---------------------------------------------------------------------------

type mockLLM struct{}

func (m *mockLLM) Chat(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	return &core.Response{Content: "mock response", Model: "mock"}, nil
}

func (m *mockLLM) Stream(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	ch <- core.StreamChunk{Content: "mock", Done: true}
	close(ch)
	return ch, nil
}

func (m *mockLLM) Name() string                          { return "mock" }
func (m *mockLLM) ModelName() string                     { return "mock-model" }
func (m *mockLLM) Available(_ context.Context) bool      { return true }

// ---------------------------------------------------------------------------
// FileMemory tests
// ---------------------------------------------------------------------------

func TestFileMemoryStoreAndRecall(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	entry := core.MemoryEntry{
		Key:   "greeting",
		Value: "hello from the deep",
		Tags:  []string{"test"},
	}

	if err := mem.Store(ctx, entry); err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	recalled, err := mem.Recall(ctx, "greeting")
	if err != nil {
		t.Fatalf("Recall() error: %v", err)
	}
	if recalled == nil {
		t.Fatal("Recall() returned nil, want entry")
	}
	if recalled.Value != "hello from the deep" {
		t.Errorf("Recall().Value = %q, want %q", recalled.Value, "hello from the deep")
	}
	if recalled.Key != "greeting" {
		t.Errorf("Recall().Key = %q, want %q", recalled.Key, "greeting")
	}
}

func TestFileMemoryCount(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()

	if got := mem.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}

	_ = mem.Store(ctx, core.MemoryEntry{Key: "a", Value: "alpha"})
	_ = mem.Store(ctx, core.MemoryEntry{Key: "b", Value: "bravo"})
	_ = mem.Store(ctx, core.MemoryEntry{Key: "c", Value: "charlie"})

	if got := mem.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}

func TestFileMemoryList(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	_ = mem.Store(ctx, core.MemoryEntry{Key: "x", Value: "xray"})
	_ = mem.Store(ctx, core.MemoryEntry{Key: "y", Value: "yankee"})

	entries, err := mem.List(ctx)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("List() returned %d entries, want 2", len(entries))
	}
}

func TestFileMemoryForget(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	_ = mem.Store(ctx, core.MemoryEntry{Key: "temp", Value: "temporary"})

	if got := mem.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1 after store", got)
	}

	if err := mem.Forget(ctx, "temp"); err != nil {
		t.Fatalf("Forget() error: %v", err)
	}

	if got := mem.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0 after forget", got)
	}

	// Forget non-existent key should not error (idempotent)
	if err := mem.Forget(ctx, "nonexistent"); err != nil {
		t.Errorf("Forget(nonexistent) error: %v, want nil", err)
	}
}

func TestFileMemoryNotFound(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	recalled, err := mem.Recall(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Recall(nonexistent) error: %v, want nil error", err)
	}
	if recalled != nil {
		t.Errorf("Recall(nonexistent) = %+v, want nil", recalled)
	}
}

func TestFileMemorySearch(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	_ = mem.Store(ctx, core.MemoryEntry{Key: "ocean-depth", Value: "krill live at 600m depth"})
	_ = mem.Store(ctx, core.MemoryEntry{Key: "food-chain", Value: "whales eat krill"})
	_ = mem.Store(ctx, core.MemoryEntry{Key: "weather", Value: "sunny day today"})

	// Search for "krill" should match two entries
	results, err := mem.Search(ctx, "krill", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Search('krill') returned %d results, want 2", len(results))
	}

	// Search with limit
	results, err = mem.Search(ctx, "krill", 1)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search('krill', limit=1) returned %d results, want 1", len(results))
	}

	// Search for non-matching query
	results, err = mem.Search(ctx, "dinosaur", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search('dinosaur') returned %d results, want 0", len(results))
	}
}

func TestFileMemorySearchCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory() error: %v", err)
	}

	ctx := context.Background()
	_ = mem.Store(ctx, core.MemoryEntry{Key: "MyKey", Value: "Some Value"})

	results, err := mem.Search(ctx, "mykey", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("case-insensitive Search returned %d results, want 1", len(results))
	}
}

// ---------------------------------------------------------------------------
// Soul tests
// ---------------------------------------------------------------------------

func TestDefaultSoul(t *testing.T) {
	soul, personality, err := LoadSoul("")
	if err != nil {
		t.Fatalf("LoadSoul('') error: %v", err)
	}

	if soul.SystemPrompt == "" {
		t.Error("default soul has empty SystemPrompt")
	}
	if soul.Identity == "" {
		t.Error("default soul has empty Identity")
	}
	if len(soul.Values) == 0 {
		t.Error("default soul has no Values")
	}
	if len(soul.Boundaries) == 0 {
		t.Error("default soul has no Boundaries")
	}
	if personality.Name == "" {
		t.Error("default personality has empty Name")
	}
	if len(personality.Traits) == 0 {
		t.Error("default personality has no Traits")
	}
	if personality.Style == "" {
		t.Error("default personality has empty Style")
	}
	if len(personality.Quirks) == 0 {
		t.Error("default personality has no Quirks")
	}
	if personality.Greeting == "" {
		t.Error("default personality has empty Greeting")
	}
	if len(personality.KrillFacts) == 0 {
		t.Error("default personality has no KrillFacts")
	}
}

func TestLoadSoulNonexistentFile(t *testing.T) {
	soul, personality, err := LoadSoul("/nonexistent/soul.yaml")
	if err != nil {
		t.Fatalf("LoadSoul(nonexistent) error: %v, want fallback to defaults", err)
	}
	if soul.SystemPrompt == "" {
		t.Error("fallback soul has empty SystemPrompt")
	}
	if personality.Name == "" {
		t.Error("fallback personality has empty Name")
	}
}

// ---------------------------------------------------------------------------
// KrillBrain tests
// ---------------------------------------------------------------------------

func TestBrainNew(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.BrainConfig{
		DataDir:      tmpDir,
		MaxMemories:  100,
		HeartbeatSec: 10,
	}

	brain, err := New(cfg, &mockLLM{})
	if err != nil {
		t.Fatalf("brain.New() error: %v", err)
	}

	if brain.Memory() == nil {
		t.Error("brain.Memory() returned nil")
	}
	if brain.GetPersonality() == nil {
		t.Error("brain.GetPersonality() returned nil")
	}
	if brain.GetSoul() == nil {
		t.Error("brain.GetSoul() returned nil")
	}
	if brain.SystemPrompt() == "" {
		t.Error("brain.SystemPrompt() returned empty string")
	}
}

func TestBrainRandomFact(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.BrainConfig{
		DataDir:      tmpDir,
		MaxMemories:  100,
		HeartbeatSec: 10,
	}

	brain, err := New(cfg, &mockLLM{})
	if err != nil {
		t.Fatalf("brain.New() error: %v", err)
	}

	fact := brain.RandomFact()
	if fact == "" {
		t.Error("RandomFact() returned empty string")
	}

	// Verify it is one of the known krill facts
	found := false
	for _, kf := range core.KrillFacts {
		if kf == fact {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RandomFact() returned %q which is not in KrillFacts", fact)
	}
}

func TestBrainEnrichMessages(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.BrainConfig{
		DataDir:      tmpDir,
		MaxMemories:  100,
		HeartbeatSec: 10,
	}

	brain, err := New(cfg, &mockLLM{})
	if err != nil {
		t.Fatalf("brain.New() error: %v", err)
	}

	// Test prepending system prompt to messages without one
	msgs := []core.Message{
		{Role: "user", Content: "hello"},
	}
	enriched := brain.EnrichMessages(msgs)

	if len(enriched) != 2 {
		t.Fatalf("EnrichMessages() returned %d messages, want 2", len(enriched))
	}
	if enriched[0].Role != "system" {
		t.Errorf("enriched[0].Role = %q, want %q", enriched[0].Role, "system")
	}
	if enriched[0].Content != brain.SystemPrompt() {
		t.Error("enriched[0].Content does not match brain.SystemPrompt()")
	}
	if enriched[1].Content != "hello" {
		t.Errorf("enriched[1].Content = %q, want %q", enriched[1].Content, "hello")
	}

	// Test replacing existing system prompt
	msgsWithSys := []core.Message{
		{Role: "system", Content: "old prompt"},
		{Role: "user", Content: "hello"},
	}
	enriched2 := brain.EnrichMessages(msgsWithSys)

	if len(enriched2) != 2 {
		t.Fatalf("EnrichMessages(with system) returned %d messages, want 2", len(enriched2))
	}
	if enriched2[0].Content == "old prompt" {
		t.Error("EnrichMessages did not replace the existing system prompt")
	}
	if enriched2[0].Content != brain.SystemPrompt() {
		t.Error("enriched[0].Content does not match brain.SystemPrompt() after replacement")
	}
}

func TestBrainEnrichMessagesEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.BrainConfig{
		DataDir:      tmpDir,
		MaxMemories:  100,
		HeartbeatSec: 10,
	}

	brain, err := New(cfg, &mockLLM{})
	if err != nil {
		t.Fatalf("brain.New() error: %v", err)
	}

	enriched := brain.EnrichMessages(nil)
	if len(enriched) != 1 {
		t.Fatalf("EnrichMessages(nil) returned %d messages, want 1", len(enriched))
	}
	if enriched[0].Role != "system" {
		t.Errorf("enriched[0].Role = %q, want %q", enriched[0].Role, "system")
	}
}
