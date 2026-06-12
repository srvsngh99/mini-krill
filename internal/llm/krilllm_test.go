package llm

import (
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
)

func TestNewProviderKrillLM(t *testing.T) {
	for _, name := range []string{"krilllm", "krill-lm", "krill_lm"} {
		p, err := NewProvider(config.LLMConfig{Provider: name}, config.OllamaConfig{})
		if err != nil {
			t.Fatalf("NewProvider(%q) error: %v", name, err)
		}
		if p.Name() != "krilllm" {
			t.Errorf("NewProvider(%q).Name() = %q, want krilllm", name, p.Name())
		}
		if p.ModelName() != KrillLMDefaultModel {
			t.Errorf("NewProvider(%q).ModelName() = %q, want %q", name, p.ModelName(), KrillLMDefaultModel)
		}
	}
}

func TestKrillLMModelOverride(t *testing.T) {
	p, err := NewProvider(config.LLMConfig{Provider: "krilllm", Model: "llama3.2:3b"}, config.OllamaConfig{})
	if err != nil {
		t.Fatalf("NewProvider error: %v", err)
	}
	if p.ModelName() != "llama3.2:3b" {
		t.Errorf("ModelName() = %q, want llama3.2:3b", p.ModelName())
	}
	// "auto" resolves to the primary model rather than being sent to Ollama verbatim.
	p, err = NewProvider(config.LLMConfig{Provider: "krilllm", Model: "auto"}, config.OllamaConfig{})
	if err != nil {
		t.Fatalf("NewProvider error: %v", err)
	}
	if p.ModelName() != KrillLMDefaultModel {
		t.Errorf("ModelName() with auto = %q, want %q", p.ModelName(), KrillLMDefaultModel)
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
