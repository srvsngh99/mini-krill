package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// aiDigestSkill produces a fresh AI-news digest from real sources: Hacker
// News stories from the last 24h (Algolia API, no key needed) plus a
// DuckDuckGo news sweep. Every item carries its real URL and the LLM is
// instructed to cite only the listed links — the skill exists because a
// generic "search the web" pass returned homepage boilerplate and tempted
// the model to invent headlines.
type aiDigestSkill struct {
	hnBase   string // overridable in tests; empty = public Algolia API
	searchFn func(ctx context.Context, query string) ([]searchResult, error)
}

func newAIDigestSkill() *aiDigestSkill {
	return &aiDigestSkill{
		hnBase:   "https://hn.algolia.com/api/v1",
		searchFn: duckduckgoSearch,
	}
}

func (s *aiDigestSkill) Name() string { return "digest" }
func (s *aiDigestSkill) Description() string {
	return "Fetch a fresh AI news digest from real sources (Hacker News last 24h + web search)"
}

type hnHit struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Points    int    `json:"points"`
	ObjectID  string `json:"objectID"`
	CreatedAt string `json:"created_at"`
}

func (s *aiDigestSkill) fetchHN(ctx context.Context) ([]hnHit, error) {
	since := time.Now().Add(-24 * time.Hour).Unix()
	q := url.Values{}
	q.Set("query", "AI")
	q.Set("tags", "story")
	// Algolia rejects a raw '>' in the query string with 400 — encode it.
	q.Set("numericFilters", fmt.Sprintf("created_at_i>%d", since))
	q.Set("hitsPerPage", "15")
	req, err := http.NewRequestWithContext(ctx, "GET", s.hnBase+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MiniKrill/1.0 (digest skill)")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("hacker news API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Hits []hnHit `json:"hits"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Hits, nil
}

func (s *aiDigestSkill) Execute(ctx context.Context, _ string, llm core.LLMProvider) (string, error) {
	hits, hnErr := s.fetchHN(ctx)
	var web []searchResult
	var webErr error
	if s.searchFn != nil {
		web, webErr = s.searchFn(ctx, "AI news today")
	}

	if hnErr != nil && webErr != nil {
		return "", fmt.Errorf("digest sources unavailable: hacker news: %v; web search: %v", hnErr, webErr)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("AI digest sources for %s\n\n", time.Now().Format("2006-01-02")))
	if len(hits) > 0 {
		sb.WriteString("Hacker News (last 24h):\n")
		for i, h := range hits {
			if i >= 10 {
				break
			}
			url := h.URL
			if url == "" {
				url = "https://news.ycombinator.com/item?id=" + h.ObjectID
			}
			sb.WriteString(fmt.Sprintf("- %s (%d points)\n  %s\n", h.Title, h.Points, url))
		}
		sb.WriteString("\n")
	}
	if len(web) > 0 {
		sb.WriteString("Web results:\n")
		for i, r := range web {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n  %s\n  %s\n", r.title, r.url, r.snippet))
		}
	}
	raw := sb.String()
	if len(hits) == 0 && len(web) == 0 {
		return "No fresh AI items found in the last 24h — sources were reachable but empty.", nil
	}

	if llm != nil {
		summary, err := llm.Chat(ctx, []core.Message{
			{Role: "system", Content: "Turn these items into a concise AI news digest: a headline line per story with a one-line takeaway, grouped by theme. Use ONLY the items and URLs listed — never invent stories, never cite a URL that is not in the list. If an item's substance is unclear from the title, say so rather than guessing."},
			{Role: "user", Content: raw},
		})
		if err == nil && strings.TrimSpace(summary.Content) != "" {
			return summary.Content, nil
		}
	}
	return raw, nil
}
