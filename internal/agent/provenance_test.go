package agent

import (
	"strings"
	"testing"
)

func TestEnforceProvenance_KeepsHonestTag(t *testing.T) {
	log := newTurnFetchLog()
	log.Record("https://example.com/article")
	got, removed := EnforceProvenance("Stocks rallied today [web:example.com/article].", log)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if got != "Stocks rallied today [web:example.com/article]." {
		t.Fatalf("kept tag mangled response: %q", got)
	}
}

func TestEnforceProvenance_StripsFabricatedTag(t *testing.T) {
	log := newTurnFetchLog()
	got, removed := EnforceProvenance("Karpathy said X [web:youtube.com/abc].", log)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if !strings.Contains(got, "⚠") {
		t.Fatalf("expected warning prefix, got: %q", got)
	}
	if strings.Contains(got, "[web:") {
		t.Fatalf("fabricated tag should be stripped, got: %q", got)
	}
}

func TestEnforceProvenance_MixedTags(t *testing.T) {
	log := newTurnFetchLog()
	log.Record("https://real.com/x")
	got, removed := EnforceProvenance("A [web:real.com/x] and B [web:fake.com/y].", log)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if !strings.Contains(got, "[web:real.com/x]") {
		t.Fatalf("honest tag should remain, got: %q", got)
	}
	if strings.Contains(got, "fake.com") {
		t.Fatalf("fabricated tag should be gone, got: %q", got)
	}
}

func TestEnforceProvenance_NoTagsNoWarn(t *testing.T) {
	log := newTurnFetchLog()
	in := "Just a normal reply with no tags."
	got, removed := EnforceProvenance(in, log)
	if removed != 0 || got != in {
		t.Fatalf("expected passthrough, got %q removed=%d", got, removed)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://www.Example.com/path/": "example.com/path",
		"http://example.com":            "example.com",
		"www.example.com":               "example.com",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}
