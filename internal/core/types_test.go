package core

import (
	"testing"
)

func TestApplyOptionsDefaults(t *testing.T) {
	opts := ApplyOptions(nil)
	if opts.Temperature != 0.7 {
		t.Errorf("default Temperature = %f, want 0.7", opts.Temperature)
	}
	if opts.MaxTokens != 2048 {
		t.Errorf("default MaxTokens = %d, want 2048", opts.MaxTokens)
	}
	if opts.Model != "" {
		t.Errorf("default Model = %q, want empty", opts.Model)
	}
	if opts.SystemPrompt != "" {
		t.Errorf("default SystemPrompt = %q, want empty", opts.SystemPrompt)
	}
}

func TestApplyOptionsOverrides(t *testing.T) {
	opts := ApplyOptions([]ChatOption{
		WithTemperature(0.3),
		WithMaxTokens(512),
		WithModel("test-model"),
		WithSystemPrompt("you are a krill"),
	})

	if opts.Temperature != 0.3 {
		t.Errorf("Temperature = %f, want 0.3", opts.Temperature)
	}
	if opts.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", opts.MaxTokens)
	}
	if opts.Model != "test-model" {
		t.Errorf("Model = %q, want %q", opts.Model, "test-model")
	}
	if opts.SystemPrompt != "you are a krill" {
		t.Errorf("SystemPrompt = %q, want %q", opts.SystemPrompt, "you are a krill")
	}
}

func TestApplyOptionsPartial(t *testing.T) {
	opts := ApplyOptions([]ChatOption{
		WithTemperature(0.9),
	})

	if opts.Temperature != 0.9 {
		t.Errorf("Temperature = %f, want 0.9", opts.Temperature)
	}
	// Non-overridden fields should keep defaults
	if opts.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048 (default)", opts.MaxTokens)
	}
}

func TestKrillFactsMinimumCount(t *testing.T) {
	if len(KrillFacts) < 10 {
		t.Errorf("KrillFacts has %d entries, want at least 10", len(KrillFacts))
	}
}

func TestKrillFactsNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(KrillFacts))
	for i, fact := range KrillFacts {
		if fact == "" {
			t.Errorf("KrillFacts[%d] is empty", i)
			continue
		}
		if seen[fact] {
			t.Errorf("duplicate krill fact at index %d: %q", i, fact)
		}
		seen[fact] = true
	}
}

func TestKrillFactsContent(t *testing.T) {
	for i, fact := range KrillFacts {
		if len(fact) < 10 {
			t.Errorf("KrillFacts[%d] is suspiciously short (%d chars): %q", i, len(fact), fact)
		}
	}
}
