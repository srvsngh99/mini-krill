//go:build colony

// Package socialmem gives the agent a durable, SEMANTIC social memory: the
// things it has said and heard across the Reef feed and its DMs, embedded with
// the colony's shared bge embedder (served by krillm) and stored in the agent's
// OWN Chroma collection so it can later RECALL them by meaning — e.g. "what did
// I say about the map renderer a few days ago" — and reference the past like a
// human would.
//
// Design rules:
//   - OWN memory only: each agent uses a separate collection; nothing is shared.
//   - Best-effort, never fatal: if the embedder or Chroma is unreachable,
//     Remember silently no-ops and Recall returns nothing, so the agent behaves
//     exactly as before. Social memory is an enhancement, not a dependency.
package socialmem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config tunes the social memory. Zero values fall back to colony defaults.
type Config struct {
	Enabled    bool   `yaml:"enabled"`
	ChromaURL  string `yaml:"chroma_url"`  // default http://127.0.0.1:8001
	EmbedURL   string `yaml:"embed_url"`   // default http://127.0.0.1:57455
	EmbedModel string `yaml:"embed_model"` // default bge-base-en
	Collection string `yaml:"collection"`  // default "<agent>_social"
	RecallK    int    `yaml:"recall_k"`    // default 4
}

// Store is the agent's social memory. A nil *Store is safe (all methods no-op),
// so callers never need to nil-check before using it.
type Store struct {
	cfg    Config
	agent  string
	http   *http.Client
	mu     sync.Mutex
	collID string // resolved lazily and cached
}

// Memory is one thing worth remembering. Meta is free-form structured context
// (kind, surface, post_id, comment_id, counterparty, channel, ts).
type Memory struct {
	ID   string
	Text string
	Meta map[string]any
}

// Hit is one recalled memory, ordered by relevance (smaller Distance = closer).
type Hit struct {
	Text     string
	Meta     map[string]any
	Distance float64
}

// New builds a social memory store for the given agent. Defaults are applied for
// any unset config field. The collection defaults to "<agent>_social" so each
// agent's memory is isolated.
func New(agent string, cfg Config) *Store {
	if cfg.ChromaURL == "" {
		cfg.ChromaURL = "http://127.0.0.1:8001"
	}
	if cfg.EmbedURL == "" {
		cfg.EmbedURL = "http://127.0.0.1:57455"
	}
	if cfg.EmbedModel == "" {
		cfg.EmbedModel = "bge-base-en"
	}
	if cfg.Collection == "" {
		cfg.Collection = agent + "_social"
	}
	if cfg.RecallK == 0 {
		cfg.RecallK = 4
	}
	return &Store{cfg: cfg, agent: agent, http: &http.Client{Timeout: 20 * time.Second}}
}

// Enabled reports whether the store should do anything.
func (s *Store) Enabled() bool { return s != nil && s.cfg.Enabled }

// Remember stores one memory. Best-effort: any failure is logged and swallowed.
func (s *Store) Remember(ctx context.Context, m Memory) {
	if !s.Enabled() || strings.TrimSpace(m.Text) == "" {
		return
	}
	collID, err := s.collection(ctx)
	if err != nil {
		log.Printf("[socialmem] remember skipped (collection): %v", err)
		return
	}
	emb, err := s.embed(ctx, m.Text)
	if err != nil {
		log.Printf("[socialmem] remember skipped (embed): %v", err)
		return
	}
	if m.Meta == nil {
		m.Meta = map[string]any{}
	}
	if _, ok := m.Meta["agent"]; !ok {
		m.Meta["agent"] = s.agent
	}
	body := map[string]any{
		"ids":        []string{m.ID},
		"embeddings": [][]float32{emb},
		"documents":  []string{m.Text},
		"metadatas":  []map[string]any{m.Meta},
	}
	if err := s.post(ctx, s.collPath(collID)+"/upsert", body, nil); err != nil {
		log.Printf("[socialmem] remember failed: %v", err)
	}
}

// Recall returns up to k of the agent's own memories most semantically similar
// to query, nearest first. Returns nil on any error (never fatal).
func (s *Store) Recall(ctx context.Context, query string, k int) []Hit {
	if !s.Enabled() || strings.TrimSpace(query) == "" {
		return nil
	}
	if k <= 0 {
		k = s.cfg.RecallK
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return nil
	}
	emb, err := s.embed(ctx, query)
	if err != nil {
		return nil
	}
	req := map[string]any{
		"query_embeddings": [][]float32{emb},
		"n_results":        k,
		"include":          []string{"documents", "metadatas", "distances"},
	}
	var out struct {
		Documents [][]string         `json:"documents"`
		Metadatas [][]map[string]any `json:"metadatas"`
		Distances [][]float64        `json:"distances"`
	}
	if err := s.post(ctx, s.collPath(collID)+"/query", req, &out); err != nil {
		log.Printf("[socialmem] recall failed: %v", err)
		return nil
	}
	if len(out.Documents) == 0 {
		return nil
	}
	docs := out.Documents[0]
	hits := make([]Hit, 0, len(docs))
	for i, d := range docs {
		h := Hit{Text: d}
		if len(out.Metadatas) > 0 && i < len(out.Metadatas[0]) {
			h.Meta = out.Metadatas[0][i]
		}
		if len(out.Distances) > 0 && i < len(out.Distances[0]) {
			h.Distance = out.Distances[0][i]
		}
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Distance < hits[j].Distance })
	return hits
}

// RecallBlock recalls the agent's own relevant memories for query and renders
// them as a prompt section the model can use to reference the past naturally.
// Returns "" when nothing relevant is found (or memory is disabled), so callers
// can append it unconditionally.
func (s *Store) RecallBlock(ctx context.Context, query string) string {
	hits := s.Recall(ctx, query, 0)
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nYOUR RELEVANT MEMORY (things you said or heard before — reference them naturally if useful, don't repeat yourself):\n")
	for _, h := range hits {
		when := ""
		if ts, ok := metaInt(h.Meta, "ts"); ok && ts > 0 {
			when = relTime(ts) + " — "
		}
		ctxLabel := ""
		if k, _ := h.Meta["kind"].(string); k != "" {
			ctxLabel = "[" + k + "] "
		}
		b.WriteString("- " + when + ctxLabel + strings.TrimSpace(h.Text) + "\n")
	}
	return b.String()
}

func metaInt(m map[string]any, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

// relTime renders a millisecond unix timestamp as a coarse human interval.
func relTime(ms int64) string {
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Hour:
		return "earlier today"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/(24*7)))
	}
}

// collection resolves (and caches) the Chroma collection id, creating it if it
// does not exist (cosine space, which suits bge embeddings).
func (s *Store) collection(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collID != "" {
		return s.collID, nil
	}
	body := map[string]any{
		"name":          s.cfg.Collection,
		"get_or_create": true,
		"configuration": map[string]any{"hnsw": map[string]any{"space": "cosine"}},
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := s.post(ctx, s.base()+"/collections", body, &resp); err != nil || resp.ID == "" {
		// Retry without the space configuration in case the server rejects the
		// configuration shape; default space still gives usable recall.
		body2 := map[string]any{"name": s.cfg.Collection, "get_or_create": true}
		if err2 := s.post(ctx, s.base()+"/collections", body2, &resp); err2 != nil || resp.ID == "" {
			if err == nil {
				err = err2
			}
			return "", fmt.Errorf("ensure collection: %v", err)
		}
	}
	s.collID = resp.ID
	return s.collID, nil
}

func (s *Store) base() string {
	return strings.TrimRight(s.cfg.ChromaURL, "/") + "/api/v2/tenants/default_tenant/databases/default_database"
}

func (s *Store) collPath(id string) string { return s.base() + "/collections/" + id }

// embed turns text into a vector via the shared krillm bge embedder.
func (s *Store) embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{"model": s.cfg.EmbedModel, "input": text}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := s.post(ctx, strings.TrimRight(s.cfg.EmbedURL, "/")+"/v1/embeddings", body, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return out.Data[0].Embedding, nil
}

// post issues a JSON POST and optionally decodes the response into out.
func (s *Store) post(ctx context.Context, url string, payload any, out any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return fmt.Errorf("%s -> %d: %s", url, resp.StatusCode, strings.TrimSpace(b.String()))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
