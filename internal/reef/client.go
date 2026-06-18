// Package reef connects Mini-Krill to the Reef messaging hub: it posts the
// agent's messages and long-polls for the owner's replies. When
// REEF_INGEST_URL / REEF_AGENT_TOKEN are unset the client is inert
// (IsConfigured returns false) and Mini-Krill uses its other channels.
package reef

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func baseURL() string   { return strings.TrimRight(os.Getenv("REEF_INGEST_URL"), "/") }
func authToken() string { return os.Getenv("REEF_AGENT_TOKEN") }

// AgentID is this agent's id in Reef (default "minikrill").
func AgentID() string {
	if v := os.Getenv("REEF_AGENT_ID"); v != "" {
		return v
	}
	return "minikrill"
}

// IsConfigured reports whether the Reef hub URL and token are both set.
func IsConfigured() bool { return baseURL() != "" && authToken() != "" }

type ingestPayload struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
	Kind    string `json:"kind"`
	Format  string `json:"format"`
	Content string `json:"content"`
}

// PostIngest delivers one message to the Reef hub.
func PostIngest(channel, kind, content string) error {
	if !IsConfigured() {
		return nil
	}
	body, err := json.Marshal(ingestPayload{AgentID(), channel, kind, "md", content})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL()+"/api/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Reef-Token", authToken())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("reef ingest: status %d", resp.StatusCode)
	}
	return nil
}

// OutboxItem is one owner->agent message returned by the long-poll.
type OutboxItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// PollOutbox long-polls the hub for owner messages (waitSeconds <= 50). The
// ctx aborts the in-flight request on shutdown so the caller does not block for
// the full long-poll window. The hub blocks server-side up to waitSeconds when
// the queue is empty (it does not return early), so callers do not spin.
func PollOutbox(ctx context.Context, waitSeconds int) ([]OutboxItem, error) {
	if !IsConfigured() {
		return nil, nil
	}
	url := fmt.Sprintf("%s/api/outbox?agent=%s&wait=%d", baseURL(), AgentID(), waitSeconds)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Reef-Token", authToken())
	resp, err := (&http.Client{Timeout: time.Duration(waitSeconds+10) * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("reef outbox: status %d", resp.StatusCode)
	}
	var out struct {
		Items []OutboxItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Items, nil
}
