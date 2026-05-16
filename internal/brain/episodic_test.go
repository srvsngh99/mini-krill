package brain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

func newEpisodicFixture(t *testing.T) (*Episodic, *FileMemory, *ConversationStore) {
	t.Helper()
	mem, err := NewFileMemory(t.TempDir(), 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}
	store, err := NewConversationStore(filepath.Join(t.TempDir(), "conv.jsonl"))
	if err != nil {
		t.Fatalf("NewConversationStore: %v", err)
	}
	return NewEpisodic(mem, store, &mockLLM{}), mem, store
}

func TestEpisodic_ConsolidateAndLatest(t *testing.T) {
	e, _, store := newEpisodicFixture(t)
	ctx := context.Background()

	// Thin session → no episode.
	_ = store.SaveTurn("cli", "user", "hi")
	if ep, err := e.Consolidate(ctx, "cli"); err != nil || ep != nil {
		t.Fatalf("thin session should not consolidate, got ep=%v err=%v", ep, err)
	}

	// A real session → one episode.
	for _, p := range [][2]string{
		{"user", "help me ship the parser"},
		{"assistant", "drafted a plan"},
		{"user", "run it"},
		{"assistant", "done, tests pass"},
	} {
		_ = store.SaveTurn("cli", p[0], p[1])
	}
	ep, err := e.Consolidate(ctx, "cli")
	if err != nil || ep == nil {
		t.Fatalf("real session should consolidate, got ep=%v err=%v", ep, err)
	}
	if !hasTag(ep.Tags, episodeTag) || ep.Value == "" {
		t.Fatalf("episode must be tagged and non-empty: %+v", ep)
	}

	// Dedupe: a second call inside the window must no-op.
	if ep2, err := e.Consolidate(ctx, "cli"); err != nil || ep2 != nil {
		t.Fatalf("dedupe window should suppress second episode, got ep=%v err=%v", ep2, err)
	}

	// Latest within maxAge returns the summary, point-in-time framed.
	line, err := e.Latest(ctx, 7*24*time.Hour)
	if err != nil || line == "" {
		t.Fatalf("Latest should return the fresh episode, got %q err=%v", line, err)
	}

	// Latest outside maxAge returns nothing.
	if line, _ := e.Latest(ctx, time.Nanosecond); line != "" {
		t.Fatalf("episode older than maxAge must be filtered, got %q", line)
	}
}

func TestEpisodic_DisabledIsNoOp(t *testing.T) {
	var e *Episodic // nil receiver
	if ep, err := e.Consolidate(context.Background(), "cli"); ep != nil || err != nil {
		t.Fatalf("nil Episodic must no-op, got ep=%v err=%v", ep, err)
	}
	e2 := NewEpisodic(nil, nil, nil)
	if line, err := e2.Latest(context.Background(), time.Hour); line != "" || err != nil {
		t.Fatalf("disabled Episodic.Latest must be empty, got %q err=%v", line, err)
	}
}

func TestEpisodic_LatestPicksNewest(t *testing.T) {
	e, mem, _ := newEpisodicFixture(t)
	ctx := context.Background()
	old := core.MemoryEntry{Key: "episode_1", Value: "old session", Tags: []string{episodeTag}, Source: "reflection", CreatedAt: time.Now().Add(-3 * time.Hour)}
	recent := core.MemoryEntry{Key: "episode_2", Value: "recent session", Tags: []string{episodeTag}, Source: "reflection", CreatedAt: time.Now().Add(-1 * time.Hour)}
	_ = mem.Store(ctx, old)
	_ = mem.Store(ctx, recent)
	line, err := e.Latest(ctx, 7*24*time.Hour)
	if err != nil || line == "" {
		t.Fatalf("Latest err=%v line=%q", err, line)
	}
	if !strings.Contains(line, "recent session") {
		t.Fatalf("Latest should pick the newest episode, got %q", line)
	}
}
