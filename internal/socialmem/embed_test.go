//go:build colony

package socialmem

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedSendsTaskAndNormalises(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"data":[{"embedding":[3,4]}]}`))
	}))
	defer srv.Close()
	s := New("a", Config{Enabled: true, EmbedURL: srv.URL})
	v, err := s.embed(context.Background(), "hi", "SearchQuery")
	if err != nil || got["task"] != "SearchQuery" || got["dimensions"] != float64(768) {
		t.Fatalf("err=%v body=%v", err, got)
	}
	if math.Abs(float64(v[0])-0.6) > 1e-6 || math.Abs(float64(v[1])-0.8) > 1e-6 {
		t.Fatalf("not unit: %v", v)
	}
}

func TestCollectionSuffix(t *testing.T) {
	if c := New("a", Config{}).cfg.Collection; c != "a_social_eg2" {
		t.Fatal(c)
	}
	if c := New("a", Config{EmbedModel: "bge-base-en"}).cfg.Collection; c != "a_social" {
		t.Fatal(c)
	}
}
