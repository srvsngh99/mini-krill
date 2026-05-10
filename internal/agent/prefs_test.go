package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// In-memory Memory mock — needed for tests that exercise typed preferences
// and feedback signals. The default mockBrain returns nil for Memory(), so
// pref-related code paths are bypassed and can't be tested with it.
// ---------------------------------------------------------------------------

type memBrain struct {
	mem core.Memory
}

func (b *memBrain) Memory() core.Memory                       { return b.mem }
func (b *memBrain) ConversationStore() core.ConversationStore { return nil }
func (b *memBrain) GetPersonality() *core.Personality         { return &core.Personality{Name: "TestKrill"} }
func (b *memBrain) GetSoul() *core.Soul {
	return &core.Soul{SystemPrompt: "You are a test krill.", Identity: "test"}
}
func (b *memBrain) SystemPrompt() string { return "You are a test krill." }
func (b *memBrain) RandomFact() string   { return "Krill are tiny." }
func (b *memBrain) EnrichMessages(msgs []core.Message) []core.Message {
	sysMsg := core.Message{Role: "system", Content: "You are a test krill."}
	if len(msgs) > 0 && msgs[0].Role == "system" {
		out := make([]core.Message, len(msgs))
		copy(out, msgs)
		out[0] = sysMsg
		return out
	}
	out := make([]core.Message, 0, len(msgs)+1)
	out = append(out, sysMsg)
	out = append(out, msgs...)
	return out
}

type fakeMemory struct {
	mu      sync.Mutex
	entries map[string]core.MemoryEntry
}

func newFakeMemory() *fakeMemory { return &fakeMemory{entries: map[string]core.MemoryEntry{}} }

func (m *fakeMemory) Store(_ context.Context, entry core.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.Key] = entry
	return nil
}
func (m *fakeMemory) Recall(_ context.Context, key string) (*core.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return nil, nil
	}
	return &e, nil
}
func (m *fakeMemory) Search(_ context.Context, query string, limit int) ([]core.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.MemoryEntry
	for _, e := range m.entries {
		if query == "" {
			out = append(out, e)
			continue
		}
		// substring match on value or any tag
		if strings.Contains(strings.ToLower(e.Value), strings.ToLower(query)) {
			out = append(out, e)
			continue
		}
		for _, t := range e.Tags {
			if strings.Contains(strings.ToLower(t), strings.ToLower(query)) {
				out = append(out, e)
				break
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *fakeMemory) RankedSearch(ctx context.Context, query, _ string, limit int) ([]core.MemoryEntry, error) {
	return m.Search(ctx, query, limit)
}
func (m *fakeMemory) Forget(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}
func (m *fakeMemory) List(_ context.Context) ([]core.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]core.MemoryEntry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e)
	}
	return out, nil
}
func (m *fakeMemory) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// ---------------------------------------------------------------------------
// detectTypedPreference
// ---------------------------------------------------------------------------

func TestDetectTypedPreference_NoApproval(t *testing.T) {
	cases := []string{
		"no need for approval",
		"No need for taking my approval on this kind of things",
		"Stop asking me to approve",
		"don't ask for approval",
		"skip approval",
		"auto approve",
		"just do it",
	}
	for _, c := range cases {
		key, val, matched := detectTypedPreference(c)
		if !matched {
			t.Errorf("detectTypedPreference(%q) = not matched, want matched", c)
			continue
		}
		if key != PrefNoPlanApproval {
			t.Errorf("detectTypedPreference(%q) key = %q, want %q", c, key, PrefNoPlanApproval)
		}
		if !val {
			t.Errorf("detectTypedPreference(%q) val = false, want true", c)
		}
	}
}

func TestDetectTypedPreference_RequireApproval(t *testing.T) {
	cases := []string{
		"ask me before doing things",
		"please ask first",
		"require approval",
		"check with me before deploying",
	}
	for _, c := range cases {
		key, val, matched := detectTypedPreference(c)
		if !matched {
			t.Errorf("detectTypedPreference(%q) = not matched, want matched", c)
			continue
		}
		if key != PrefNoPlanApproval {
			t.Errorf("detectTypedPreference(%q) key = %q, want %q", c, key, PrefNoPlanApproval)
		}
		if val {
			t.Errorf("detectTypedPreference(%q) val = true, want false", c)
		}
	}
}

func TestDetectTypedPreference_Style(t *testing.T) {
	terseInputs := []string{"be terse", "be more concise", "shorter responses please", "keep it short"}
	for _, c := range terseInputs {
		key, val, matched := detectTypedPreference(c)
		if !matched || key != PrefStyleTerse || !val {
			t.Errorf("detectTypedPreference(%q) → (%q, %v, %v); want (%q, true, true)",
				c, key, val, matched, PrefStyleTerse)
		}
	}

	noMetaInputs := []string{"no metaphors", "stop with the metaphors", "drop the metaphors"}
	for _, c := range noMetaInputs {
		key, val, matched := detectTypedPreference(c)
		if !matched || key != PrefNoMetaphors || !val {
			t.Errorf("detectTypedPreference(%q) → (%q, %v, %v); want (%q, true, true)",
				c, key, val, matched, PrefNoMetaphors)
		}
	}
}

func TestDetectTypedPreference_NoMatch(t *testing.T) {
	cases := []string{
		"hello",
		"what time is it",
		"build me a website",
		"give me some ideas",
		"",
	}
	for _, c := range cases {
		_, _, matched := detectTypedPreference(c)
		if matched {
			t.Errorf("detectTypedPreference(%q) unexpectedly matched", c)
		}
	}
}

// ---------------------------------------------------------------------------
// GetBoolPref / SetBoolPref round-trip
// ---------------------------------------------------------------------------

func TestBoolPrefRoundTrip(t *testing.T) {
	mem := newFakeMemory()
	ctx := context.Background()

	if v := GetBoolPref(ctx, mem, PrefNoPlanApproval, false); v != false {
		t.Errorf("default should be false, got true")
	}
	if v := GetBoolPref(ctx, mem, PrefNoPlanApproval, true); v != true {
		t.Errorf("default should be true when entry missing, got false")
	}

	SetBoolPref(ctx, mem, PrefNoPlanApproval, true)
	if v := GetBoolPref(ctx, mem, PrefNoPlanApproval, false); v != true {
		t.Errorf("after set true, GetBoolPref returned false")
	}

	SetBoolPref(ctx, mem, PrefNoPlanApproval, false)
	if v := GetBoolPref(ctx, mem, PrefNoPlanApproval, true); v != false {
		t.Errorf("after set false, GetBoolPref returned true")
	}
}

func TestBoolPrefNilMemory(t *testing.T) {
	ctx := context.Background()
	if v := GetBoolPref(ctx, nil, PrefNoPlanApproval, true); v != true {
		t.Errorf("nil memory should fall back to default")
	}
	// SetBoolPref with nil memory must not panic.
	SetBoolPref(ctx, nil, PrefNoPlanApproval, true)
}

// ---------------------------------------------------------------------------
// buildStyleDirective
// ---------------------------------------------------------------------------

func TestBuildStyleDirective_Empty(t *testing.T) {
	mem := newFakeMemory()
	if got := buildStyleDirective(context.Background(), mem); got != "" {
		t.Errorf("expected empty directive when no prefs set, got: %q", got)
	}
}

func TestBuildStyleDirective_Composes(t *testing.T) {
	mem := newFakeMemory()
	ctx := context.Background()
	SetBoolPref(ctx, mem, PrefStyleTerse, true)
	SetBoolPref(ctx, mem, PrefNoMetaphors, true)

	got := buildStyleDirective(ctx, mem)
	if got == "" {
		t.Fatal("expected non-empty directive")
	}
	if !strings.Contains(strings.ToLower(got), "short") {
		t.Errorf("expected terse directive, got: %s", got)
	}
	if !strings.Contains(strings.ToLower(got), "metaphor") {
		t.Errorf("expected metaphor directive, got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// shouldRequireApproval honours pref:no_plan_approval
// ---------------------------------------------------------------------------

func newAgentWithMemory(approval string, mem core.Memory) *KrillAgent {
	return New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: approval},
		&MockProvider{chatResponse: "CHAT"},
		&memBrain{mem: mem},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
}

func TestShouldRequireApproval_PrefSkipsForSafePlan(t *testing.T) {
	mem := newFakeMemory()
	SetBoolPref(context.Background(), mem, PrefNoPlanApproval, true)
	a := newAgentWithMemory("auto", mem)

	plan := &core.Plan{
		Task: "improve everything in the whole project", // would normally gate on "everything"
		Steps: []core.PlanStep{
			{ID: 1, Description: "analyze code"},
			{ID: 2, Description: "make improvements"},
		},
	}

	if a.shouldRequireApproval(plan) {
		t.Error("auto + pref:no_plan_approval should skip approval on non-destructive plan")
	}
}

func TestShouldRequireApproval_PrefDoesNotSkipDestructive(t *testing.T) {
	mem := newFakeMemory()
	SetBoolPref(context.Background(), mem, PrefNoPlanApproval, true)
	a := newAgentWithMemory("auto", mem)

	plan := &core.Plan{
		Task: "cleanup old branches",
		Steps: []core.PlanStep{
			{ID: 1, Description: "delete branch old-feature"},
		},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("destructive plan must still gate even with pref:no_plan_approval=true")
	}
}

func TestShouldRequireApproval_PrefDoesNotOverrideAlways(t *testing.T) {
	mem := newFakeMemory()
	SetBoolPref(context.Background(), mem, PrefNoPlanApproval, true)
	a := newAgentWithMemory("always", mem)

	plan := &core.Plan{
		Task:  "small task",
		Steps: []core.PlanStep{{ID: 1, Description: "do something"}},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("config 'always' must override the pref")
	}
}

// ---------------------------------------------------------------------------
// planIsDestructive
// ---------------------------------------------------------------------------

func TestPlanIsDestructive(t *testing.T) {
	cases := []struct {
		name string
		plan *core.Plan
		want bool
	}{
		{
			name: "rm -rf",
			plan: &core.Plan{Steps: []core.PlanStep{{Description: "rm -rf node_modules"}}},
			want: true,
		},
		{
			name: "git push",
			plan: &core.Plan{Steps: []core.PlanStep{{Description: "git push origin main"}}},
			want: true,
		},
		{
			name: "needs-approval flag",
			plan: &core.Plan{Steps: []core.PlanStep{{Description: "innocuous", NeedsApproval: true}}},
			want: true,
		},
		{
			name: "drop-down (false positive guard)",
			plan: &core.Plan{Steps: []core.PlanStep{{Description: "create a drop-down menu"}}},
			want: false,
		},
		{
			name: "summarize results",
			plan: &core.Plan{Steps: []core.PlanStep{{Description: "summarize the findings"}}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planIsDestructive(tc.plan); got != tc.want {
				t.Errorf("planIsDestructive(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Approval / rejection regex coverage
// ---------------------------------------------------------------------------

func TestApprovalRegexAcceptsCasualYes(t *testing.T) {
	approving := []string{"yes", "Yes", "YES", "y", "yea", "yeah", "yep", "yup",
		"sure", "ok", "okay", "alright", "alright!", "Sounds good", "lgtm", "go ahead",
		"do it", "proceed", "approve", "approved", "go", "fine", "👍"}
	for _, s := range approving {
		norm := strings.ToLower(strings.TrimSpace(s))
		if !approvalRegex.MatchString(norm) {
			t.Errorf("approvalRegex missed %q", s)
		}
	}
}

func TestRejectionRegexAcceptsCasualNo(t *testing.T) {
	rejecting := []string{"no", "No", "n", "nah", "nope", "cancel", "cancelled",
		"stop", "abort", "reject", "rejected", "skip", "nevermind", "never mind",
		"forget it", "don't", "dont"}
	for _, s := range rejecting {
		norm := strings.ToLower(strings.TrimSpace(s))
		if !rejectionRegex.MatchString(norm) {
			t.Errorf("rejectionRegex missed %q", s)
		}
	}
}

func TestApprovalRegexDoesNotMatchSentences(t *testing.T) {
	// Real sentences should fall through to LLM judging, not be auto-approved
	notApproval := []string{
		"yes but change step 3",
		"actually no",
		"can you make the plan smaller",
		"why did you choose go",
	}
	for _, s := range notApproval {
		norm := strings.ToLower(strings.TrimSpace(s))
		if approvalRegex.MatchString(norm) {
			t.Errorf("approvalRegex unexpectedly matched %q (should fall through)", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Pending-plan flow: MODIFY regenerates, UNRELATED releases control,
// "yea" approves (the original transcript bug)
// ---------------------------------------------------------------------------

func TestPendingPlan_YeaApproves(t *testing.T) {
	a := newTestAgent("execution result")
	a.pendingPlan = &core.Plan{
		Task:  "test task",
		Steps: []core.PlanStep{{ID: 1, Description: "step", Status: "pending"}},
	}

	resp, err := a.Chat(context.Background(), "yea")
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if a.pendingPlan != nil {
		t.Error("pending plan should be nil after 'yea' approval")
	}
	if resp == "" {
		t.Error("expected execution response")
	}
}

func TestPendingPlan_UnrelatedReleasesControl(t *testing.T) {
	// Sequential mock: first call is the LLM approval judge (returns UNRELATED),
	// second is the chat response after the input was re-routed.
	callCount := 0
	provider := &sequentialMockProvider{
		responses: []string{
			"UNRELATED",
			"hello back",
		},
		callCount: &callCount,
	}
	a := New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: "always"},
		provider,
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
	a.pendingPlan = &core.Plan{
		Task:  "old task",
		Steps: []core.PlanStep{{ID: 1, Description: "step", Status: "pending"}},
	}

	// "hi" is unambiguously unrelated to the pending plan; the LLM judge mock
	// returns UNRELATED so the agent should drop the plan and route normally.
	resp, err := a.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if a.pendingPlan != nil {
		t.Error("pending plan should be nil after UNRELATED reply (control released)")
	}
	if resp == "" {
		t.Error("expected a response after re-routing")
	}
	if strings.Contains(resp, "Approve this plan") {
		t.Errorf("UNRELATED reply must not loop the approval prompt, got: %s", resp)
	}
}

func TestPendingPlan_LearnsNoApprovalDuringPending(t *testing.T) {
	// Even while a plan is pending, "no need for approval" should be captured
	// so the next plan does not gate.
	mem := newFakeMemory()
	callCount := 0
	provider := &sequentialMockProvider{
		responses: []string{
			"UNRELATED", // approval judge for "no need for approval"
			"chat reply", // re-routed chat
		},
		callCount: &callCount,
	}
	a := New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: "auto"},
		provider,
		&memBrain{mem: mem},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
	a.pendingPlan = &core.Plan{
		Task:  "old task",
		Steps: []core.PlanStep{{ID: 1, Description: "step"}},
	}

	if _, err := a.Chat(context.Background(), "no need for approval on this kind of things"); err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	if !GetBoolPref(context.Background(), mem, PrefNoPlanApproval, false) {
		t.Error("pref:no_plan_approval should have been set during pending-plan state")
	}
}

// ---------------------------------------------------------------------------
// maybeAutoEvolveStyle: 3 corrections → terse mode
// ---------------------------------------------------------------------------

func TestMaybeAutoEvolveStyle_FlipsAfterThreshold(t *testing.T) {
	mem := newFakeMemory()
	a := newAgentWithMemory("auto", mem)
	ctx := context.Background()

	// Pre-populate two recent correction signals.
	for i := 0; i < 2; i++ {
		entry := core.MemoryEntry{
			Key:        "feedback_" + time.Now().Format(time.RFC3339Nano) + "_" + string(rune('a'+i)),
			Value:      "signal:correction | user: that's wrong | krill: ok",
			Tags:       []string{"personality-feedback", "correction"},
			Scope:      "system",
			CreatedAt:  time.Now(),
			AccessedAt: time.Now(),
		}
		_ = mem.Store(ctx, entry)
	}

	// One more correction triggers the threshold.
	a.maybeAutoEvolveStyle(ctx, "correction")

	if !GetBoolPref(ctx, mem, PrefStyleTerse, false) {
		t.Error("expected pref:style_terse to flip after 3 corrections")
	}
}

func TestMaybeAutoEvolveStyle_BelowThresholdDoesNothing(t *testing.T) {
	mem := newFakeMemory()
	a := newAgentWithMemory("auto", mem)
	ctx := context.Background()

	// One pre-existing correction + one fresh one = 2 (below threshold).
	entry := core.MemoryEntry{
		Key:        "feedback_x",
		Value:      "signal:correction",
		Tags:       []string{"personality-feedback", "correction"},
		Scope:      "system",
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	_ = mem.Store(ctx, entry)

	a.maybeAutoEvolveStyle(ctx, "correction")

	if GetBoolPref(ctx, mem, PrefStyleTerse, false) {
		t.Error("pref:style_terse should not flip below threshold")
	}
}

func TestMaybeAutoEvolveStyle_OldSignalsDontCount(t *testing.T) {
	mem := newFakeMemory()
	a := newAgentWithMemory("auto", mem)
	ctx := context.Background()

	// Two old corrections (>1h) plus one fresh — only the fresh one counts,
	// so we should be below threshold.
	for i := 0; i < 2; i++ {
		entry := core.MemoryEntry{
			Key:        "feedback_old_" + string(rune('a'+i)),
			Value:      "signal:correction",
			Tags:       []string{"personality-feedback", "correction"},
			Scope:      "system",
			CreatedAt:  time.Now().Add(-2 * time.Hour),
			AccessedAt: time.Now().Add(-2 * time.Hour),
		}
		_ = mem.Store(ctx, entry)
	}

	a.maybeAutoEvolveStyle(ctx, "correction")

	if GetBoolPref(ctx, mem, PrefStyleTerse, false) {
		t.Error("old corrections (>1h) should not contribute to threshold")
	}
}
