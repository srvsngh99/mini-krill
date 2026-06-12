// Package llm implements LLM provider backends for Mini Krill.
// Like krill adapting to every ocean depth, this package adapts to every
// LLM backend - local Ollama, subscription CLIs, and API providers.
package llm

import (
	"fmt"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// NewProvider creates the appropriate LLMProvider based on configuration.
func NewProvider(cfg config.LLMConfig, ollamaCfg config.OllamaConfig) (core.LLMProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))

	log.Info("initializing LLM provider", "provider", provider, "model", cfg.Model)

	switch provider {
	case "ollama":
		host := ollamaCfg.Host
		if host == "" {
			host = "http://localhost:11434"
		}
		model := cfg.Model
		if model == "" {
			model = ollamaCfg.DefaultModel
		}
		if model == "" {
			model = "llama3.2"
		}
		return NewOllamaProvider(host, model, cfg), nil

	case "krilllm", "krill-lm", "krill_lm":
		host := ollamaCfg.Host
		if host == "" {
			host = "http://localhost:11434"
		}
		return NewKrillLMProvider(host, cfg.Model, cfg), nil

	case "codex", "codex_cli", "codex-cli":
		return NewCLIProvider("codex", cfg), nil

	case "claude", "claude_code", "claude-code":
		return NewCLIProvider("claude", cfg), nil

	case "openai", "anthropic", "google":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("provider %q requires an API key (set llm.api_key or KRILL_LLM_API_KEY)", provider)
		}
		return NewCloudProvider(provider, cfg), nil

	default:
		return nil, fmt.Errorf("unknown LLM provider %q (supported: krilllm, ollama, codex, claude, openai, anthropic, google)", provider)
	}
}
