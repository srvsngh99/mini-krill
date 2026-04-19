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
// Supports OpenAI (and compatible), Anthropic, and Google Gemini.
type CloudProvider struct {
	providerName string
	model        string
	apiKey       string
	baseURL      string
	temperature  float64
	maxTokens    int
	client       *http.Client
}

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
		client:       &http.Client{Timeout: 120 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// LLMProvider interface
// ---------------------------------------------------------------------------

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

func (c *CloudProvider) Name() string                    { return c.providerName }
func (c *CloudProvider) ModelName() string               { return c.model }
func (c *CloudProvider) Available(_ context.Context) bool { return c.apiKey != "" }

// ---------------------------------------------------------------------------
// Shared HTTP helper
// ---------------------------------------------------------------------------

// doJSON sends a JSON request and returns the raw response body.
// Handles marshalling, context, headers, and error status codes.
func (c *CloudProvider) doJSON(ctx context.Context, method, url string, headers map[string]string, reqBody interface{}) ([]byte, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Truncate response body in error to avoid leaking API keys
		errMsg := string(respBody)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errMsg)
	}

	return respBody, nil
}

// ---------------------------------------------------------------------------
// OpenAI
// ---------------------------------------------------------------------------

func (c *CloudProvider) chatOpenAI(ctx context.Context, messages []core.Message, model string, temp float64, maxTok int, systemPrompt string) (*core.Response, error) {
	base := c.baseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	url := strings.TrimRight(base, "/") + "/v1/chat/completions"

	oaiMsgs := make([]map[string]string, 0, len(messages)+1)
	if systemPrompt != "" {
		oaiMsgs = append(oaiMsgs, map[string]string{"role": "system", "content": systemPrompt})
	}
	for _, m := range messages {
		oaiMsgs = append(oaiMsgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	respBody, err := c.doJSON(ctx, http.MethodPost, url,
		map[string]string{"Authorization": "Bearer " + c.apiKey},
		map[string]interface{}{
			"model": model, "messages": oaiMsgs,
			"temperature": temp, "max_tokens": maxTok,
		})
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

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
		return nil, fmt.Errorf("openai decode: %w", err)
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
	anthropicMsgs := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			if systemPrompt == "" {
				systemPrompt = m.Content
			}
			continue
		}
		anthropicMsgs = append(anthropicMsgs, map[string]string{"role": m.Role, "content": m.Content})
	}
	if len(anthropicMsgs) == 0 {
		return nil, fmt.Errorf("anthropic requires at least one non-system message")
	}

	reqBody := map[string]interface{}{
		"model": model, "messages": anthropicMsgs,
		"max_tokens": maxTok, "temperature": temp,
	}
	if systemPrompt != "" {
		reqBody["system"] = systemPrompt
	}

	respBody, err := c.doJSON(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages",
		map[string]string{
			"x-api-key":         c.apiKey,
			"anthropic-version": "2023-06-01",
		}, reqBody)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
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
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}

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

	contents := make([]map[string]interface{}, 0, len(messages)+2)
	if systemPrompt != "" {
		contents = append(contents, map[string]interface{}{
			"role":  "user",
			"parts": []map[string]string{{"text": systemPrompt}},
		})
		contents = append(contents, map[string]interface{}{
			"role":  "model",
			"parts": []map[string]string{{"text": "Understood."}},
		})
	}

	for _, m := range messages {
		role := m.Role
		switch role {
		case "assistant":
			role = "model"
		case "system":
			if systemPrompt != "" {
				continue
			}
			role = "user"
		}
		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}

	respBody, err := c.doJSON(ctx, http.MethodPost, url, nil,
		map[string]interface{}{
			"contents": contents,
			"generationConfig": map[string]interface{}{
				"temperature": temp, "maxOutputTokens": maxTok,
			},
		})
	if err != nil {
		return nil, fmt.Errorf("google: %w", err)
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
		return nil, fmt.Errorf("google decode: %w", err)
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
