package reef

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// configure points the client at a mock hub for the duration of a test.
func configure(t *testing.T, url string) {
	t.Helper()
	t.Setenv("REEF_INGEST_URL", url)
	t.Setenv("REEF_AGENT_TOKEN", "test-token")
	t.Setenv("REEF_AGENT_ID", "minikrill")
}

func TestIsConfigured(t *testing.T) {
	t.Setenv("REEF_INGEST_URL", "")
	t.Setenv("REEF_AGENT_TOKEN", "")
	if IsConfigured() {
		t.Fatal("IsConfigured should be false with no env")
	}
	configure(t, "http://example.test")
	if !IsConfigured() {
		t.Fatal("IsConfigured should be true once url+token are set")
	}
}

func TestPollOutboxLeasesWithAck(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotToken = r.Header.Get("X-Reef-Token")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]string{{"id": "m1", "type": "reply", "text": "hi"}},
		})
	}))
	defer srv.Close()
	configure(t, srv.URL)

	items, err := PollOutbox(context.Background(), 0, true)
	if err != nil {
		t.Fatalf("PollOutbox: %v", err)
	}
	if len(items) != 1 || items[0].ID != "m1" || items[0].Text != "hi" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if gotToken != "test-token" {
		t.Fatalf("missing/incorrect token header: %q", gotToken)
	}
	if !contains(gotPath, "ack=1") {
		t.Fatalf("ack=1 not sent in lease mode: %q", gotPath)
	}
	if !contains(gotPath, "agent=minikrill") {
		t.Fatalf("agent not sent: %q", gotPath)
	}
}

func TestPollOutboxLegacyHasNoAck(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		io.WriteString(w, `{"items":[]}`)
	}))
	defer srv.Close()
	configure(t, srv.URL)

	if _, err := PollOutbox(context.Background(), 0, false); err != nil {
		t.Fatalf("PollOutbox: %v", err)
	}
	if contains(gotPath, "ack=1") {
		t.Fatalf("legacy poll must not send ack=1: %q", gotPath)
	}
}

func TestPollOutboxSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	configure(t, srv.URL)

	if _, err := PollOutbox(context.Background(), 0, true); err == nil {
		t.Fatal("expected an error on 401")
	}
}

func TestAckPostsIDs(t *testing.T) {
	var body struct {
		Agent string   `json:"agent"`
		IDs   []string `json:"ids"`
	}
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Reef-Token")
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"ok":true,"acked":1}`)
	}))
	defer srv.Close()
	configure(t, srv.URL)

	if err := Ack(context.Background(), []string{"m1", "m2"}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if gotPath != "/api/ack" {
		t.Fatalf("ack hit wrong path: %q", gotPath)
	}
	if gotToken != "test-token" {
		t.Fatalf("ack missing token: %q", gotToken)
	}
	if body.Agent != "minikrill" || len(body.IDs) != 2 || body.IDs[0] != "m1" {
		t.Fatalf("ack body wrong: %+v", body)
	}
}

func TestAckNoopOnEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	configure(t, srv.URL)

	if err := Ack(context.Background(), nil); err != nil {
		t.Fatalf("Ack(nil): %v", err)
	}
	if called {
		t.Fatal("Ack with no ids must not hit the network")
	}
}

func TestPostIngest(t *testing.T) {
	var payload ingestPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&payload)
		io.WriteString(w, `{"ok":true,"id":"x"}`)
	}))
	defer srv.Close()
	configure(t, srv.URL)

	if err := PostIngest("chat", "chat", "hello owner"); err != nil {
		t.Fatalf("PostIngest: %v", err)
	}
	if payload.Agent != "minikrill" || payload.Channel != "chat" || payload.Content != "hello owner" {
		t.Fatalf("ingest payload wrong: %+v", payload)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
