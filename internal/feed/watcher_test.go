//go:build colony

package feed

import (
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/reef"
)

func TestParseAppraisal(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantAct string
		wantInt float64
		wantErr bool
	}{
		{"plain", `{"interest":0.8,"action":"comment","draft":"hi","why":"x"}`, "comment", 0.8, false},
		{"fenced", "```json\n{\"interest\":0.2,\"action\":\"IGNORE\",\"draft\":\"\",\"why\":\"meh\"}\n```", "ignore", 0.2, false},
		{"prose-wrapped", `Sure! {"interest":0.6,"action":"Like"} hope that helps`, "like", 0.6, false},
		{"no-json", `I don't think I should engage here.`, "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := parseAppraisal(c.raw)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", a)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Action != c.wantAct {
				t.Errorf("action: got %q want %q", a.Action, c.wantAct)
			}
			if a.Interest != c.wantInt {
				t.Errorf("interest: got %v want %v", a.Interest, c.wantInt)
			}
		})
	}
}

// A chain owner -> minikrill -> labkrill -> minikrill should read as depth 2 of
// agent<->agent above the newest comment, and an owner reply must reset it.
func TestAgentChainDepth(t *testing.T) {
	comments := []reef.Comment{
		{ID: "c1", Author: "owner", ParentCommentID: ""},
		{ID: "c2", Author: "minikrill", ParentCommentID: "c1"},
		{ID: "c3", Author: "labkrill", ParentCommentID: "c2"},
		{ID: "c4", Author: "minikrill", ParentCommentID: "c3"},
	}
	byID := map[string]reef.Comment{}
	for _, c := range comments {
		byID[c.ID] = c
	}

	// Considering a reply to c4 (labkrill replying): above c4 are c3(lab),
	// c2(mini) — 2 agent levels, then c1 is owner which stops the count.
	if got := agentChainDepth(byID["c4"], byID, "labkrill"); got != 2 {
		t.Errorf("depth at c4: got %d want 2", got)
	}
	// Considering a reply to c2: above it is only owner -> depth 0.
	if got := agentChainDepth(byID["c2"], byID, "labkrill"); got != 0 {
		t.Errorf("depth at c2: got %d want 0", got)
	}

	// Owner stepping in mid-thread resets depth: owner reply under c4.
	byID["c5"] = reef.Comment{ID: "c5", Author: "owner", ParentCommentID: "c4"}
	byID["c6"] = reef.Comment{ID: "c6", Author: "labkrill", ParentCommentID: "c5"}
	if got := agentChainDepth(byID["c6"], byID, "minikrill"); got != 0 {
		t.Errorf("owner should reset depth: got %d want 0", got)
	}
}

// A cyclic parent chain from malformed hub data must not hang the walk.
func TestAgentChainDepthCycle(t *testing.T) {
	byID := map[string]reef.Comment{
		"a": {ID: "a", Author: "labkrill", ParentCommentID: "b"},
		"b": {ID: "b", Author: "minikrill", ParentCommentID: "a"},
	}
	done := make(chan int, 1)
	go func() { done <- agentChainDepth(byID["a"], byID, "minikrill") }()
	select {
	case <-done: // returned (any finite value) = guard works
	case <-time.After(2 * time.Second):
		t.Fatal("agentChainDepth hung on a cyclic parent chain")
	}
}

func TestBudgetCapsPerHour(t *testing.T) {
	b := &budget{perHour: 2}
	if !b.allow() {
		t.Fatal("fresh budget should allow")
	}
	b.record()
	b.record()
	if b.allow() {
		t.Fatal("budget should be exhausted at the cap")
	}
	// An old engagement outside the window must not count against the cap.
	b.window[0] = time.Now().Add(-2 * time.Hour)
	if !b.allow() {
		t.Fatal("stale engagement should have aged out of the window")
	}
}

func TestMentionsMe(t *testing.T) {
	w := &FeedWatcher{me: "minikrill"}
	for _, s := range []string{"hey @minikrill look", "cc @MiniKrill", "@minikrill"} {
		if !w.mentionsMe(s) {
			t.Errorf("should detect mention in %q", s)
		}
	}
	for _, s := range []string{"hey @labkrill", "minikrill without at", "@mini"} {
		if w.mentionsMe(s) {
			t.Errorf("should NOT detect mention in %q", s)
		}
	}
}

func TestIsOwner(t *testing.T) {
	for _, a := range []string{"owner", "sourav"} {
		if !isOwner(a) {
			t.Errorf("%q should be owner", a)
		}
	}
	if isOwner("minikrill") {
		t.Error("minikrill is not owner")
	}
}
