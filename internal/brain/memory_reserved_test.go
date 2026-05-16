package brain

import (
	"context"
	"errors"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// The affinity:cluster: namespace is reserved. Only writes stamped with
// Source == core.AffinitySource (the affinity store itself) may land there;
// any other writer — most importantly a user typing
// `/remember affinity:cluster:abc = junk` — must be refused so a learned
// plan-affinity record can't be corrupted.
func TestStore_ReservedAffinityPrefix(t *testing.T) {
	dir := t.TempDir()
	mem, err := NewFileMemory(dir, 100)
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}
	ctx := context.Background()
	key := core.ReservedAffinityPrefix + "deadbeef"

	// 1. Non-affinity writer is refused with the sentinel.
	err = mem.Store(ctx, core.MemoryEntry{
		Key:    key,
		Value:  "junk",
		Source: "user-explicit",
	})
	if !errors.Is(err, core.ErrReservedKey) {
		t.Fatalf("user write to reserved key: got err=%v, want ErrReservedKey", err)
	}
	if got, _ := mem.Recall(ctx, key); got != nil {
		t.Fatalf("refused write must not persist; found %+v", got)
	}

	// 2. The affinity store's own writes are exempt.
	if err := mem.Store(ctx, core.MemoryEntry{
		Key:    key,
		Value:  `{"plan_score":0.8}`,
		Source: core.AffinitySource,
	}); err != nil {
		t.Fatalf("affinity-store write to reserved key should succeed, got %v", err)
	}
	got, err := mem.Recall(ctx, key)
	if err != nil || got == nil || got.Value != `{"plan_score":0.8}` {
		t.Fatalf("affinity write should persist; got=%+v err=%v", got, err)
	}

	// 3. A normal key with an arbitrary source is unaffected.
	if err := mem.Store(ctx, core.MemoryEntry{
		Key:    "pref:style_terse",
		Value:  "true",
		Source: "user-explicit",
	}); err != nil {
		t.Fatalf("normal key write should succeed, got %v", err)
	}
}
