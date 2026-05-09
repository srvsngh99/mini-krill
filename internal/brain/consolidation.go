// Package brain — consolidation.go merges duplicate memories and stores
// structured reflections after task completion.
package brain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// MemoryConsolidator merges redundant memory entries and stores task reflections.
type MemoryConsolidator struct {
	memory *FileMemory
	llm    core.LLMProvider
}

// NewConsolidator creates a consolidator wired to a memory store and optional LLM.
func NewConsolidator(memory *FileMemory, llm core.LLMProvider) *MemoryConsolidator {
	return &MemoryConsolidator{memory: memory, llm: llm}
}

// Consolidate groups similar memory entries by scope and merges clusters.
// Returns the number of entries merged and removed.
func (c *MemoryConsolidator) Consolidate(ctx context.Context) (merged, removed int, err error) {
	entries, err := c.memory.List(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("list memories: %w", err)
	}

	if len(entries) < 2 {
		return 0, 0, nil
	}

	// Group by scope
	groups := make(map[string][]core.MemoryEntry)
	for _, e := range entries {
		scope := e.Scope
		if scope == "" {
			scope = "system"
		}
		groups[scope] = append(groups[scope], e)
	}

	for scope, scopeEntries := range groups {
		if len(scopeEntries) < 2 {
			continue
		}

		// Find clusters of similar entries (word overlap > 0.5)
		used := make(map[int]bool)
		for i := 0; i < len(scopeEntries); i++ {
			if used[i] {
				continue
			}
			cluster := []core.MemoryEntry{scopeEntries[i]}
			for j := i + 1; j < len(scopeEntries); j++ {
				if used[j] {
					continue
				}
				if wordOverlap(scopeEntries[i].Value, scopeEntries[j].Value) > 0.5 {
					cluster = append(cluster, scopeEntries[j])
					used[j] = true
				}
			}

			if len(cluster) < 2 {
				continue
			}

			// Merge the cluster
			mergedEntry, err := c.mergeCluster(ctx, scope, cluster)
			if err != nil {
				log.Warn("cluster merge failed", "scope", scope, "count", len(cluster), "error", err)
				continue
			}

			// Delete originals and store the merged entry
			for _, e := range cluster {
				if err := c.memory.Forget(ctx, e.Key); err != nil {
					log.Debug("forget during consolidation failed", "key", e.Key, "error", err)
				}
				removed++
			}

			if err := c.memory.Store(ctx, mergedEntry); err != nil {
				log.Warn("store consolidated entry failed", "error", err)
				continue
			}
			merged++

			log.Debug("cluster consolidated", "scope", scope, "from", len(cluster), "to", 1)
		}
	}

	log.Info("memory consolidation complete", "merged", merged, "removed", removed)
	return merged, removed, nil
}

// mergeCluster combines a cluster of similar entries into one.
// Uses the LLM if available, otherwise falls back to keeping the most recent entry
// with key facts appended from the others.
func (c *MemoryConsolidator) mergeCluster(ctx context.Context, scope string, cluster []core.MemoryEntry) (core.MemoryEntry, error) {
	// Find the most recently accessed entry as the base
	best := cluster[0]
	for _, e := range cluster[1:] {
		if e.AccessedAt.After(best.AccessedAt) {
			best = e
		}
	}

	// Try LLM synthesis
	if c.llm != nil {
		var parts []string
		for _, e := range cluster {
			parts = append(parts, fmt.Sprintf("- [%s] %s", e.Key, e.Value))
		}

		prompt := fmt.Sprintf(
			"Consolidate these %d related memory entries into ONE concise entry. "+
				"Preserve all important information. Return only the consolidated text, nothing else.\n\n%s",
			len(cluster), strings.Join(parts, "\n"),
		)

		resp, err := c.llm.Chat(ctx, []core.Message{{Role: "user", Content: prompt}},
			core.WithTemperature(0.2), core.WithMaxTokens(256))
		if err == nil && strings.TrimSpace(resp.Content) != "" {
			now := time.Now().UTC()
			return core.MemoryEntry{
				Key:         best.Key,
				Value:       strings.TrimSpace(resp.Content),
				Tags:        mergeTags(cluster),
				Scope:       scope,
				Source:      "consolidation",
				AccessCount: best.AccessCount,
				CreatedAt:   best.CreatedAt,
				AccessedAt:  now,
			}, nil
		}
		log.Debug("LLM consolidation failed, using fallback", "error", err)
	}

	// Fallback: keep the best entry and append unique facts from others
	var extras []string
	for _, e := range cluster {
		if e.Key == best.Key {
			continue
		}
		// Only append if it adds something not already in the best value
		if !strings.Contains(strings.ToLower(best.Value), strings.ToLower(e.Value)) {
			extras = append(extras, e.Value)
		}
	}

	value := best.Value
	if len(extras) > 0 {
		value += " | Also: " + strings.Join(extras, "; ")
	}

	now := time.Now().UTC()
	return core.MemoryEntry{
		Key:         best.Key,
		Value:       value,
		Tags:        mergeTags(cluster),
		Scope:       scope,
		Source:      "consolidation",
		AccessCount: best.AccessCount,
		CreatedAt:   best.CreatedAt,
		AccessedAt:  now,
	}, nil
}

// ReflectOnTask stores a structured reflection memory after a task completes.
func (c *MemoryConsolidator) ReflectOnTask(ctx context.Context, taskResult, taskDescription string) error {
	summary := taskResult
	if len(summary) > 500 {
		summary = summary[:500]
	}

	// Try LLM summary if available
	if c.llm != nil {
		prompt := fmt.Sprintf(
			"A task was just completed. Summarize what was learned in 2-3 sentences.\n\n"+
				"Task: %s\nResult: %s", taskDescription, summary)

		resp, err := c.llm.Chat(ctx, []core.Message{{Role: "user", Content: prompt}},
			core.WithTemperature(0.3), core.WithMaxTokens(128))
		if err == nil && strings.TrimSpace(resp.Content) != "" {
			summary = strings.TrimSpace(resp.Content)
		}
	}

	now := time.Now().UTC()
	entry := core.MemoryEntry{
		Key:        fmt.Sprintf("reflection_%d", now.UnixMilli()),
		Value:      fmt.Sprintf("Task: %s\nLearned: %s", taskDescription, summary),
		Tags:       []string{"task-outcome", "reflection"},
		Scope:      "task-outcome",
		Source:     "reflection",
		CreatedAt:  now,
		AccessedAt: now,
	}

	return c.memory.Store(ctx, entry)
}

// wordOverlap calculates the fraction of shared words between two strings.
func wordOverlap(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[w] = true
	}

	shared := 0
	for _, w := range wordsA {
		if setB[w] {
			shared++
		}
	}

	total := len(wordsA)
	if len(wordsB) > total {
		total = len(wordsB)
	}
	return float64(shared) / float64(total)
}

// mergeTags combines tags from all entries, deduplicating.
func mergeTags(entries []core.MemoryEntry) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, e := range entries {
		for _, t := range e.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	return tags
}
