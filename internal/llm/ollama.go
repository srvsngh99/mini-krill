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

// OllamaProvider talks to a local Ollama instance over its REST API.
// Krill fact: krill thrive in cold local waters - Ollama thrives on local GPUs.
type OllamaProvider struct {
	host        string
	model       string
	temperature float64
	maxTokens   int
	chatClient  *http.Client // 120s timeout for inference
	healthClient *http.Client // 5s timeout for health checks
}

// NewOllamaProvider creates a provider targeting the given Ollama host and model.
func NewOllamaProvider(host string, model string, defaultOpts config.LLMConfig) *OllamaProvider {
	host = strings.TrimRight(host, "/")

	temp := defaultOpts.Temperature
	if temp <= 0 {
		temp = 0.7
	}
	maxTok := defaultOpts.MaxTokens
	if maxTok <= 0 {
		maxTok = 2048
	}

	return &OllamaProvider{
		host:        host,
		model:       model,
		temperature: temp,
		maxTokens:   maxTok,
		chatClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		healthClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// Ollama request / response types (unexported, internal to this file)
// ---------------------------------------------------------------------------

type ollamaChatRequest struct {
	Model    string            `json:"model"`
	Messages []ollamaChatMsg   `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  ollamaChatOptions `json:"options,omitempty"`
}

type ollamaChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaChatResponse struct {
	Message        ollamaChatMsg `json:"message"`
	Done           bool          `json:"done"`
	EvalCount      int           `json:"eval_count"`
	PromptEvalCount int          `json:"prompt_eval_count"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ---------------------------------------------------------------------------
// LLMProvider interface implementation
// ---------------------------------------------------------------------------

// Chat sends a non-streaming chat request to Ollama and returns the full response.
func (o *OllamaProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	options := core.ApplyOptions(opts)

	model := o.model
	if options.Model != "" {
		model = options.Model
	}
	temp := o.temperature
	if options.Temperature != 0 {
		temp = options.Temperature
	}
	maxTok := o.maxTokens
	if options.MaxTokens != 0 {
		maxTok = options.MaxTokens
	}

	// Build message list, prepending system prompt if provided
	ollamaMsgs := make([]ollamaChatMsg, 0, len(messages)+1)
	if options.SystemPrompt != "" {
		ollamaMsgs = append(ollamaMsgs, ollamaChatMsg{Role: "system", Content: options.SystemPrompt})
	}
	for _, m := range messages {
		ollamaMsgs = append(ollamaMsgs, ollamaChatMsg{Role: m.Role, Content: m.Content})
	}

	reqBody := ollamaChatRequest{
		Model:    model,
		Messages: ollamaMsgs,
		Stream:   false,
		Options: ollamaChatOptions{
			Temperature: temp,
			NumPredict:  maxTok,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama request: %w", err)
	}

	url := o.host + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	log.Debug("ollama chat request", "model", model, "messages", len(ollamaMsgs))

	resp, err := o.chatClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return nil, fmt.Errorf("ollama is not running at %s - start it with 'ollama serve' or 'krill dive'", o.host)
		}
		return nil, fmt.Errorf("ollama chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}

	return &core.Response{
		Content:          ollamaResp.Message.Content,
		Model:            model,
		PromptTokens:     ollamaResp.PromptEvalCount,
		CompletionTokens: ollamaResp.EvalCount,
	}, nil
}

// Stream sends a streaming chat request to Ollama and returns a channel of chunks.
// Each line of the NDJSON response becomes one StreamChunk.
func (o *OllamaProvider) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	options := core.ApplyOptions(opts)

	model := o.model
	if options.Model != "" {
		model = options.Model
	}
	temp := o.temperature
	if options.Temperature != 0 {
		temp = options.Temperature
	}
	maxTok := o.maxTokens
	if options.MaxTokens != 0 {
		maxTok = options.MaxTokens
	}

	ollamaMsgs := make([]ollamaChatMsg, 0, len(messages)+1)
	if options.SystemPrompt != "" {
		ollamaMsgs = append(ollamaMsgs, ollamaChatMsg{Role: "system", Content: options.SystemPrompt})
	}
	for _, m := range messages {
		ollamaMsgs = append(ollamaMsgs, ollamaChatMsg{Role: m.Role, Content: m.Content})
	}

	reqBody := ollamaChatRequest{
		Model:    model,
		Messages: ollamaMsgs,
		Stream:   true,
		Options: ollamaChatOptions{
			Temperature: temp,
			NumPredict:  maxTok,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama stream request: %w", err)
	}

	url := o.host + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ollama stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a client without timeout since streaming can be long-lived;
	// cancellation is handled via context.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return nil, fmt.Errorf("ollama is not running at %s - start it with 'ollama serve' or 'krill dive'", o.host)
		}
		return nil, fmt.Errorf("ollama stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan core.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		for {
			// Check for context cancellation between reads
			select {
			case <-ctx.Done():
				ch <- core.StreamChunk{Done: true, Err: ctx.Err()}
				return
			default:
			}

			var chunk ollamaChatResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					ch <- core.StreamChunk{Done: true}
					return
				}
				ch <- core.StreamChunk{Done: true, Err: fmt.Errorf("decode stream chunk: %w", err)}
				return
			}

			ch <- core.StreamChunk{
				Content: chunk.Message.Content,
				Done:    chunk.Done,
			}

			if chunk.Done {
				return
			}
		}
	}()

	return ch, nil
}

// Name returns the provider name.
func (o *OllamaProvider) Name() string { return "ollama" }

// ModelName returns the configured model name.
func (o *OllamaProvider) ModelName() string { return o.model }

// Available checks whether the Ollama server is reachable.
// Krill fact: krill use bioluminescence to check if their swarm-mates are nearby.
func (o *OllamaProvider) Available(ctx context.Context) bool {
	url := o.host + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := o.healthClient.Do(req)
	if err != nil {
		log.Debug("ollama health check failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusOK
}

// isConnectionRefused detects connection refused errors regardless of wrapping.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connect: connection refused") ||
		strings.Contains(s, "dial tcp") && strings.Contains(s, "refused")
}
