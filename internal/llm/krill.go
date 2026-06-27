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

// Krill is Sourav's own local inference engine (`krillm serve`), exposed over an
// OpenAI-compatible API. It is the colony's primary local backend; Ollama is the
// fallback. (Distinct from Ollama - do not confuse the two.)
const (
	KrillDefaultURL   = "http://127.0.0.1:57455"
	KrillModelDefault = "gemma-4-12b"
)

// KrillProvider calls Krill's OpenAI-compatible /v1/chat/completions endpoint.
// The request timeout is deliberately generous: a cold MLX model load can take
// minutes and a background agent should take the time it needs rather than be
// cut off mid-thought. A refused connection still errors immediately, so a
// FailoverProvider can drop to Ollama fast when Krill is actually down.
type KrillProvider struct {
	baseURL string
	model   string
	temp    float64
	maxTok  int
	client  *http.Client
}

func NewKrillProvider(baseURL, model string, cfg config.LLMConfig) *KrillProvider {
	if baseURL == "" {
		baseURL = KrillDefaultURL
	}
	if model == "" || model == "auto" {
		model = KrillModelDefault
	}
	temp := cfg.Temperature
	if temp <= 0 {
		temp = 0.7
	}
	maxTok := cfg.MaxTokens
	if maxTok <= 0 {
		maxTok = 2048
	}
	return &KrillProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		temp:    temp,
		maxTok:  maxTok,
		client:  &http.Client{Timeout: 10 * time.Minute},
	}
}

func (k *KrillProvider) Name() string      { return "krill" }
func (k *KrillProvider) ModelName() string { return k.model }

// Available is a fast liveness probe against /v1/models so a FailoverProvider
// can decide whether Krill is reachable without paying the full chat timeout.
func (k *KrillProvider) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.baseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < 300
}

func (k *KrillProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	o := core.ApplyOptions(opts)
	model := k.model
	if o.Model != "" {
		model = o.Model
	}
	temp := k.temp
	if o.Temperature != 0 {
		temp = o.Temperature
	}
	maxTok := k.maxTok
	if o.MaxTokens != 0 {
		maxTok = o.MaxTokens
	}

	msgs := make([]map[string]string, 0, len(messages)+1)
	if o.SystemPrompt != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": o.SystemPrompt})
	}
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	body, err := json.Marshal(map[string]interface{}{
		"model": model, "messages": msgs, "temperature": temp, "max_tokens": maxTok,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal krill request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create krill request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	log.Debug("krill chat request", "model", model, "messages", len(msgs))

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("krill request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		em := string(respBody)
		if len(em) > 200 {
			em = em[:200] + "..."
		}
		return nil, fmt.Errorf("krill HTTP %d: %s", resp.StatusCode, em)
	}

	var r struct {
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
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("decode krill response: %w", err)
	}
	if len(r.Choices) == 0 {
		return nil, fmt.Errorf("krill returned no choices")
	}
	return &core.Response{
		Content:          r.Choices[0].Message.Content,
		Model:            model,
		PromptTokens:     r.Usage.PromptTokens,
		CompletionTokens: r.Usage.CompletionTokens,
	}, nil
}

func (k *KrillProvider) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	go func() {
		defer close(ch)
		resp, err := k.Chat(ctx, messages, opts...)
		if err != nil {
			ch <- core.StreamChunk{Done: true, Err: err}
			return
		}
		ch <- core.StreamChunk{Content: resp.Content, Done: true}
	}()
	return ch, nil
}
