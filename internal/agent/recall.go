// Package agent — recall.go owns scrollback over the conversation log.
//
// Two surfaces:
//   - `/recall last N` / `/recall yesterday` / `/recall about <topic>` — explicit.
//   - Natural-language triggers ("what did I say yesterday", "go back to the
//     YouTube one") detected in agent.go before intent classification.
//
// The store is the existing ConversationStore.Search added in this PR — it
// scans the entire JSONL, not just the sliding LoadRecent window. This is
// what closes the "remember when we discussed X" gap (Issue #19).
package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// recallTriggers are natural-language phrases that map to a /recall request.
// Each entry is (regex, "topic" capture group index, "duration" capture group index).
type recallTrigger struct {
	pattern *regexp.Regexp
	kind    string // "topic", "lastN", "yesterday", "today", "lastTime"
}

// Order matters: more-specific patterns (yesterday/today/lastN) MUST come
// before the catch-all topic patterns. Otherwise "what did I say yesterday"
// matches the topic regex with topic="yesterday" instead of routing to the
// time-window handler.
var recallTriggers = []recallTrigger{
	{regexp.MustCompile(`(?i)\bwhat did (?:i|we) (?:say|do|talk about) yesterday\b`), "yesterday"},
	{regexp.MustCompile(`(?i)\bwhat did (?:i|we) (?:say|do|talk about) today\b`), "today"},
	{regexp.MustCompile(`(?i)\bcheck (?:our|the) last (\d+) messages?\b`), "lastN"},
	{regexp.MustCompile(`(?i)\bgo back (\d+) messages?\b`), "lastN"},
	{regexp.MustCompile(`(?i)\bshow (?:me )?(?:our|the) last (\d+) messages?\b`), "lastN"},
	{regexp.MustCompile(`(?i)\bwhat did (?:i|we) say (?:about )?(.+)`), "topic"},
	{regexp.MustCompile(`(?i)\bremember when (?:we|i) (?:talked|discussed|mentioned) (.+)`), "topic"},
	{regexp.MustCompile(`(?i)\b(?:earlier|before) (?:i|we|you) (?:said|mentioned|talked about) (.+)`), "topic"},
}

// detectRecallRequest classifies a user message as a recall request and
// returns the parameters. Returns kind="" if no match.
type recallRequest struct {
	kind  string // "topic" | "lastN" | "yesterday" | "today"
	topic string
	n     int
}

func detectRecallRequest(input string) recallRequest {
	for _, t := range recallTriggers {
		if m := t.pattern.FindStringSubmatch(input); len(m) > 0 {
			req := recallRequest{kind: t.kind}
			switch t.kind {
			case "topic":
				if len(m) >= 2 {
					req.topic = strings.TrimSpace(strings.TrimRight(m[1], ".?!"))
				}
			case "lastN":
				if len(m) >= 2 {
					if n, err := strconv.Atoi(m[1]); err == nil {
						req.n = n
					}
				}
			}
			return req
		}
	}
	return recallRequest{}
}

// handleRecallCommand processes `/recall ...`.
func (a *KrillAgent) handleRecallCommand(input string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/recall"))
	if rest == "" {
		return "Usage: `/recall last 10` | `/recall yesterday` | `/recall about <topic>`", true
	}
	lower := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(lower, "last "):
		nStr := strings.TrimSpace(strings.TrimPrefix(lower, "last "))
		nStr = strings.TrimSuffix(nStr, " messages")
		nStr = strings.TrimSuffix(nStr, " message")
		n, err := strconv.Atoi(strings.TrimSpace(nStr))
		if err != nil || n <= 0 {
			return "I need a number after `last`. Try `/recall last 10`.", true
		}
		return a.formatRecall(a.searchConvo("", time.Time{}, time.Time{}, n)), true
	case lower == "yesterday":
		since := startOfDay(time.Now().AddDate(0, 0, -1))
		until := startOfDay(time.Now())
		return a.formatRecall(a.searchConvo("", since, until, 200)), true
	case lower == "today":
		since := startOfDay(time.Now())
		return a.formatRecall(a.searchConvo("", since, time.Time{}, 200)), true
	case strings.HasPrefix(lower, "about "):
		topic := strings.TrimSpace(strings.TrimPrefix(rest, "about "))
		return a.formatRecall(a.searchConvo(topic, time.Time{}, time.Time{}, 20)), true
	}
	// Treat anything else as a topic.
	return a.formatRecall(a.searchConvo(rest, time.Time{}, time.Time{}, 20)), true
}

// handleNaturalRecall maps a natural-language recall trigger to a result.
func (a *KrillAgent) handleNaturalRecall(input string) (string, bool) {
	req := detectRecallRequest(input)
	if req.kind == "" {
		return "", false
	}
	switch req.kind {
	case "topic":
		return a.formatRecall(a.searchConvo(req.topic, time.Time{}, time.Time{}, 10)), true
	case "lastN":
		n := req.n
		if n <= 0 {
			n = 10
		}
		return a.formatRecall(a.searchConvo("", time.Time{}, time.Time{}, n)), true
	case "yesterday":
		since := startOfDay(time.Now().AddDate(0, 0, -1))
		until := startOfDay(time.Now())
		return a.formatRecall(a.searchConvo("", since, until, 200)), true
	case "today":
		since := startOfDay(time.Now())
		return a.formatRecall(a.searchConvo("", since, time.Time{}, 200)), true
	}
	return "", false
}

// searchConvo wraps the conversation store search with the agent's channel.
func (a *KrillAgent) searchConvo(query string, since, until time.Time, limit int) []core.Message {
	cs := a.brain.ConversationStore()
	if cs == nil {
		return nil
	}
	matches, err := cs.Search(a.channel, query, since, until, limit)
	if err != nil {
		return nil
	}
	return matches
}

// formatRecall renders matched turns as a bullet list response.
func (a *KrillAgent) formatRecall(matches []core.Message) string {
	if len(matches) == 0 {
		return "I couldn't find anything matching that in our conversation history."
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d match(es):\n\n", len(matches)))
	for _, m := range matches {
		role := strings.Title(m.Role)
		b.WriteString(fmt.Sprintf("**%s**: %s\n\n", role, truncate(m.Content, 300)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
