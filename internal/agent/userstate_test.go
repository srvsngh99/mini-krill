package agent

import (
	"context"
	"strings"
	"testing"
)

func TestLightMood(t *testing.T) {
	cases := map[string]string{
		"thanks, that's perfect":     "positive",
		"this is broken and useless": "negative",
		"run the parser on main.go":  "neutral",
		"":                           "neutral",
	}
	for in, want := range cases {
		if got := lightMood(in); got != want {
			t.Errorf("lightMood(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUserState_RoundTripAndContextLine(t *testing.T) {
	mem := newFakeMemory()
	ctx := context.Background()

	// Nothing stored yet → empty context line.
	if line := loadUserState(ctx, mem).contextLine(); line != "" {
		t.Fatalf("fresh user state should yield no context line, got %q", line)
	}

	updateUserState(ctx, mem, "thanks! now summarize https://example.com/post")

	us := loadUserState(ctx, mem)
	if us.LastMood != "positive" {
		t.Errorf("mood = %q, want positive", us.LastMood)
	}
	if us.LastCluster == "" || us.FocusArea == "" {
		t.Errorf("cluster/focus should be populated, got cluster=%q focus=%q", us.LastCluster, us.FocusArea)
	}
	if us.LastSeen.IsZero() {
		t.Error("LastSeen must be set")
	}

	line := loadUserState(ctx, mem).contextLine()
	if !strings.Contains(line, "read-only") || !strings.Contains(line, "mood=positive") {
		t.Fatalf("context line should be point-in-time framed and carry mood, got %q", line)
	}
}

// updateUserState must tolerate a nil memory (headless / test paths).
func TestUserState_NilMemorySafe(t *testing.T) {
	updateUserState(context.Background(), nil, "anything")
	if line := loadUserState(context.Background(), nil).contextLine(); line != "" {
		t.Fatalf("nil memory must yield empty context line, got %q", line)
	}
}
