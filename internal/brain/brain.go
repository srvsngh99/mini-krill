package brain

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// KrillBrain implements core.Brain, orchestrating memory, personality, soul,
// and heartbeat into one cohesive cognitive system.
type KrillBrain struct {
	memory      *FileMemory
	soul        *core.Soul
	personality *core.Personality
	heartbeat   *KrillHeartbeat
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

	// Create heartbeat monitor
	hb := NewHeartbeat(cfg.HeartbeatSec, llm, cfg.DataDir)

	brain := &KrillBrain{
		memory:      memory,
		soul:        soul,
		personality: personality,
		heartbeat:   hb,
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

// GetPersonality returns the krill's personality configuration.
func (b *KrillBrain) GetPersonality() *core.Personality {
	return b.personality
}

// GetSoul returns the krill's soul configuration.
func (b *KrillBrain) GetSoul() *core.Soul {
	return b.soul
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
