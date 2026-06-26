//go:build colony

package brain

import (
	"context"
	"os"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// TestChromaMemoryLive exercises the Chroma-backed core.Memory against the real
// Chroma server + krillm embedder. Gated on SOCIALMEM_LIVE=1.
func TestChromaMemoryLive(t *testing.T) {
	if os.Getenv("SOCIALMEM_LIVE") != "1" {
		t.Skip("set SOCIALMEM_LIVE=1 to run the live Chroma memory test")
	}
	ctx := context.Background()
	m := NewChromaMemory("chromamem_test", 100)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(m.Store(ctx, core.MemoryEntry{Key: "fact:map", Value: "The capital map uses per-kind forged GLBs.", Scope: "project", Tags: []string{"map", "render"}}))
	must(m.Store(ctx, core.MemoryEntry{Key: "fact:ssd", Value: "The HF cache and Ollama models live on the external SSD.", Scope: "system"}))

	// Exact-key recall round-trips structured fields.
	got, err := m.Recall(ctx, "fact:map")
	must(err)
	if got == nil || got.Value == "" || got.Scope != "project" {
		t.Fatalf("recall mismatch: %+v", got)
	}

	// Semantic search finds the right memory by meaning.
	hits, err := m.Search(ctx, "how is the city rendered in 3D?", 3)
	must(err)
	if len(hits) == 0 || hits[0].Key != "fact:map" {
		t.Fatalf("expected fact:map first, got %+v", hits)
	}
	t.Logf("semantic top: %s = %q", hits[0].Key, hits[0].Value)

	// Scope-filtered ranked search.
	sys, err := m.RankedSearch(ctx, "where do models live", "system", 3)
	must(err)
	if len(sys) == 0 || sys[0].Scope != "system" {
		t.Fatalf("expected system-scoped hit, got %+v", sys)
	}

	if m.Count() < 2 {
		t.Fatalf("expected >=2 entries, got %d", m.Count())
	}
	must(m.Forget(ctx, "fact:ssd"))
	if g, _ := m.Recall(ctx, "fact:ssd"); g != nil {
		t.Fatal("expected fact:ssd forgotten")
	}

	// Clean up the test collection.
	_ = m.store.DropCollection(ctx)
}
