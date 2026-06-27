// Package llm implements LLM provider backends for Mini Krill.
// Like krill adapting to every ocean depth, this package adapts to every
// LLM backend - local Ollama, subscription CLIs, and API providers.
package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

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

	case "krill", "krilllm", "krill-lm", "krill_lm", "krill+ollama":
		// "krill+ollama" is the FailoverProvider's own Name(): a previous run that
		// switched to this stack persisted it to config, and on the next launch it
		// is fed straight back in here. Recognizing it keeps the persisted provider
		// name round-trip safe so startup never bricks with "unknown LLM provider".
		// Krill (the local engine on :57455) is primary; Ollama (a SMALL model,
		// so it can coexist in RAM with Krill's 12B on a 16GB box) is the
		// fallback. Krill endpoint/model are env-overridable (KRILLM_URL /
		// KRILL_MODEL), matching how Reef locates the same server.
		krillURL := envOr("KRILLM_URL", KrillDefaultURL)
		krillModel := cfg.Model
		if krillModel == "" || krillModel == "auto" {
			krillModel = envOr("KRILL_MODEL", KrillModelDefault)
		}
		primary := NewKrillProvider(krillURL, krillModel, cfg)

		oHost := ollamaCfg.Host
		if oHost == "" {
			oHost = "http://localhost:11434"
		}
		fbModel := ollamaCfg.FallbackModel
		if fbModel == "" {
			fbModel = "gemma4:e2b"
		}
		fallback := NewOllamaProvider(oHost, fbModel, cfg)
		log.Info("krill primary + ollama fallback", "krill", krillURL,
			"krill_model", krillModel, "fallback_model", fbModel)
		return NewFailoverProvider(primary, fallback), nil

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
