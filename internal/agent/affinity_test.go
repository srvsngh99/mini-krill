package agent

import (
	"context"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// fakeMem is a tiny in-memory Memory implementation for tests.
type fakeMem struct {
	store map[string]core.MemoryEntry
}

func newFakeMem() *fakeMem { return &fakeMem{store: map[string]core.MemoryEntry{}} }

func (m *fakeMem) Store(_ context.Context, e core.MemoryEntry) error {
	m.store[e.Key] = e
	return nil
}
func (m *fakeMem) Recall(_ context.Context, k string) (*core.MemoryEntry, error) {
	if e, ok := m.store[k]; ok {
		return &e, nil
	}
	return nil, nil
}
func (m *fakeMem) Search(_ context.Context, _ string, _ int) ([]core.MemoryEntry, error) {
	return nil, nil
}
func (m *fakeMem) RankedSearch(_ context.Context, _, _ string, _ int) ([]core.MemoryEntry, error) {
	return nil, nil
}
func (m *fakeMem) Forget(_ context.Context, k string) error { delete(m.store, k); return nil }
func (m *fakeMem) List(_ context.Context) ([]core.MemoryEntry, error) {
	out := make([]core.MemoryEntry, 0, len(m.store))
	for _, v := range m.store {
		out = append(out, v)
	}
	return out, nil
}
func (m *fakeMem) Count() int { return len(m.store) }

func TestAffinity_PullsTowardOneOnApproved(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("summarize this https://youtube.com/watch?v=abc")
	rec, _, err := store.Update(context.Background(), c, OutcomeApproved)
	if err != nil {
		t.Fatal(err)
	}
	if rec.PlanScore <= 0.5 {
		t.Fatalf("approved should pull score above 0.5, got %v", rec.PlanScore)
	}
}

func TestAffinity_PullsTowardZeroOnRejected(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("debug error: panic")
	rec, _, _ := store.Update(context.Background(), c, OutcomeRejected)
	if rec.PlanScore >= 0.5 {
		t.Fatalf("rejected should pull score below 0.5, got %v", rec.PlanScore)
	}
}

func TestAffinity_DecideUsesLegacyBelowMinSamples(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("hi")
	d, _ := store.Decide(context.Background(), c)
	if d != DecisionUseLegacy {
		t.Fatalf("brand new cluster should yield to legacy, got %v", d)
	}
}

func TestAffinity_DecideAfterSamples(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("summarize https://youtube.com/watch?v=abc")
	for i := 0; i < 5; i++ {
		_, _, _ = store.Update(context.Background(), c, OutcomeApproved)
	}
	d, rec := store.Decide(context.Background(), c)
	if d != DecisionPlanThenAct {
		t.Fatalf("five approvals should yield plan_then_act, got %v score=%v", d, rec.PlanScore)
	}
}

func TestAffinity_RingBufferCap(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("anything")
	for i := 0; i < 15; i++ {
		_, _, _ = store.Update(context.Background(), c, OutcomeApproved)
	}
	rec := store.Get(context.Background(), c)
	if len(rec.LastOutcomes) != 10 {
		t.Fatalf("ring buffer should cap at 10, got %d", len(rec.LastOutcomes))
	}
}

func TestAffinity_ClampsTo05To95(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("anything")
	for i := 0; i < 100; i++ {
		_, _, _ = store.Update(context.Background(), c, OutcomeApproved)
	}
	rec := store.Get(context.Background(), c)
	if rec.PlanScore > 0.95 {
		t.Fatalf("score should clamp at 0.95, got %v", rec.PlanScore)
	}
	for i := 0; i < 100; i++ {
		_, _, _ = store.Update(context.Background(), c, OutcomeRejected)
	}
	rec = store.Get(context.Background(), c)
	if rec.PlanScore < 0.05 {
		t.Fatalf("score should clamp at 0.05, got %v", rec.PlanScore)
	}
}

// TestAffinity_FlippedOnAutoRunCrossing covers the narration-correctness
// regression from review: previously narration only fired when SampleCount
// hit minSamples exactly, so a cluster that took 5 approvals to cross the 0.7
// threshold would slip silently into auto-run. Now the sticky WasAutoRun bit
// + the bool returned by Update give the caller exactly one chance to narrate.
func TestAffinity_FlippedOnAutoRunCrossing(t *testing.T) {
	store := NewAffinityStore(newFakeMem())
	c := ClusterFor("debug some error")

	// One MODIFIED then several APPROVED — the cluster will cross 0.7 later
	// than minSamples, exercising the path the old narration logic missed.
	flippedAtIdx := -1
	for i := 0; i < 10; i++ {
		out := OutcomeApproved
		if i == 0 {
			out = OutcomeModified
		}
		_, flipped, _ := store.Update(context.Background(), c, out)
		if flipped {
			if flippedAtIdx != -1 {
				t.Fatalf("Update returned flipped=true twice (once at %d and again at %d)", flippedAtIdx, i)
			}
			flippedAtIdx = i
		}
	}
	if flippedAtIdx == -1 {
		t.Fatal("expected exactly one flipped=true within 10 approvals; got none")
	}
	rec := store.Get(context.Background(), c)
	if !rec.WasAutoRun {
		t.Fatal("WasAutoRun should be sticky true after crossing the threshold")
	}
	// One more approval should NOT re-flip.
	_, flipped, _ := store.Update(context.Background(), c, OutcomeApproved)
	if flipped {
		t.Fatal("flipped should not be true on a subsequent update — the bit is sticky")
	}
}
