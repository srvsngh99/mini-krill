//go:build colony

package socialmem

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Record is a stored row (id + document + metadata) returned by Get/All. It is
// the raw shape; higher layers (e.g. a core.Memory adapter) map it to their own
// entry types.
type Record struct {
	ID   string
	Text string
	Meta map[string]any
}

// Upsert writes one memory (id + text + metadata), embedding the text with the
// shared bge embedder. Unlike Remember (best-effort, void), Upsert returns the
// error so callers that need to know — e.g. a migration — can react.
func (s *Store) Upsert(ctx context.Context, m Memory) error {
	if !s.Enabled() {
		return fmt.Errorf("social memory disabled")
	}
	if strings.TrimSpace(m.Text) == "" {
		return fmt.Errorf("empty text")
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return err
	}
	emb, err := s.embed(ctx, m.Text)
	if err != nil {
		return err
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
	return s.post(ctx, s.collPath(collID)+"/upsert", body, nil)
}

// Get returns the row with the given id, or (nil, nil) if it does not exist.
func (s *Store) Get(ctx context.Context, id string) (*Record, error) {
	if !s.Enabled() {
		return nil, nil
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return nil, err
	}
	req := map[string]any{"ids": []string{id}, "include": []string{"documents", "metadatas"}}
	var out struct {
		IDs       []string         `json:"ids"`
		Documents []string         `json:"documents"`
		Metadatas []map[string]any `json:"metadatas"`
	}
	if err := s.post(ctx, s.collPath(collID)+"/get", req, &out); err != nil {
		return nil, err
	}
	if len(out.IDs) == 0 {
		return nil, nil
	}
	r := &Record{ID: out.IDs[0]}
	if len(out.Documents) > 0 {
		r.Text = out.Documents[0]
	}
	if len(out.Metadatas) > 0 {
		r.Meta = out.Metadatas[0]
	}
	return r, nil
}

// All returns up to limit rows (limit<=0 means a large default). Used for List
// and for bounded scans; the colony memory is small (~1k entries).
func (s *Store) All(ctx context.Context, limit int) ([]Record, error) {
	if !s.Enabled() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10000
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return nil, err
	}
	req := map[string]any{"limit": limit, "include": []string{"documents", "metadatas"}}
	var out struct {
		IDs       []string         `json:"ids"`
		Documents []string         `json:"documents"`
		Metadatas []map[string]any `json:"metadatas"`
	}
	if err := s.post(ctx, s.collPath(collID)+"/get", req, &out); err != nil {
		return nil, err
	}
	recs := make([]Record, 0, len(out.IDs))
	for i, id := range out.IDs {
		r := Record{ID: id}
		if i < len(out.Documents) {
			r.Text = out.Documents[i]
		}
		if i < len(out.Metadatas) {
			r.Meta = out.Metadatas[i]
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// Count returns the number of rows in the collection (0 on any error).
func (s *Store) Count(ctx context.Context) int {
	if !s.Enabled() {
		return 0
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return 0
	}
	var n int
	if err := s.get(ctx, s.collPath(collID)+"/count", &n); err != nil {
		return 0
	}
	return n
}

// Delete removes the row with the given id (best-effort).
func (s *Store) Delete(ctx context.Context, id string) error {
	if !s.Enabled() {
		return nil
	}
	collID, err := s.collection(ctx)
	if err != nil {
		return err
	}
	return s.post(ctx, s.collPath(collID)+"/delete", map[string]any{"ids": []string{id}}, nil)
}

// DropCollection deletes the whole collection by name (used by tests/cleanup).
func (s *Store) DropCollection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.base()+"/collections/"+s.cfg.Collection, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	s.mu.Lock()
	s.collID = ""
	s.mu.Unlock()
	return nil
}

// RecallWhere is Recall with an optional metadata filter (Chroma `where`), used
// for scope-filtered ranked search. A nil/empty where behaves like Recall.
func (s *Store) RecallWhere(ctx context.Context, query string, k int, where map[string]any) []Hit {
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
	if len(where) > 0 {
		req["where"] = where
	}
	var out struct {
		Documents [][]string         `json:"documents"`
		Metadatas [][]map[string]any `json:"metadatas"`
		Distances [][]float64        `json:"distances"`
	}
	if err := s.post(ctx, s.collPath(collID)+"/query", req, &out); err != nil || len(out.Documents) == 0 {
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
	return hits
}
