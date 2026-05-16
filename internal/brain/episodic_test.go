package brain

import (
	"context"
	"fmt"
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

// TestEpisodic_LatestSurvivesManyEpisodes is the regression for the round-1
// review: latestEntry used Search("episode_", 50), which returns the *oldest*
// 50 (ReadDir is lexical, episode_<unixMilli> is monotonic), so once history
// exceeded the scan limit Latest returned the newest-of-the-oldest-50 — stale
// — and the dedupe guard broke with it. The rolling episode:latest pointer
// makes "newest" an O(1) exact-key read, independent of history size, and
// pruning bounds the namespace.
func TestEpisodic_LatestSurvivesManyEpisodes(t *testing.T) {
	e, mem, store := newEpisodicFixture(t)
	ctx := context.Background()

	// Seed far more than the old scan limit, all old, lexically-ascending
	// keys. The pre-fix code would only ever see these.
	base := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 60; i++ {
		_ = mem.Store(ctx, core.MemoryEntry{
			Key:       fmt.Sprintf("episode_%013d", i),
			Value:     "stale session",
			Tags:      []string{episodeTag, "system"},
			Source:    "reflection",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}

	// A real, current session consolidates a fresh episode.
	for _, p := range [][2]string{
		{"user", "wire up the new exporter"},
		{"assistant", "drafted it"},
		{"user", "ship it"},
		{"assistant", "shipped, green"},
	} {
		_ = store.SaveTurn("cli", p[0], p[1])
	}
	ep, err := e.Consolidate(ctx, "cli")
	if err != nil || ep == nil {
		t.Fatalf("Consolidate err=%v ep=%v", err, ep)
	}

	// Latest must return the just-made episode, not one of the 60 stale ones.
	line, err := e.Latest(ctx, 7*24*time.Hour)
	if err != nil || line == "" {
		t.Fatalf("Latest err=%v line=%q", err, line)
	}
	if strings.Contains(line, "stale session") {
		t.Fatalf("Latest returned a stale episode despite >50 in history: %q", line)
	}
	if !strings.Contains(line, ep.Value) {
		t.Fatalf("Latest should surface the newest episode %q, got %q", ep.Value, line)
	}

	// The episode_* namespace must be bounded (pointer key is excluded).
	hist, _ := mem.Search(ctx, "episode_", 0)
	n := 0
	for _, en := range hist {
		if strings.HasPrefix(en.Key, "episode_") {
			n++
		}
	}
	if n > maxEpisodeHistory {
		t.Fatalf("history not pruned: %d > %d", n, maxEpisodeHistory)
	}
}

func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "moments"},
		{45 * time.Minute, "45m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.d); got != c.want {
			t.Errorf("humanizeAge(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
