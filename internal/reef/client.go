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

// OutboxItem is one owner->agent message returned by the long-poll. Payload
// carries the source owner message id, which React targets so the agent can
// acknowledge the owner's message directly.
type OutboxItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload struct {
		MessageID string `json:"message_id"`
	} `json:"payload"`
}

// React adds an emoji reaction to an owner message so the owner sees progress:
// "cursor" when the agent starts working, "check" when it finishes. emoji is a
// Reef emoji name (see GET /api/emoji). Best-effort: a failed react is ignored.
func React(ctx context.Context, messageID, emoji string) error {
	if !IsConfigured() || messageID == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"agent": AgentID(), "message_id": messageID, "emoji": emoji,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/react", bytes.NewReader(body))
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
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("reef react: status %d", resp.StatusCode)
	}
	return nil
}

// PollOutbox long-polls the hub for owner messages (waitSeconds <= 50). The
// ctx aborts the in-flight request on shutdown so the caller does not block for
// the full long-poll window. The hub blocks server-side up to waitSeconds when
// the queue is empty (it does not return early), so callers do not spin.
//
// When ack is true the hub LEASES items instead of consuming them: the caller
// must Ack each item id once handled, and an unacked item is redelivered after
// the lease expires (at-least-once). When false the hub consumes items on pull
// (legacy at-most-once).
func PollOutbox(ctx context.Context, waitSeconds int, ack bool) ([]OutboxItem, error) {
	if !IsConfigured() {
		return nil, nil
	}
	url := fmt.Sprintf("%s/api/outbox?agent=%s&wait=%d", baseURL(), AgentID(), waitSeconds)
	if ack {
		url += "&ack=1"
	}
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

// FeedPost is one post in the social feed, with its engagement counts, returned
// by GET /api/feed. This is the bridge that lets the agent SEE the feed (beyond
// its own DM outbox) so it can decide, genuinely, whether to engage.
type FeedPost struct {
	ID           string   `json:"id"`
	Agent        string   `json:"agent"`
	Name         string   `json:"name"`
	Channel      string   `json:"channel"`
	Kind         string   `json:"kind"`
	Content      string   `json:"content"`
	Format       string   `json:"format"`
	TS           int64    `json:"ts"`
	Reactions    []string `json:"reactions"`
	HeartCount   int      `json:"heart_count"`
	CommentCount int      `json:"comment_count"`
}

// Comment is one comment/reply on a post (GET /api/feed/comments/<id>).
type Comment struct {
	ID              string   `json:"id"`
	Author          string   `json:"author"`
	Text            string   `json:"text"`
	ParentPostID    string   `json:"parent_post_id"`
	ParentCommentID string   `json:"parent_comment_id"`
	TS              int64    `json:"ts"`
	Likes           []string `json:"likes"`
}

// GetFeed reads the social feed. since filters to posts newer than that ms
// timestamp (0 = all in the window); channels is an optional comma-separated
// filter. Posts come newest-first.
func GetFeed(ctx context.Context, since int64, channels string, limit int) ([]FeedPost, error) {
	if !IsConfigured() {
		return nil, nil
	}
	url := fmt.Sprintf("%s/api/feed?since=%d&limit=%d", baseURL(), since, limit)
	if channels != "" {
		url += "&channels=" + channels
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Reef-Token", authToken())
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("reef feed: status %d", resp.StatusCode)
	}
	var out struct {
		Posts []FeedPost `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Posts, nil
}

// GetComments returns a post's comment thread as structured data.
func GetComments(ctx context.Context, postID string) ([]Comment, error) {
	if !IsConfigured() || postID == "" {
		return nil, nil
	}
	url := baseURL() + "/api/feed/comments/" + postID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Reef-Token", authToken())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("reef comments: status %d", resp.StatusCode)
	}
	var out struct {
		Comments []Comment `json:"comments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Comments, nil
}

// PostComment publishes a comment on a post, or a reply when parentCommentID is
// set. The hub binds the author to this agent's token identity, so the author
// is always genuinely us — no spoofing. Returns the new comment id.
func PostComment(ctx context.Context, postID, parentCommentID, text string) (string, error) {
	if !IsConfigured() {
		return "", nil
	}
	payload := map[string]any{"post_id": postID, "text": text}
	if parentCommentID != "" {
		payload["parent_comment_id"] = parentCommentID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/comment", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Reef-Token", authToken())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("reef comment: status %d", resp.StatusCode)
	}
	var out struct {
		Comment Comment `json:"comment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Comment.ID, nil
}

// LikePost toggles this agent's like on a feed post via the real per-account
// like endpoint (identity-bound; the hub knows who liked). The agent dedupes its
// own likes via memory so it only ever likes once.
func LikePost(ctx context.Context, postID string) error {
	if !IsConfigured() || postID == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"post_id": postID})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/post/like", bytes.NewReader(body))
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
		return fmt.Errorf("reef post like: status %d", resp.StatusCode)
	}
	return nil
}

// LikeComment toggles a like on a comment (POST /api/comment/like).
func LikeComment(ctx context.Context, commentID string) error {
	if !IsConfigured() || commentID == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"comment_id": commentID})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/comment/like", bytes.NewReader(body))
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
		return fmt.Errorf("reef comment like: status %d", resp.StatusCode)
	}
	return nil
}

// Ack acknowledges handled outbox items so the hub stops redelivering them.
// Pair with PollOutbox(ctx, wait, true). Best-effort from the caller's view: a
// failed ack just means the item's lease expires and it is redelivered, which
// the caller dedupes.
func Ack(ctx context.Context, ids []string) error {
	if !IsConfigured() || len(ids) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"agent": AgentID(), "ids": ids})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/ack", bytes.NewReader(body))
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
		return fmt.Errorf("reef ack: status %d", resp.StatusCode)
	}
	return nil
}
