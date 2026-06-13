package llm

import (
	"github.com/srvsngh99/mini-krill/internal/config"
)

// KrillLMDefaultModel is the primary model for the KrillLM provider.
const KrillLMDefaultModel = config.DefaultKrillLMModel

// KrillLMProvider is Mini Krill's branded local provider: an Ollama-backed
// stack whose primary model is gemma 12b. It behaves exactly like the
// ollama provider but identifies as "krilllm" so switching, persistence,
// and the provider catalog treat it as a first-class target.
type KrillLMProvider struct {
	*OllamaProvider
}

// NewKrillLMProvider creates a KrillLM provider on the given Ollama host.
// An empty or "auto" model resolves to KrillLMDefaultModel.
func NewKrillLMProvider(host, model string, defaultOpts config.LLMConfig) *KrillLMProvider {
	if model == "" || model == "auto" {
		model = KrillLMDefaultModel
	}
	return &KrillLMProvider{OllamaProvider: NewOllamaProvider(host, model, defaultOpts)}
}

func (k *KrillLMProvider) Name() string { return "krilllm" }
