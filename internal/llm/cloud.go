package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// CloudProvider implements core.LLMProvider for cloud-hosted LLM APIs.
// Supports OpenAI (and any OpenAI-compatible endpoint), Anthropic, and Google Gemini.
// Krill fact: krill migrate between surface and deep ocean daily - this provider
// migrates between cloud APIs with the same ease.
type CloudProvider struct {
	providerName string
	model        string
	apiKey       string
	baseURL      string
	temperature  float64
	maxTokens    int
	client       *http.Client
}

// NewCloudProvider creates a cloud LLM provider. providerName must be one of:
// "openai", "anthropic", "google".
func NewCloudProvider(providerName string, cfg config.LLMConfig) *CloudProvider {
	temp := cfg.Temperature
	if temp <= 0 {
		temp = 0.7
	}
	maxTok := cfg.MaxTokens
	if maxTok <= 0 {
		maxTok = 2048
	}

	return &CloudProvider{
		providerName: providerName,
		model:        cfg.Model,
		apiKey:       cfg.APIKey,
		baseURL:      cfg.BaseURL,
		temperature:  temp,
		maxTokens:    maxTok,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// LLMProvider interface implementation
// ---------------------------------------------------------------------------

// Chat dispatches to the appropriate cloud API based on provider name.
func (c *CloudProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	options := core.ApplyOptions(opts)

	model := c.model
	if options.Model != "" {
		model = options.Model
	}
	temp := c.temperature
	if options.Temperature != 0 {
		temp = options.Temperature
	}
	maxTok := c.maxTokens
	if options.MaxTokens != 0 {
		maxTok = options.MaxTokens
	}

	log.Debug("cloud chat request", "provider", c.providerName, "model", model, "messages", len(messages))

	switch c.providerName {
	case "openai":
		return c.chatOpenAI(ctx, messages, model, temp, maxTok, options.SystemPrompt)
	case "anthropic":
		return c.chatAnthropic(ctx, messages, model, temp, maxTok, options.SystemPrompt)
	case "google":
		return c.chatGoogle(ctx, messages, model, temp, maxTok, options.SystemPrompt)
	default:
		return nil, fmt.Errorf("unsupported cloud provider: %s", c.providerName)
	}
}

// Stream returns a channel that emits a single chunk with the full response.
// Full streaming support can be added later per-provider.
func (c *CloudProvider) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)

	go func() {
		defer close(ch)

		resp, err := c.Chat(ctx, messages, opts...)
		if err != nil {
			ch <- core.StreamChunk{Done: true, Err: err}
			return
		}
		ch <- core.StreamChunk{Content: resp.Content, Done: true}
	}()

	return ch, nil
}

// Name returns the provider name (openai, anthropic, google).
func (c *CloudProvider) Name() string { return c.providerName }

// ModelName returns the configured model name.
func (c *CloudProvider) ModelName() string { return c.model }

// Available returns true if an API key is configured.
// A lightweight probe could be added per-provider, but checking for a key
// avoids burning API quota on health checks.
func (c *CloudProvider) Available(_ context.Context) bool {
	return c.apiKey != ""
}

// ---------------------------------------------------------------------------
// OpenAI (and OpenAI-compatible APIs)
// ---------------------------------------------------------------------------

func (c *CloudProvider) chatOpenAI(ctx context.Context, messages []core.Message, model string, temp float64, maxTok int, systemPrompt string) (*core.Response, error) {
	base := c.baseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	base = strings.TrimRight(base, "/")
	url := base + "/v1/chat/completions"

	// Build messages with optional system prompt
	oaiMsgs := make([]map[string]string, 0, len(messages)+1)
	if systemPrompt != "" {
		oaiMsgs = append(oaiMsgs, map[string]string{"role": "system", "content": systemPrompt})
	}
	for _, m := range messages {
		oaiMsgs = append(oaiMsgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	reqBody := map[string]interface{}{
		"model":       model,
		"messages":    oaiMsgs,
		"temperature": temp,
		"max_tokens":  maxTok,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response - use a flexible struct to handle unexpected fields
	var oaiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	return &core.Response{
		Content:          oaiResp.Choices[0].Message.Content,
		Model:            model,
		PromptTokens:     oaiResp.Usage.PromptTokens,
		CompletionTokens: oaiResp.Usage.CompletionTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// Anthropic
// ---------------------------------------------------------------------------

func (c *CloudProvider) chatAnthropic(ctx context.Context, messages []core.Message, model string, temp float64, maxTok int, systemPrompt string) (*core.Response, error) {
	url := "https://api.anthropic.com/v1/messages"

	// Anthropic: system prompt goes in the top-level "system" field, not in messages.
	// Messages must alternate user/assistant, starting with user.
	anthropicMsgs := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		// Skip system messages - they go in the system field
		if m.Role == "system" {
			if systemPrompt == "" {
				systemPrompt = m.Content
			}
			continue
		}
		anthropicMsgs = append(anthropicMsgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	// Anthropic requires at least one message
	if len(anthropicMsgs) == 0 {
		return nil, fmt.Errorf("anthropic requires at least one non-system message")
	}

	reqBody := map[string]interface{}{
		"model":       model,
		"messages":    anthropicMsgs,
		"max_tokens":  maxTok,
		"temperature": temp,
	}
	if systemPrompt != "" {
		reqBody["system"] = systemPrompt
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	// Extract text from the first text content block
	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "text" || block.Text != "" {
			content = block.Text
			break
		}
	}

	return &core.Response{
		Content:          content,
		Model:            model,
		PromptTokens:     anthropicResp.Usage.InputTokens,
		CompletionTokens: anthropicResp.Usage.OutputTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// Google Gemini
// ---------------------------------------------------------------------------

func (c *CloudProvider) chatGoogle(ctx context.Context, messages []core.Message, model string, temp float64, maxTok int, systemPrompt string) (*core.Response, error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, c.apiKey,
	)

	// Build Gemini contents array
	// Gemini uses "user" and "model" roles (not "assistant")
	contents := make([]map[string]interface{}, 0, len(messages)+1)

	// Prepend system prompt as a user message if provided (Gemini handles
	// system instructions via systemInstruction field, but for simplicity
	// and broad compatibility we use the generationConfig approach)
	if systemPrompt != "" {
		contents = append(contents, map[string]interface{}{
			"role": "user",
			"parts": []map[string]string{
				{"text": systemPrompt},
			},
		})
		// Add a model acknowledgment to maintain alternating turns
		contents = append(contents, map[string]interface{}{
			"role": "model",
			"parts": []map[string]string{
				{"text": "Understood."},
			},
		})
	}

	for _, m := range messages {
		role := m.Role
		switch role {
		case "assistant":
			role = "model"
		case "system":
			// System messages already handled above; skip duplicates
			if systemPrompt != "" {
				continue
			}
			role = "user"
		}
		contents = append(contents, map[string]interface{}{
			"role": role,
			"parts": []map[string]string{
				{"text": m.Content},
			},
		})
	}

	reqBody := map[string]interface{}{
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"temperature":     temp,
			"maxOutputTokens": maxTok,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal google request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create google request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read google response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("decode google response: %w", err)
	}

	var content string
	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		content = geminiResp.Candidates[0].Content.Parts[0].Text
	}
	if content == "" && len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("google returned no candidates")
	}

	return &core.Response{
		Content:          content,
		Model:            model,
		PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
		CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
	}, nil
}
