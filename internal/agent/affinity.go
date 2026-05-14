// Package agent — affinity.go maintains a per-cluster "should I plan this?"
// score that the agent updates from outcomes (approved / modified / rejected /
// ignored / thanked / fixed).
//
// The score is stored in the existing core.Memory under reserved keys
// `affinity:cluster:<id>`. After ~3 samples per cluster (minSamples) the agent
// can decide autonomously whether to plan-then-act, plan-in-parallel, or just
// act, with no user intervention.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// AffinityKeyPrefix reserves a memory key namespace so listing/searching the
// regular memory store skips affinity entries.
const AffinityKeyPrefix = "affinity:cluster:"

// AffinityOutcome is the discrete event types the store understands.
type AffinityOutcome string

const (
	OutcomeApproved AffinityOutcome = "approved"
	OutcomeModified AffinityOutcome = "modified"
	OutcomeRejected AffinityOutcome = "rejected"
	OutcomeIgnored  AffinityOutcome = "ignored"
	OutcomeThanked  AffinityOutcome = "thanked" // user happy after auto-execute → less planning
	OutcomeFixed    AffinityOutcome = "fixed"   // user corrected after auto-execute → more planning
)

// PlanDecision is what the affinity store recommends for a fresh request.
type PlanDecision string

const (
	DecisionPlanThenAct    PlanDecision = "plan_then_act"    // generate plan, show as FYI, auto-execute
	DecisionPlanInParallel PlanDecision = "plan_in_parallel" // generate plan, start execution in parallel
	DecisionJustAct        PlanDecision = "just_act"         // skip plan generation entirely
	DecisionUseLegacy      PlanDecision = "use_legacy"       // not enough samples, fall back to old gate
)

// minSamples is the calibration floor — below this the store won't recommend
// anything stronger than DecisionUseLegacy.
const minSamples = 3

// ClusterAffinity is the persisted record per cluster.
type ClusterAffinity struct {
	ClusterID    string    `json:"cluster_id"`
	Verb         string    `json:"verb"`
	ObjectClass  string    `json:"object_class"`
	PlanScore    float64   `json:"plan_score"` // [0.05, 0.95], starts at 0.5
	SampleCount  int       `json:"sample_count"`
	LastUpdate   time.Time `json:"last_update"`
	LastOutcomes []string  `json:"last_outcomes"` // ring buffer, max 10
	// WasAutoRun is sticky once true. Set when the cluster first crosses into
	// "auto-run" territory (calibrated AND PlanScore > 0.7). Update returns
	// true when this update flipped it false→true so callers can narrate the
	// transition exactly once. Without this, narration would either fire
	// every time or only at SampleCount==minSamples — neither catches the
	// real "we just stopped asking for approval" moment.
	WasAutoRun bool `json:"was_auto_run,omitempty"`
}

// isAutoRun returns whether the cluster currently qualifies for auto-run
// (score above the PlanThenAct threshold, calibrated). Pure function on the
// record fields so callers and tests can call it without state.
func (c ClusterAffinity) isAutoRun() bool {
	return c.SampleCount >= minSamples && c.PlanScore > 0.7
}

// AffinityStore wraps a core.Memory with affinity-typed accessors.
type AffinityStore struct {
	mem core.Memory
}

// NewAffinityStore wires the store to the existing memory backend. The mem
// store can be nil for tests; in that case all reads return zero values and
// all writes are no-ops.
func NewAffinityStore(mem core.Memory) *AffinityStore {
	return &AffinityStore{mem: mem}
}

// Get returns the affinity record for cluster id, or a fresh-default if not
// previously seen. The returned record is safe to mutate; call Put to persist.
func (s *AffinityStore) Get(ctx context.Context, c TaskCluster) ClusterAffinity {
	rec := ClusterAffinity{
		ClusterID:   c.ID,
		Verb:        c.Verb,
		ObjectClass: c.ObjectClass,
		PlanScore:   0.5,
		LastUpdate:  time.Now(),
	}
	if s.mem == nil {
		return rec
	}
	entry, err := s.mem.Recall(ctx, AffinityKeyPrefix+c.ID)
	if err != nil || entry == nil || entry.Value == "" {
		return rec
	}
	if err := json.Unmarshal([]byte(entry.Value), &rec); err != nil {
		log.Warn("affinity record corrupt, resetting", "cluster", c.ID, "error", err)
		return ClusterAffinity{
			ClusterID:   c.ID,
			Verb:        c.Verb,
			ObjectClass: c.ObjectClass,
			PlanScore:   0.5,
			LastUpdate:  time.Now(),
		}
	}
	return rec
}

// Put persists the affinity record.
func (s *AffinityStore) Put(ctx context.Context, rec ClusterAffinity) error {
	if s.mem == nil {
		return nil
	}
	rec.LastUpdate = time.Now()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.mem.Store(ctx, core.MemoryEntry{
		Key:        AffinityKeyPrefix + rec.ClusterID,
		Value:      string(data),
		Tags:       []string{"affinity", "system"},
		Scope:      "system",
		Source:     "affinity-store",
		CreatedAt:  rec.LastUpdate,
		AccessedAt: rec.LastUpdate,
	})
}

// Update applies an outcome to a cluster's score using a simple
// pull-toward-extreme rule, persists, and returns the post-update record plus
// a flag indicating whether this update flipped the cluster into auto-run
// territory for the first time (so callers can narrate the transition once).
//
//	APPROVED  → score += 0.10 * (1 - score)
//	MODIFIED  → score += 0.05 * (1 - score)
//	REJECTED  → score -= 0.20 * score
//	IGNORED   → score -= 0.15 * score
//	THANKED   → score -= 0.10 * score      (user happy without plan = plan was unneeded)
//	FIXED     → score += 0.15 * (1 - score) (user corrected after auto-act = plan would help)
//
// Result is clamped to [0.05, 0.95] so a cluster can always recover.
func (s *AffinityStore) Update(ctx context.Context, c TaskCluster, outcome AffinityOutcome) (ClusterAffinity, bool, error) {
	rec := s.Get(ctx, c)
	wasAutoRunBefore := rec.WasAutoRun
	score := rec.PlanScore
	switch outcome {
	case OutcomeApproved:
		score += 0.10 * (1 - score)
	case OutcomeModified:
		score += 0.05 * (1 - score)
	case OutcomeRejected:
		score -= 0.20 * score
	case OutcomeIgnored:
		score -= 0.15 * score
	case OutcomeThanked:
		score -= 0.10 * score
	case OutcomeFixed:
		score += 0.15 * (1 - score)
	default:
		return rec, false, fmt.Errorf("unknown affinity outcome: %s", outcome)
	}
	if score < 0.05 {
		score = 0.05
	} else if score > 0.95 {
		score = 0.95
	}
	rec.PlanScore = score
	rec.SampleCount++
	rec.LastOutcomes = appendRing(rec.LastOutcomes, string(outcome), 10)
	// Sticky transition: set WasAutoRun the first time the cluster qualifies.
	// Once set, stays set even if the score later dips below — the narration
	// is about "we just stopped asking", not "we are still not asking".
	if !rec.WasAutoRun && rec.isAutoRun() {
		rec.WasAutoRun = true
	}
	if err := s.Put(ctx, rec); err != nil {
		return rec, false, err
	}
	flippedToAutoRun := !wasAutoRunBefore && rec.WasAutoRun
	return rec, flippedToAutoRun, nil
}

// Decide returns the decision for a fresh request based on the cluster's
// current affinity. Below minSamples the store yields to the legacy gate so
// new clusters don't behave erratically.
func (s *AffinityStore) Decide(ctx context.Context, c TaskCluster) (PlanDecision, ClusterAffinity) {
	rec := s.Get(ctx, c)
	if rec.SampleCount < minSamples {
		return DecisionUseLegacy, rec
	}
	switch {
	case rec.PlanScore > 0.7:
		return DecisionPlanThenAct, rec
	case rec.PlanScore > 0.3:
		return DecisionPlanInParallel, rec
	default:
		return DecisionJustAct, rec
	}
}

// appendRing pushes v onto a ring buffer of size n, dropping the oldest entry.
func appendRing(ring []string, v string, n int) []string {
	ring = append(ring, v)
	if len(ring) > n {
		ring = ring[len(ring)-n:]
	}
	return ring
}

// SummaryFor returns a one-line human-readable description of why a decision
// was made. Used by the narration path so the agent can say "I'm now treating
// summarize/youtube as auto-run — you've approved 4 of those."
func (s *AffinityStore) SummaryFor(rec ClusterAffinity) string {
	if rec.SampleCount == 0 {
		return ""
	}
	approved, rejected := 0, 0
	for _, o := range rec.LastOutcomes {
		switch AffinityOutcome(o) {
		case OutcomeApproved, OutcomeModified, OutcomeThanked:
			approved++
		case OutcomeRejected, OutcomeIgnored:
			rejected++
		}
	}
	cluster := strings.Join([]string{rec.Verb, rec.ObjectClass}, "/")
	return fmt.Sprintf("cluster=%s score=%.2f samples=%d approved=%d rejected=%d",
		cluster, rec.PlanScore, rec.SampleCount, approved, rejected)
}
