package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/ollama"
)

func newTestOllamaProvider(serverURL, model string) *OllamaProvider {
	return NewOllamaProvider(serverURL, model, config.LLMConfig{})
}

func TestAvailableModelPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []struct {
				Name string `json:"name"`
			}{
				{Name: "gemma3:4b"},
				{Name: "llama3.2:3b"},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL, "gemma3:4b")
	if !p.Available(context.Background()) {
		t.Error("expected Available=true when model is in the list")
	}
}

func TestAvailableModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []struct {
				Name string `json:"name"`
			}{
				{Name: "llama3.2:3b"},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL, "gemma3:4b")
	if p.Available(context.Background()) {
		t.Error("expected Available=false when model is not in the list")
	}
}

func TestAvailableEmptyModelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(ollamaTagsResponse{}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL, "gemma3:4b")
	if p.Available(context.Background()) {
		t.Error("expected Available=false when model list is empty")
	}
}

func TestAvailableServerDown(t *testing.T) {
	p := newTestOllamaProvider("http://127.0.0.1:1", "gemma3:4b")
	if p.Available(context.Background()) {
		t.Error("expected Available=false when server is unreachable")
	}
}

func TestAvailableServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestOllamaProvider(srv.URL, "gemma3:4b")
	if p.Available(context.Background()) {
		t.Error("expected Available=false when server returns 500")
	}
}

func TestAvailableNormalizesModelName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(ollamaTagsResponse{
			Models: []struct {
				Name string `json:"name"`
			}{
				{Name: "gemma3:latest"},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	// Config says "gemma3" (no tag), server has "gemma3:latest" - should match
	p := newTestOllamaProvider(srv.URL, "gemma3")
	if !p.Available(context.Background()) {
		t.Error("expected Available=true: 'gemma3' should match 'gemma3:latest'")
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gemma3", "gemma3:latest"},
		{"gemma3:4b", "gemma3:4b"},
		{"Gemma3:4B", "gemma3:4b"},
		{"LLAMA3.2:3B", "llama3.2:3b"},
		{"model:latest", "model:latest"},
	}
	for _, tt := range tests {
		got := ollama.NormalizeModelName(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
