package agent

import (
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// validPrev is a realistic prior assistant turn: long enough and not a
// greeting, so looksLikeCorrection's guards don't short-circuit.
const validPrev = "Here is the summary you asked for about the project status today."

// #16: the negative correction trigger was asymmetric — it caught "don't"
// but not the other contracted negatives, so genuine corrections went
// uncredited. These must now all register as corrections.
func TestLooksLikeCorrection_ContractionAsymmetry(t *testing.T) {
	corrections := []string{
		"that doesn't work",
		"no it didn't",
		"that isn't right",
		"this wasn't what i meant",
		"that won't help",
		"that shouldn't happen",
		"that can't be right",
		"nope that doesn’t work", // curly apostrophe (iOS/Android)
		"don't",                  // original behaviour preserved
	}
	for _, c := range corrections {
		if !looksLikeCorrection(c, validPrev) {
			t.Errorf("looksLikeCorrection(%q) = false, want true", c)
		}
	}

	// Redirection, not correction: a forward verb after the negative means
	// "do X instead", which must NOT be counted as a correction.
	redirections := []string{
		"no just find me the digest",
		"nope, show me the other one",
	}
	for _, r := range redirections {
		if looksLikeCorrection(r, validPrev) {
			t.Errorf("looksLikeCorrection(%q) = true, want false (redirection)", r)
		}
	}

	// Guard intact: too-short prior assistant turn is never a correction.
	if looksLikeCorrection("that doesn't work", "ok") {
		t.Error("correction against a <30-char prior turn should be false")
	}
}

func TestPostTurnOutcome(t *testing.T) {
	if got := postTurnOutcome("thanks, that's perfect", validPrev); got != OutcomeThanked {
		t.Errorf("praise → %v, want THANKED", got)
	}
	if got := postTurnOutcome("can you also add a test?", validPrev); got != OutcomeThanked {
		t.Errorf("neutral follow-up → %v, want THANKED", got)
	}
	if got := postTurnOutcome("no that doesn't work", validPrev); got != OutcomeFixed {
		t.Errorf("correction → %v, want FIXED", got)
	}
}

// creditPostTurn must consume lastAutoRun exactly once: the next message is
// the verdict, and a later message must not re-credit a stale cluster.
func TestCreditPostTurn_ConsumesLastAutoRun(t *testing.T) {
	a := newTestAgent("CHAT")
	a.lastAutoRun = TaskCluster{ID: "abc123", Verb: "summarize", ObjectClass: "url"}
	a.history = []core.Message{{Role: "assistant", Content: validPrev}}

	a.creditPostTurn("thanks!")
	if a.lastAutoRun.ID != "" {
		t.Fatalf("lastAutoRun should be consumed, still %q", a.lastAutoRun.ID)
	}

	// No auto-run pending → must be a no-op (no panic, nothing to credit).
	a.creditPostTurn("and another thing")
	if a.lastAutoRun.ID != "" {
		t.Fatalf("creditPostTurn with no pending auto-run must stay empty")
	}
}
