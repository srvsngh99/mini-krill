package brain

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// KrillBrain implements core.Brain, orchestrating memory, personality, soul,
// and heartbeat into one cohesive cognitive system.
type KrillBrain struct {
	memory      *FileMemory
	convStore   *ConversationStore
	soul        *core.Soul
	personality *core.Personality
	heartbeat   *KrillHeartbeat
	llm         core.LLMProvider
	cfg         config.BrainConfig
}

// New creates and initializes a KrillBrain from the given configuration.
// It creates required data directories, loads the soul (from YAML or defaults),
// initializes file-based memory, and sets up the heartbeat monitor.
func New(cfg config.BrainConfig, llm core.LLMProvider) (*KrillBrain, error) {
	// Ensure data directories exist - the krill needs a habitat
	memDir := filepath.Join(cfg.DataDir, "memories")
	dirs := []string{cfg.DataDir, memDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create brain dir %s: %w", d, err)
		}
	}

	// Load soul and personality - the krill awakens.
	// Uses personality name from agent config if available, otherwise soul file.
	soul, personality, err := LoadPersonalityByName(cfg.Personality, cfg.DataDir, cfg.SoulFile)
	if err != nil {
		return nil, fmt.Errorf("load soul: %w", err)
	}

	// Initialize file-based memory
	memory, err := NewFileMemory(memDir, cfg.MaxMemories)
	if err != nil {
		return nil, fmt.Errorf("init memory: %w", err)
	}

	// Initialize conversation store.
	convStore, err := NewConversationStore(filepath.Join(cfg.DataDir, "conversations.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("init conversation store: %w", err)
	}

	// Create heartbeat monitor
	hb := NewHeartbeat(cfg.HeartbeatSec, llm, cfg.DataDir)

	brain := &KrillBrain{
		memory:      memory,
		convStore:   convStore,
		soul:        soul,
		personality: personality,
		heartbeat:   hb,
		llm:         llm,
		cfg:         cfg,
	}

	log.Info("brain initialized",
		"identity", soul.Identity,
		"memories", memory.Count(),
		"heartbeat_sec", cfg.HeartbeatSec,
	)

	return brain, nil
}

// Memory returns the krill's persistent memory store.
func (b *KrillBrain) Memory() core.Memory {
	return b.memory
}

// ConversationStore returns the durable conversation turn store, or nil.
func (b *KrillBrain) ConversationStore() core.ConversationStore {
	if b.convStore == nil {
		return nil
	}
	return b.convStore
}

// Close releases resources held by the brain.
func (b *KrillBrain) Close() error {
	if b.convStore != nil {
		return b.convStore.Close()
	}
	return nil
}

// GetPersonality returns the krill's personality configuration.
func (b *KrillBrain) GetPersonality() *core.Personality {
	return b.personality
}

// GetSoul returns the krill's soul configuration.
func (b *KrillBrain) GetSoul() *core.Soul {
	return b.soul
}

// episodic builds a consolidator over this brain's stores. Cheap to construct
// per call (just wires three pointers) so there's no lifecycle to manage.
func (b *KrillBrain) episodic() *Episodic {
	return NewEpisodic(b.memory, b.ConversationStore(), b.llm)
}

// ConsolidateEpisode summarises the recent session on `channel` into a stored
// episode. Satisfies the agent's optional episodicBrain interface (#29 / D6).
func (b *KrillBrain) ConsolidateEpisode(ctx context.Context, channel string) error {
	_, err := b.episodic().Consolidate(ctx, channel)
	return err
}

// LatestEpisode returns the most recent episode younger than maxAge as an
// inject-ready, point-in-time context line, or "" when none qualifies.
func (b *KrillBrain) LatestEpisode(ctx context.Context, maxAge time.Duration) (string, error) {
	return b.episodic().Latest(ctx, maxAge)
}

// SystemPrompt returns the soul's system prompt string.
// This is the foundational instruction that shapes every LLM interaction.
func (b *KrillBrain) SystemPrompt() string {
	return b.soul.SystemPrompt
}

// EnrichMessages prepends the system prompt as the first message in a
// conversation. If the messages already start with a system message, it is
// replaced to ensure consistency.
func (b *KrillBrain) EnrichMessages(messages []core.Message) []core.Message {
	sysMsg := core.Message{
		Role:    "system",
		Content: b.soul.SystemPrompt,
	}

	if len(messages) > 0 && messages[0].Role == "system" {
		// Replace existing system message with our soul prompt
		enriched := make([]core.Message, len(messages))
		copy(enriched, messages)
		enriched[0] = sysMsg
		return enriched
	}

	// Prepend system message
	enriched := make([]core.Message, 0, len(messages)+1)
	enriched = append(enriched, sysMsg)
	enriched = append(enriched, messages...)
	return enriched
}

// RandomFact returns a random krill fact from the built-in collection.
// Perfect for idle moments, greeting messages, and loading screens.
func (b *KrillBrain) RandomFact() string {
	facts := core.KrillFacts
	if len(facts) == 0 {
		return "Krill are mysterious creatures."
	}
	return facts[rand.Intn(len(facts))]
}

// Heartbeat returns the heartbeat monitor, allowing callers to start health
// monitoring or register beat callbacks.
func (b *KrillBrain) Heartbeat() core.Heartbeat {
	return b.heartbeat
}
