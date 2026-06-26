//go:build colony

package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
	"github.com/srvsngh99/mini-krill/internal/socialmem"
)

// ChromaMemory is the COLONY default memory backend: the agent's entire memory
// lives in its OWN Chroma collection (embedded with the shared bge embedder),
// so every recall is semantic — "what did I learn / say about X" returns by
// meaning, not exact key. It satisfies core.Memory and is selected over
// FileMemory only in colony builds (see memory_default.go for the public one).
//
// One collection per agent ("<agent>") holds general memories AND social
// interactions (tagged by metadata kind), unifying recall across everything.
type ChromaMemory struct {
	store *socialmem.Store
	max   int
}

// NewChromaMemory builds a Chroma-backed memory for the agent. The collection is
// the agent id, so it is the same store the feed watcher records into — one mind,
// one collection.
func NewChromaMemory(agent string, max int) *ChromaMemory {
	s := socialmem.New(agent, socialmem.Config{Enabled: true, Collection: agent})
	return &ChromaMemory{store: s, max: max}
}

var _ core.Memory = (*ChromaMemory)(nil)

// Store upserts an entry. The structured fields are kept in metadata so List/
// Recall can faithfully reconstruct the core.MemoryEntry.
func (m *ChromaMemory) Store(ctx context.Context, entry core.MemoryEntry) error {
	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.AccessedAt.IsZero() {
		entry.AccessedAt = now
	}
	meta := map[string]any{
		"key":          entry.Key,
		"scope":        entry.Scope,
		"source":       entry.Source,
		"tags":         strings.Join(entry.Tags, ","),
		"access_count": entry.AccessCount,
		"created_at":   entry.CreatedAt.UnixMilli(),
		"accessed_at":  entry.AccessedAt.UnixMilli(),
		"kind":         "memory",
	}
	return m.store.Upsert(ctx, socialmem.Memory{ID: entry.Key, Text: entry.Value, Meta: meta})
}

// Recall returns the entry for an exact key, or (nil, nil) if absent.
func (m *ChromaMemory) Recall(ctx context.Context, key string) (*core.MemoryEntry, error) {
	rec, err := m.store.Get(ctx, key)
	if err != nil || rec == nil {
		return nil, err
	}
	e := entryFrom(rec.ID, rec.Text, rec.Meta)
	return &e, nil
}

// Search returns entries semantically similar to query (nearest first).
func (m *ChromaMemory) Search(ctx context.Context, query string, limit int) ([]core.MemoryEntry, error) {
	hits := m.store.Recall(ctx, query, limit)
	return hitsToEntries(hits), nil
}

// RankedSearch is semantic search optionally constrained to a scope.
func (m *ChromaMemory) RankedSearch(ctx context.Context, query, scope string, limit int) ([]core.MemoryEntry, error) {
	var where map[string]any
	if scope != "" {
		where = map[string]any{"scope": scope}
	}
	hits := m.store.RecallWhere(ctx, query, limit, where)
	return hitsToEntries(hits), nil
}

// Forget deletes an entry by key.
func (m *ChromaMemory) Forget(ctx context.Context, key string) error {
	return m.store.Delete(ctx, key)
}

// List returns all entries (bounded by max).
func (m *ChromaMemory) List(ctx context.Context) ([]core.MemoryEntry, error) {
	recs, err := m.store.All(ctx, m.max)
	if err != nil {
		return nil, err
	}
	out := make([]core.MemoryEntry, 0, len(recs))
	for _, r := range recs {
		out = append(out, entryFrom(r.ID, r.Text, r.Meta))
	}
	return out, nil
}

// Count returns the number of stored entries.
func (m *ChromaMemory) Count() int { return m.store.Count(context.Background()) }

// --- mapping helpers -------------------------------------------------------

func hitsToEntries(hits []socialmem.Hit) []core.MemoryEntry {
	out := make([]core.MemoryEntry, 0, len(hits))
	for _, h := range hits {
		id, _ := h.Meta["key"].(string)
		out = append(out, entryFrom(id, h.Text, h.Meta))
	}
	return out
}

// metaInt coerces a JSON metadata value (float64/int/int64) to int64.
func metaInt(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

// entryFrom reconstructs a core.MemoryEntry from a stored doc + metadata.
func entryFrom(id, text string, meta map[string]any) core.MemoryEntry {
	e := core.MemoryEntry{Key: id, Value: text}
	if meta == nil {
		return e
	}
	if k, ok := meta["key"].(string); ok && k != "" {
		e.Key = k
	}
	if s, ok := meta["scope"].(string); ok {
		e.Scope = s
	}
	if s, ok := meta["source"].(string); ok {
		e.Source = s
	}
	if t, ok := meta["tags"].(string); ok && t != "" {
		e.Tags = strings.Split(t, ",")
	}
	if n, ok := metaInt(meta, "access_count"); ok {
		e.AccessCount = int(n)
	}
	if ms, ok := metaInt(meta, "created_at"); ok && ms > 0 {
		e.CreatedAt = time.UnixMilli(ms)
	}
	if ms, ok := metaInt(meta, "accessed_at"); ok && ms > 0 {
		e.AccessedAt = time.UnixMilli(ms)
	}
	return e
}

// newDefaultMemory (colony) returns the Chroma-backed memory and migrates the
// legacy FileMemory store into it exactly once.
func newDefaultMemory(memDir string, cfg config.BrainConfig) (core.Memory, error) {
	cm := NewChromaMemory(reef.AgentID(), cfg.MaxMemories)
	migrateFileMemoryOnce(memDir, cfg, cm)
	return cm, nil
}

// migrateFileMemoryOnce copies any existing FileMemory entries into Chroma the
// first time a colony build runs, guarded by a marker file so it happens once.
func migrateFileMemoryOnce(memDir string, cfg config.BrainConfig, cm *ChromaMemory) {
	marker := filepath.Join(cfg.DataDir, ".chroma_migrated")
	if _, err := os.Stat(marker); err == nil {
		return // already migrated
	}
	fm, err := NewFileMemory(memDir, cfg.MaxMemories)
	if err != nil {
		return
	}
	entries, err := fm.List(context.Background())
	if err != nil {
		return
	}
	migrated := 0
	for _, e := range entries {
		if err := cm.Store(context.Background(), e); err == nil {
			migrated++
		}
	}
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
	klog.Info("migrated FileMemory -> Chroma", "entries", migrated, "of", len(entries))
}
