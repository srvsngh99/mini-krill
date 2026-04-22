package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// Type aliases for the shared core types used by ProviderManager.
// ProviderManager implements core.ProviderControl.
type ProviderInfo = core.ProviderInfo
type ActiveInfo = core.ActiveInfo

// ProviderManager wraps a core.LLMProvider and adds runtime switching.
// It implements core.LLMProvider itself, so the rest of the codebase
// (agent, brain, skills) doesn't need to change.
type ProviderManager struct {
	mu      sync.RWMutex
	current core.LLMProvider
	cfg     *config.Config
}

// NewProviderManager wraps an existing provider with management capabilities.
func NewProviderManager(cfg *config.Config, initial core.LLMProvider) *ProviderManager {
	return &ProviderManager{
		current: initial,
		cfg:     cfg,
	}
}

// Current returns the active provider.
func (m *ProviderManager) Current() core.LLMProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// ── core.LLMProvider delegation ─────────────────────────────────

func (m *ProviderManager) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	return m.Current().Chat(ctx, messages, opts...)
}

func (m *ProviderManager) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	return m.Current().Stream(ctx, messages, opts...)
}

func (m *ProviderManager) Name() string      { return m.Current().Name() }
func (m *ProviderManager) ModelName() string  { return m.Current().ModelName() }

func (m *ProviderManager) Available(ctx context.Context) bool {
	return m.Current().Available(ctx)
}

// ── Management methods ──────────────────────────────────────────

// ActiveInfo returns the current provider name and model.
func (m *ProviderManager) ActiveInfo() ActiveInfo {
	p := m.Current()
	return ActiveInfo{
		Provider: p.Name(),
		Model:    p.ModelName(),
	}
}

// Switch changes the active LLM provider at runtime.
// provider is the provider name (ollama, openai, anthropic, google).
// model is optional - if empty, uses the provider's default.
func (m *ProviderManager) Switch(provider, model string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)

	// Build config for the new provider
	newCfg := m.cfg.LLM
	newCfg.Provider = provider
	if model != "" {
		newCfg.Model = model
	} else {
		newCfg.Model = "" // let NewProvider pick default
	}

	newProvider, err := NewProvider(newCfg, m.cfg.Ollama)
	if err != nil {
		return fmt.Errorf("switch to %s failed: %w", provider, err)
	}

	// Verify the new provider is reachable
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !newProvider.Available(ctx) {
		return fmt.Errorf("provider %s is not available (health check failed)", provider)
	}

	m.mu.Lock()
	old := m.current
	m.current = newProvider
	m.mu.Unlock()

	log.Info("switched LLM provider",
		"from", old.Name()+"/"+old.ModelName(),
		"to", newProvider.Name()+"/"+newProvider.ModelName(),
	)
	return nil
}

// ListProviders returns all known providers with their availability.
func (m *ProviderManager) ListProviders() []ProviderInfo {
	active := m.ActiveInfo()
	providers := []ProviderInfo{
		{
			Name:     "ollama",
			Models:   m.discoverOllamaModels(),
			IsActive: active.Provider == "ollama",
			NeedsKey: false,
			HasKey:   true,
		},
		{
			Name:     "openai",
			Models:   []string{"gpt-4o", "gpt-4o-mini", "gpt-4"},
			IsActive: active.Provider == "openai",
			NeedsKey: true,
			HasKey:   m.cfg.LLM.APIKey != "",
		},
		{
			Name:     "anthropic",
			Models:   []string{"claude-sonnet-4-20250514", "claude-haiku-4-5-20251001"},
			IsActive: active.Provider == "anthropic",
			NeedsKey: true,
			HasKey:   m.cfg.LLM.APIKey != "",
		},
		{
			Name:     "google",
			Models:   []string{"gemini-2.0-flash", "gemini-2.5-pro"},
			IsActive: active.Provider == "google",
			NeedsKey: true,
			HasKey:   m.cfg.LLM.APIKey != "",
		},
	}

	// Mark active model
	for i, p := range providers {
		if p.IsActive && active.Model != "" {
			// Ensure active model is in the list
			found := false
			for _, m := range p.Models {
				if m == active.Model {
					found = true
					break
				}
			}
			if !found {
				providers[i].Models = append([]string{active.Model}, p.Models...)
			}
		}
	}

	return providers
}

// discoverOllamaModels queries the Ollama API for available models.
func (m *ProviderManager) discoverOllamaModels() []string {
	host := m.cfg.Ollama.Host
	if host == "" {
		host = "http://localhost:11434"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		log.Debug("ollama model discovery failed", "error", err)
		// Return configured default
		if m.cfg.Ollama.DefaultModel != "" {
			return []string{m.cfg.Ollama.DefaultModel}
		}
		return []string{"llama3.2"}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []string{"llama3.2"}
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return []string{"llama3.2"}
	}

	var models []string
	for _, m := range tagsResp.Models {
		if !strings.Contains(strings.ToLower(m.Name), "embed") {
			models = append(models, m.Name)
		}
	}
	if len(models) == 0 {
		return []string{"llama3.2"}
	}
	return models
}

// ResolveTarget maps a user input like "ollama", "openai", "claude",
// "gemini", "gpt-4o" to a (provider, model) pair.
func (m *ProviderManager) ResolveTarget(input string) (provider, model string, ok bool) {
	input = strings.ToLower(strings.TrimSpace(input))

	// Provider aliases
	aliases := map[string]string{
		"ollama":    "ollama",
		"openai":    "openai",
		"anthropic": "anthropic",
		"claude":    "anthropic",
		"google":    "google",
		"gemini":    "google",
	}

	// Check if it's a provider name/alias
	if prov, found := aliases[input]; found {
		return prov, "", true
	}

	// Check if it's a model name - match to provider
	modelMap := map[string]string{
		"gpt-4o":      "openai",
		"gpt-4o-mini": "openai",
		"gpt-4":       "openai",
	}

	// Add Ollama models dynamically
	for _, mdl := range m.discoverOllamaModels() {
		short := strings.Split(mdl, ":")[0]
		modelMap[strings.ToLower(mdl)] = "ollama"
		modelMap[strings.ToLower(short)] = "ollama"
	}

	if prov, found := modelMap[input]; found {
		return prov, input, true
	}

	// Partial match: "sonnet" -> anthropic, "haiku" -> anthropic
	partialMap := map[string]struct{ prov, model string }{
		"sonnet": {"anthropic", "claude-sonnet-4-20250514"},
		"haiku":  {"anthropic", "claude-haiku-4-5-20251001"},
		"opus":   {"anthropic", "claude-opus-4-20250514"},
		"flash":  {"google", "gemini-2.0-flash"},
	}
	if pm, found := partialMap[input]; found {
		return pm.prov, pm.model, true
	}

	return "", "", false
}
