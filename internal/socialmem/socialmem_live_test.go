//go:build colony

package socialmem

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestLive exercises Remember+Recall against the real Chroma server and krillm
// embedder. Gated on SOCIALMEM_LIVE=1 so it never runs in normal CI.
func TestLive(t *testing.T) {
	if os.Getenv("SOCIALMEM_LIVE") != "1" {
		t.Skip("set SOCIALMEM_LIVE=1 to run the live Chroma/embedder test")
	}
	ctx := context.Background()
	s := New("socialmem_test", Config{Enabled: true})

	s.Remember(ctx, Memory{ID: "c1", Text: "On Sourav's post about the new capital map renderer, I commented that the per-kind GLBs look sharp.", Meta: map[string]any{"kind": "comment", "ts": int64(1782000000000)}})
	s.Remember(ctx, Memory{ID: "c2", Text: "I posted in #labs: benchmark of gemma vs qwen on the routing task finished, qwen edged ahead.", Meta: map[string]any{"kind": "post", "ts": int64(1782100000000)}})
	s.Remember(ctx, Memory{ID: "c3", Text: "In a DM, the owner asked me to keep nightly experiments under 20 minutes.", Meta: map[string]any{"kind": "dm_in", "ts": int64(1782200000000)}})

	hits := s.Recall(ctx, "what did I say about the map rendering?", 3)
	if len(hits) == 0 {
		t.Fatal("expected recall hits, got none")
	}
	if !strings.Contains(strings.ToLower(hits[0].Text), "map") {
		t.Fatalf("expected map-related memory first, got: %q", hits[0].Text)
	}
	t.Logf("top hit (dist=%.4f): %s", hits[0].Distance, hits[0].Text)

	block := s.RecallBlock(ctx, "nightly experiment time limits")
	if !strings.Contains(strings.ToLower(block), "20 minutes") {
		t.Fatalf("expected DM memory in recall block, got: %q", block)
	}
	t.Logf("recall block:%s", block)

	// Clean up the test collection.
	collID, _ := s.collection(ctx)
	_ = s.deleteCollection(ctx)
	_ = collID
}

// deleteCollection removes the test collection so the live test leaves no trace.
func (s *Store) deleteCollection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.base()+"/collections/"+s.cfg.Collection, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
