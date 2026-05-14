// Package agent — provenance.go enforces source-honesty on assistant replies.
//
// The agent has no introspective sense of where its own claims came from.
// Without an external check, it can (and has — see PR #1 audit, turn 34 and
// turn 131) cite URLs it never fetched and reference search results that
// never ran. This file installs a lightweight post-processor that:
//
//  1. Asks the model to tag external claims with [web:url], [mem:key],
//     [trained], or [guess].
//  2. After generation, scans the reply for [web:...] tags and verifies that
//     a matching tool call actually ran in the same turn.
//  3. Strips unmatched tags and prepends a one-line warning so the user knows
//     a citation was fabricated.
//
// The verification is deliberately cheap and pessimistic — false positives
// (real fetches that the bookkeeper missed) cause a small disclaimer; false
// negatives let a fabrication slip through. We bias toward disclaiming.
package agent

import (
	"regexp"
	"strings"
	"sync"
)

// ProvenanceInstruction is the system-prompt addendum that asks the model to
// tag external claims. Injected by callers who care about provenance — chat
// and answer paths primarily.
const ProvenanceInstruction = `For any factual claim you draw from outside this conversation, append a bracket tag at the end of the sentence:
[web:<url>] for content from the search/web/youtube tools you used this turn
[mem:<key>] for content from your memory store
[trained] for general knowledge from your training
[guess] for reasoning beyond available evidence
Do not invent [web:...] tags for sources you did not actually fetch.`

// turnFetchLog records URLs the agent actually fetched during a single turn.
// The post-processor consults this when deciding whether a [web:...] tag in
// the reply is honest.
type turnFetchLog struct {
	mu   sync.Mutex
	urls map[string]bool
}

func newTurnFetchLog() *turnFetchLog {
	return &turnFetchLog{urls: make(map[string]bool)}
}

// Record marks a URL as fetched this turn. Call from any tool-call site that
// touched the web (search, web fetch, youtube transcript, etc.).
func (t *turnFetchLog) Record(url string) {
	if t == nil || url == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.urls[normalizeURL(url)] = true
}

// Contains returns true if url was recorded as fetched this turn.
func (t *turnFetchLog) Contains(url string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.urls[normalizeURL(url)]
}

// webTagPattern matches [web:<url>] occurrences in assistant output.
var webTagPattern = regexp.MustCompile(`\[web:([^\]]+)\]`)

// EnforceProvenance scans response text for [web:...] tags and verifies each
// against the turn's fetch log. Unmatched tags are stripped and the response
// is prepended with a one-line warning. Returns the cleaned response and the
// number of fabricated tags removed.
func EnforceProvenance(response string, log *turnFetchLog) (string, int) {
	if response == "" {
		return response, 0
	}
	matches := webTagPattern.FindAllStringSubmatchIndex(response, -1)
	if len(matches) == 0 {
		return response, 0
	}

	var b strings.Builder
	last := 0
	fabricated := 0
	for _, m := range matches {
		// m = [start, end, urlStart, urlEnd]
		start, end := m[0], m[1]
		url := response[m[2]:m[3]]
		b.WriteString(response[last:start])
		if log.Contains(url) {
			// Honest cite — keep the tag.
			b.WriteString(response[start:end])
		} else {
			// Fabricated cite — drop the tag entirely.
			fabricated++
		}
		last = end
	}
	b.WriteString(response[last:])
	cleaned := b.String()

	if fabricated > 0 {
		warn := "⚠ This response cited sources I did not actually fetch this turn. Tag(s) removed.\n\n"
		cleaned = warn + cleaned
	}
	return cleaned, fabricated
}

// normalizeURL canonicalises a URL for comparison. Strips trailing slash,
// protocol, and a leading "www." so a recorded "https://example.com/" matches
// a tag of "[web:example.com]".
func normalizeURL(u string) string {
	s := strings.TrimSpace(u)
	s = strings.TrimSuffix(s, "/")
	for _, prefix := range []string{"https://", "http://", "//"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimPrefix(s, "www.")
	return strings.ToLower(s)
}
