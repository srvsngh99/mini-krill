package llm

import (
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
)

// The krill/krilllm aliases now build a Krill-primary + Ollama-fallback stack.
func TestNewProviderKrillLM(t *testing.T) {
	for _, name := range []string{"krill", "krilllm", "krill-lm", "krill_lm"} {
		p, err := NewProvider(config.LLMConfig{Provider: name}, config.OllamaConfig{})
		if err != nil {
			t.Fatalf("NewProvider(%q) error: %v", name, err)
		}
		if p.Name() != "krill+ollama" {
			t.Errorf("NewProvider(%q).Name() = %q, want krill+ollama", name, p.Name())
		}
		// ModelName reflects the primary (Krill) model.
		if p.ModelName() != KrillModelDefault {
			t.Errorf("NewProvider(%q).ModelName() = %q, want %q", name, p.ModelName(), KrillModelDefault)
		}
	}
}

func TestKrillModelOverride(t *testing.T) {
	// An explicit model is honoured for the Krill primary.
	p, err := NewProvider(config.LLMConfig{Provider: "krill", Model: "gemma-4-26b-a4b-it-4bit"}, config.OllamaConfig{})
	if err != nil {
		t.Fatalf("NewProvider error: %v", err)
	}
	if p.ModelName() != "gemma-4-26b-a4b-it-4bit" {
		t.Errorf("ModelName() = %q, want gemma-4-26b-a4b-it-4bit", p.ModelName())
	}
	// "auto" resolves to the Krill default model.
	p, err = NewProvider(config.LLMConfig{Provider: "krill", Model: "auto"}, config.OllamaConfig{})
	if err != nil {
		t.Fatalf("NewProvider error: %v", err)
	}
	if p.ModelName() != KrillModelDefault {
		t.Errorf("ModelName() with auto = %q, want %q", p.ModelName(), KrillModelDefault)
	}
}

func TestResolveTargetKrillLM(t *testing.T) {
	m := NewProviderManager(&config.Config{}, NewOllamaProvider("http://127.0.0.1:1", "x", config.LLMConfig{}))
	for _, alias := range []string{"krilllm", "krill", "krill-lm"} {
		prov, model, ok := m.ResolveTarget(alias)
		if !ok || prov != "krilllm" || model != "" {
			t.Errorf("ResolveTarget(%q) = %q/%q/%v, want krilllm//true", alias, prov, model, ok)
		}
	}
	prov, model, ok := m.ResolveTarget("gemma12b")
	if !ok || prov != "krilllm" || model != KrillLMDefaultModel {
		t.Errorf("ResolveTarget(gemma12b) = %q/%q/%v, want krilllm/%s/true", prov, model, ok, KrillLMDefaultModel)
	}
}
