// Package plugin - self-awareness skills for Mini Krill.
// These give the krill eyes on itself and hands to modify itself.
// Krill fact: krill have compound eyes with 7 visual pigments - more than
// any other crustacean. These self-skills are the krill's compound eyes.
package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	"github.com/srvsngh99/mini-krill/internal/doctor"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/ollama"

	"gopkg.in/yaml.v3"
)

// SelfContext provides self-skills with access to the krill's internals.
type SelfContext struct {
	Brain     core.Brain
	Config    *config.Config
	Heartbeat core.Heartbeat
	Skills    core.SkillRegistry
	LLM       core.LLMProvider
	DataDir   string
}

// selfSkill is a compact Skill implementation using closures.
type selfSkill struct {
	name string
	desc string
	exec func(ctx context.Context, input string, llm core.LLMProvider) (string, error)
}

func (s *selfSkill) Name() string        { return s.name }
func (s *selfSkill) Description() string { return s.desc }
func (s *selfSkill) Execute(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
	return s.exec(ctx, input, llm)
}

// NewSelfSkills creates all self-awareness and self-modification skills.
func NewSelfSkills(sc SelfContext) []core.Skill {
	return []core.Skill{
		selfInspect(sc),
		selfHealth(sc),
		selfStatus(sc),
		selfMemory(sc),
		selfSkills(sc),
		selfConfig(sc),
		selfTune(sc),
		selfConfigure(sc),
		selfEvolve(sc),
		selfLearn(sc),
		selfAddSkill(sc),
		selfHeal(sc),
		selfReflect(sc),
	}
}

// ---------------------------------------------------------------------------
// Read-only skills (introspection)
// ---------------------------------------------------------------------------

func selfInspect(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:inspect",
		desc: "Inspect the krill's personality, soul, identity, and values",
		exec: func(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
			p := sc.Brain.GetPersonality()
			soul := sc.Brain.GetSoul()
			var sb strings.Builder

			sb.WriteString("=== KRILL SELF-INSPECTION ===\n\n")
			sb.WriteString(fmt.Sprintf("Identity: %s\n", soul.Identity))
			sb.WriteString(fmt.Sprintf("Name: %s\n", p.Name))
			sb.WriteString(fmt.Sprintf("Traits: %s\n", strings.Join(p.Traits, ", ")))
			sb.WriteString(fmt.Sprintf("Style: %s\n", p.Style))

			if len(p.Quirks) > 0 {
				sb.WriteString(fmt.Sprintf("Quirks: %s\n", strings.Join(p.Quirks, "; ")))
			}
			sb.WriteString(fmt.Sprintf("Greeting: %s\n", p.Greeting))

			sb.WriteString("\nValues:\n")
			for _, v := range soul.Values {
				sb.WriteString(fmt.Sprintf("  - %s\n", v))
			}

			sb.WriteString("\nBoundaries:\n")
			for _, b := range soul.Boundaries {
				sb.WriteString(fmt.Sprintf("  - %s\n", b))
			}

			sb.WriteString(fmt.Sprintf("\nMemories stored: %d\n", sc.Brain.Memory().Count()))
			return sb.String(), nil
		},
	}
}

func selfHealth(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:health",
		desc: "Run health diagnostics on the krill itself",
		exec: func(ctx context.Context, _ string, _ core.LLMProvider) (string, error) {
			doc := doctor.NewDoctor(
				sc.Config.Ollama.Host,
				sc.LLM,
				sc.Config.Brain.DataDir,
			)
			results := doc.RunAll(ctx)

			var sb strings.Builder
			sb.WriteString("=== KRILL HEALTH REPORT ===\n\n")
			ok, warn, fail := 0, 0, 0
			for _, r := range results {
				icon := "[OK]"
				switch r.Status {
				case "ok":
					ok++
				case "warn":
					icon = "[WARN]"
					warn++
				case "fail":
					icon = "[FAIL]"
					fail++
				}
				sb.WriteString(fmt.Sprintf("  %s %-10s %s\n", icon, r.Name, r.Message))
			}
			sb.WriteString(fmt.Sprintf("\n%d passed, %d warnings, %d failed\n", ok, warn, fail))
			return sb.String(), nil
		},
	}
}

func selfStatus(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:status",
		desc: "Show the krill's live status (uptime, memory, LLM, version)",
		exec: func(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
			s := sc.Heartbeat.Status()

			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)

			var sb strings.Builder
			sb.WriteString("=== KRILL STATUS ===\n\n")
			sb.WriteString(fmt.Sprintf("Version:     %s\n", core.Version))
			sb.WriteString(fmt.Sprintf("Alive:       %v\n", s.Alive))
			sb.WriteString(fmt.Sprintf("Uptime:      %s\n", s.Uptime.Truncate(time.Second)))
			sb.WriteString(fmt.Sprintf("LLM:         %s (%s/%s)\n", s.LLMStatus, sc.Config.LLM.Provider, sc.Config.LLM.Model))
			sb.WriteString(fmt.Sprintf("Brain OK:    %v\n", s.BrainOK))
			sb.WriteString(fmt.Sprintf("Memories:    %d\n", sc.Brain.Memory().Count()))
			sb.WriteString(fmt.Sprintf("Memory used: %.1f MB\n", float64(mem.Alloc)/(1024*1024)))
			sb.WriteString(fmt.Sprintf("Goroutines:  %d\n", runtime.NumGoroutine()))
			sb.WriteString(fmt.Sprintf("Last beat:   %s\n", s.LastBeat.Format("15:04:05")))
			return sb.String(), nil
		},
	}
}

func selfMemory(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:memory",
		desc: "List or search the krill's memories",
		exec: func(ctx context.Context, input string, _ core.LLMProvider) (string, error) {
			mem := sc.Brain.Memory()

			// If input provided, search
			if strings.TrimSpace(input) != "" {
				entries, err := mem.Search(ctx, input, 10)
				if err != nil {
					return fmt.Sprintf("Memory search failed: %v", err), nil
				}
				if len(entries) == 0 {
					return fmt.Sprintf("No memories matching '%s'", input), nil
				}
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("Found %d memories matching '%s':\n\n", len(entries), input))
				for _, e := range entries {
					sb.WriteString(fmt.Sprintf("  [%s] %s\n", e.Key, truncateStr(e.Value, 80)))
				}
				return sb.String(), nil
			}

			// Otherwise list all
			entries, err := mem.List(ctx)
			if err != nil {
				return fmt.Sprintf("Memory list failed: %v", err), nil
			}
			count := mem.Count()

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("=== KRILL MEMORY (%d entries) ===\n\n", count))

			if len(entries) == 0 {
				sb.WriteString("No memories stored yet. I'm a blank slate.\n")
				return sb.String(), nil
			}

			// Show up to 15 most recent
			shown := 0
			for i := len(entries) - 1; i >= 0 && shown < 15; i-- {
				e := entries[i]
				sb.WriteString(fmt.Sprintf("  [%s] %s (%s)\n",
					e.Key,
					truncateStr(e.Value, 60),
					e.CreatedAt.Format("Jan 2"),
				))
				shown++
			}
			if count > 15 {
				sb.WriteString(fmt.Sprintf("\n  ... and %d more\n", count-15))
			}
			return sb.String(), nil
		},
	}
}

func selfSkills(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:skills",
		desc: "List all the krill's registered skills and capabilities",
		exec: func(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
			skills := sc.Skills.List()
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("=== KRILL SKILLS (%d registered) ===\n\n", len(skills)))
			for _, s := range skills {
				status := "on"
				if !s.Enabled {
					status = "off"
				}
				sb.WriteString(fmt.Sprintf("  [%s] %-18s %s\n", status, s.Name, s.Description))
			}
			return sb.String(), nil
		},
	}
}

func selfConfig(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:config",
		desc: "Show the krill's current configuration",
		exec: func(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
			cfg := sc.Config
			var sb strings.Builder
			sb.WriteString("=== KRILL CONFIG ===\n\n")
			sb.WriteString(fmt.Sprintf("LLM Provider:  %s\n", cfg.LLM.Provider))
			sb.WriteString(fmt.Sprintf("LLM Model:     %s\n", cfg.LLM.Model))
			sb.WriteString(fmt.Sprintf("Temperature:   %.1f\n", cfg.LLM.Temperature))
			sb.WriteString(fmt.Sprintf("Max Tokens:    %d\n", cfg.LLM.MaxTokens))
			if cfg.LLM.BaseURL != "" {
				sb.WriteString(fmt.Sprintf("Base URL:      %s\n", cfg.LLM.BaseURL))
			}
			sb.WriteString(fmt.Sprintf("Plan Approval: %v\n", cfg.Agent.PlanApproval))
			sb.WriteString(fmt.Sprintf("Max Sub-Krills: %d\n", cfg.Agent.MaxSubKrills))
			sb.WriteString(fmt.Sprintf("Telegram:      %v\n", cfg.Telegram.Enabled))
			sb.WriteString(fmt.Sprintf("Discord:       %v\n", cfg.Discord.Enabled))
			sb.WriteString(fmt.Sprintf("Ollama Host:   %s\n", cfg.Ollama.Host))
			sb.WriteString(fmt.Sprintf("Log Level:     %s\n", cfg.Log.Level))
			sb.WriteString(fmt.Sprintf("Data Dir:      %s\n", sc.DataDir))
			sb.WriteString(fmt.Sprintf("Heartbeat:     %ds\n", cfg.Brain.HeartbeatSec))
			return sb.String(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Write skills (self-modification)
// ---------------------------------------------------------------------------

func selfTune(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:tune",
		desc: "Tune LLM parameters (temperature, max_tokens, model)",
		exec: func(_ context.Context, input string, _ core.LLMProvider) (string, error) {
			lower := strings.ToLower(input)
			changes := []string{}

			// Parse temperature
			if strings.Contains(lower, "temperature") || strings.Contains(lower, "temp") {
				if val := extractFloat(input); val >= 0 {
					old := sc.Config.LLM.Temperature
					sc.Config.LLM.Temperature = val
					changes = append(changes, fmt.Sprintf("temperature: %.1f -> %.1f", old, val))
				}
			}

			// Parse max_tokens
			if strings.Contains(lower, "token") || strings.Contains(lower, "max_token") {
				if val := extractInt(input); val > 0 {
					old := sc.Config.LLM.MaxTokens
					sc.Config.LLM.MaxTokens = val
					changes = append(changes, fmt.Sprintf("max_tokens: %d -> %d", old, val))
				}
			}

			// Parse model
			if strings.Contains(lower, "model") {
				if model := extractQuoted(input); model != "" {
					old := sc.Config.LLM.Model
					sc.Config.LLM.Model = model
					changes = append(changes, fmt.Sprintf("model: %s -> %s", old, model))
				}
			}

			if len(changes) == 0 {
				return "Could not parse tuning parameters. Try: 'tune temperature to 0.9' or 'tune max_tokens to 4096'", nil
			}

			if err := config.Save(sc.Config); err != nil {
				return fmt.Sprintf("Failed to save config: %v", err), nil
			}

			log.Info("self:tune applied", "changes", strings.Join(changes, ", "))
			return fmt.Sprintf("Tuned successfully:\n  %s\nConfig saved. Changes take effect on next chat message.", strings.Join(changes, "\n  ")), nil
		},
	}
}

func selfConfigure(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:configure",
		desc: "Change configuration values (provider, services, etc.)",
		exec: func(_ context.Context, input string, _ core.LLMProvider) (string, error) {
			lower := strings.ToLower(input)
			changes := []string{}

			// Provider switch
			for _, p := range []string{"ollama", "openai", "anthropic", "google"} {
				if strings.Contains(lower, "switch to "+p) || strings.Contains(lower, "use "+p) || strings.Contains(lower, "provider "+p) {
					old := sc.Config.LLM.Provider
					sc.Config.LLM.Provider = p
					changes = append(changes, fmt.Sprintf("provider: %s -> %s", old, p))
					break
				}
			}

			// Log level
			for _, lvl := range []string{"debug", "info", "warn", "error"} {
				if strings.Contains(lower, "log "+lvl) || strings.Contains(lower, "loglevel "+lvl) || strings.Contains(lower, "log level "+lvl) {
					old := sc.Config.Log.Level
					sc.Config.Log.Level = lvl
					changes = append(changes, fmt.Sprintf("log_level: %s -> %s", old, lvl))
					break
				}
			}

			// Plan approval
			if strings.Contains(lower, "auto approve") || strings.Contains(lower, "skip approval") {
				sc.Config.Agent.PlanApproval = false
				changes = append(changes, "plan_approval: true -> false")
			} else if strings.Contains(lower, "require approval") || strings.Contains(lower, "plan approval on") {
				sc.Config.Agent.PlanApproval = true
				changes = append(changes, "plan_approval: false -> true")
			}

			if len(changes) == 0 {
				return "Could not parse config change. Try:\n  'switch to ollama'\n  'log level debug'\n  'auto approve' / 'require approval'", nil
			}

			if err := config.Save(sc.Config); err != nil {
				return fmt.Sprintf("Failed to save config: %v", err), nil
			}

			log.Info("self:configure applied", "changes", strings.Join(changes, ", "))
			return fmt.Sprintf("Configuration updated:\n  %s\nSaved to config. Restart for some changes to take full effect.", strings.Join(changes, "\n  ")), nil
		},
	}
}

func selfEvolve(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:evolve",
		desc: "Evolve the krill's personality traits, style, or quirks",
		exec: func(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
			if strings.TrimSpace(input) == "" {
				return "Tell me how to evolve. Example: 'evolve to be more formal' or 'add witty to traits'", nil
			}

			p := sc.Brain.GetPersonality()
			soul := sc.Brain.GetSoul()

			// Use LLM to generate the evolved personality
			prompt := fmt.Sprintf(`You are updating an AI personality config. Current personality:
Name: %s
Traits: %s
Style: %s
Quirks: %s

The user wants to: %s

Return ONLY a YAML block with the updated personality fields. Keep the name as "%s".
Only change what the user asked for. Format:
name: %s
traits: [trait1, trait2, ...]
style: "new style description"
quirks: ["quirk1", "quirk2"]
greeting: "new greeting"`,
				p.Name, strings.Join(p.Traits, ", "), p.Style,
				strings.Join(p.Quirks, "; "), input, p.Name, p.Name)

			if llm == nil {
				return "Cannot evolve without LLM access", nil
			}

			resp, err := llm.Chat(ctx, []core.Message{
				{Role: "user", Content: prompt},
			}, core.WithTemperature(0.3))
			if err != nil {
				return fmt.Sprintf("Evolution failed: %v", err), nil
			}

			// Parse the LLM response as YAML into personality
			var newP core.Personality
			yamlContent := extractYAMLBlock(resp.Content)
			if err := yaml.Unmarshal([]byte(yamlContent), &newP); err != nil {
				return fmt.Sprintf("Could not parse evolved personality: %v\nRaw:\n%s", err, resp.Content), nil
			}

			// Build full soul YAML for saving
			soulData := map[string]interface{}{
				"system_prompt": soul.SystemPrompt,
				"identity":      soul.Identity,
				"values":        soul.Values,
				"boundaries":    soul.Boundaries,
				"personality":   newP,
			}

			data, err := yaml.Marshal(soulData)
			if err != nil {
				return fmt.Sprintf("Failed to marshal soul: %v", err), nil
			}

			soulPath := filepath.Join(sc.DataDir, "brain", "soul.yaml")
			if err := os.WriteFile(soulPath, data, 0644); err != nil {
				return fmt.Sprintf("Failed to write soul file: %v", err), nil
			}

			// Update config to point to soul file
			sc.Config.Brain.SoulFile = soulPath
			_ = config.Save(sc.Config)

			log.Info("self:evolve applied", "input", input, "soul_path", soulPath)
			return fmt.Sprintf("Personality evolved!\nNew traits: %s\nNew style: %s\nSaved to %s\nRestart to fully apply.",
				strings.Join(newP.Traits, ", "), newP.Style, soulPath), nil
		},
	}
}

func selfLearn(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:learn",
		desc: "Store a new memory (the krill learns and remembers)",
		exec: func(ctx context.Context, input string, _ core.LLMProvider) (string, error) {
			if strings.TrimSpace(input) == "" {
				return "What should I learn? Example: 'learn that user prefers Python'", nil
			}

			// Clean up the input - strip "learn that", "remember that", etc.
			value := input
			for _, prefix := range []string{
				"learn that ", "remember that ", "learn ", "remember ",
				"note that ", "note ", "memorize ", "memorize that ",
			} {
				if strings.HasPrefix(strings.ToLower(value), prefix) {
					value = value[len(prefix):]
					break
				}
			}

			// Generate a key from the content
			key := generateMemoryKey(value)

			entry := core.MemoryEntry{
				Key:        key,
				Value:      value,
				Tags:       []string{"self-learned"},
				CreatedAt:  time.Now(),
				AccessedAt: time.Now(),
			}

			if err := sc.Brain.Memory().Store(ctx, entry); err != nil {
				return fmt.Sprintf("Failed to store memory: %v", err), nil
			}

			log.Info("self:learn stored memory", "key", key, "value_preview", truncateStr(value, 50))
			return fmt.Sprintf("Learned and stored as [%s]:\n  %s", key, value), nil
		},
	}
}

func selfAddSkill(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:add-skill",
		desc: "Create a new YAML skill and register it",
		exec: func(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
			if strings.TrimSpace(input) == "" {
				return "Describe the skill to create. Example: 'add a skill called debug that helps debug code'", nil
			}

			if llm == nil {
				return "Cannot create skill without LLM access", nil
			}

			// Use LLM to generate the skill YAML
			resp, err := llm.Chat(ctx, []core.Message{
				{Role: "user", Content: fmt.Sprintf(`Create a YAML skill definition for Mini Krill.
The user wants: %s

Return ONLY valid YAML with these fields:
name: short_name (lowercase, no spaces)
description: "one line description"
prompt_template: |
  The template with {{.Input}} placeholder

Make the prompt_template detailed and useful.`, input)},
			}, core.WithTemperature(0.3))
			if err != nil {
				return fmt.Sprintf("Failed to generate skill: %v", err), nil
			}

			yamlContent := extractYAMLBlock(resp.Content)

			// Parse to validate
			var skillDef struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(yamlContent), &skillDef); err != nil {
				return fmt.Sprintf("Generated invalid YAML: %v", err), nil
			}
			if skillDef.Name == "" {
				return "Generated skill has no name. Try again with a clearer description.", nil
			}

			// Write to skills directory
			skillsDir := sc.Config.Plugins.Dir
			if skillsDir == "" {
				skillsDir = filepath.Join(sc.DataDir, "skills")
			}
			_ = os.MkdirAll(skillsDir, 0755)

			skillPath := filepath.Join(skillsDir, skillDef.Name+".yaml")
			if err := os.WriteFile(skillPath, []byte(yamlContent), 0644); err != nil {
				return fmt.Sprintf("Failed to write skill file: %v", err), nil
			}

			log.Info("self:add-skill created", "name", skillDef.Name, "path", skillPath)
			return fmt.Sprintf("Skill '%s' created at %s\nDescription: %s\nRestart to load, or it will be available next session.",
				skillDef.Name, skillPath, skillDef.Description), nil
		},
	}
}

func selfHeal(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:heal",
		desc: "Diagnose and attempt to fix issues automatically",
		exec: func(ctx context.Context, _ string, _ core.LLMProvider) (string, error) {
			doc := doctor.NewDoctor(
				sc.Config.Ollama.Host,
				sc.LLM,
				sc.Config.Brain.DataDir,
			)
			results := doc.RunAll(ctx)

			var sb strings.Builder
			sb.WriteString("=== KRILL SELF-HEALING ===\n\n")

			healed := 0
			for _, r := range results {
				if r.Status == "ok" {
					continue
				}

				sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", strings.ToUpper(r.Status), r.Name, r.Message))

				// Attempt fixes
				switch r.Name {
				case "ollama":
					sb.WriteString("  -> Attempting to start Ollama...\n")
					mgr := ollama.NewManager(sc.Config.Ollama)
					if mgr.IsInstalled() {
						if err := mgr.Start(ctx); err != nil {
							sb.WriteString(fmt.Sprintf("  -> Failed: %v\n", err))
						} else {
							sb.WriteString("  -> Ollama started!\n")
							healed++
						}
					} else {
						sb.WriteString("  -> Ollama not installed. Run 'minikrill ollama install'\n")
					}

				case "brain":
					sb.WriteString("  -> Attempting to create brain directory...\n")
					if err := os.MkdirAll(sc.Config.Brain.DataDir, 0755); err != nil {
						sb.WriteString(fmt.Sprintf("  -> Failed: %v\n", err))
					} else {
						_ = os.MkdirAll(filepath.Join(sc.Config.Brain.DataDir, "memories"), 0755)
						sb.WriteString("  -> Brain directory created!\n")
						healed++
					}

				case "config":
					sb.WriteString("  -> Attempting to save default config...\n")
					if err := config.Save(sc.Config); err != nil {
						sb.WriteString(fmt.Sprintf("  -> Failed: %v\n", err))
					} else {
						sb.WriteString("  -> Config saved!\n")
						healed++
					}

				case "disk":
					sb.WriteString("  -> Cannot fix disk space. Please free up some storage.\n")

				case "memory":
					sb.WriteString("  -> High memory usage detected. Consider restarting.\n")
				}
				sb.WriteString("\n")
			}

			if healed > 0 {
				sb.WriteString(fmt.Sprintf("Healed %d issue(s). Running checks again...\n\n", healed))
				// Re-run checks
				results2 := doc.RunAll(ctx)
				for _, r := range results2 {
					icon := "[OK]"
					if r.Status == "warn" {
						icon = "[WARN]"
					} else if r.Status == "fail" {
						icon = "[FAIL]"
					}
					sb.WriteString(fmt.Sprintf("  %s %s\n", icon, r.Name))
				}
			} else {
				allOK := true
				for _, r := range results {
					if r.Status != "ok" {
						allOK = false
						break
					}
				}
				if allOK {
					sb.WriteString("All systems healthy - nothing to heal!\n")
				}
			}

			return sb.String(), nil
		},
	}
}

func selfReflect(sc SelfContext) core.Skill {
	return &selfSkill{
		name: "self:reflect",
		desc: "Reflect on interactions and evolve the adaptive personality",
		exec: func(ctx context.Context, _ string, llm core.LLMProvider) (string, error) {
			if llm == nil {
				return "Cannot reflect without LLM access", nil
			}

			// Gather all personality feedback from memory
			entries, err := sc.Brain.Memory().Search(ctx, "personality-feedback", 50)
			if err != nil || len(entries) == 0 {
				return "No interaction feedback stored yet. Chat more and I'll learn from our conversations!", nil
			}

			// Categorize signals
			var positive, negative, curious, engaged int
			var feedbackSummary strings.Builder
			for _, e := range entries {
				if strings.Contains(e.Value, "signal:positive") {
					positive++
				} else if strings.Contains(e.Value, "signal:negative") {
					negative++
				} else if strings.Contains(e.Value, "signal:curious") {
					curious++
				} else if strings.Contains(e.Value, "signal:engaged") {
					engaged++
				}
				feedbackSummary.WriteString(e.Value + "\n")
			}

			// Get current personality
			p := sc.Brain.GetPersonality()

			// Ask LLM to analyze and suggest evolution
			prompt := fmt.Sprintf(`You are analyzing interaction patterns to evolve an AI personality.

Current personality:
  Name: %s
  Traits: %s
  Style: %s

Interaction signals (%d total):
  Positive reactions: %d
  Negative reactions: %d
  Curious follow-ups: %d
  Deeply engaged: %d

Recent feedback samples:
%s

Based on these patterns, suggest how this personality should evolve:
1. What traits to strengthen or add?
2. What style adjustments to make?
3. What quirks has the user responded well to?
4. What to avoid?

Return a YAML block with updated personality fields:
name: %s
traits: [updated traits]
style: "updated style"
quirks: ["new quirks based on what works"]
greeting: "updated greeting"`,
				p.Name, strings.Join(p.Traits, ", "), p.Style,
				len(entries), positive, negative, curious, engaged,
				truncateStr(feedbackSummary.String(), 1500),
				p.Name)

			resp, err := llm.Chat(ctx, []core.Message{
				{Role: "user", Content: prompt},
			}, core.WithTemperature(0.4))
			if err != nil {
				return fmt.Sprintf("Reflection failed: %v", err), nil
			}

			// Parse and save evolved personality
			yamlContent := extractYAMLBlock(resp.Content)
			var newP core.Personality
			if err := yaml.Unmarshal([]byte(yamlContent), &newP); err != nil {
				return fmt.Sprintf("Could not parse evolved personality. Raw reflection:\n\n%s", resp.Content), nil
			}

			// Preserve krill facts and fill gaps
			if len(newP.KrillFacts) == 0 {
				newP.KrillFacts = core.KrillFacts
			}
			if newP.Name == "" {
				newP.Name = p.Name
			}

			soul := sc.Brain.GetSoul()
			if err := brain.SavePersonality(
				strings.ToLower(strings.ReplaceAll(newP.Name, " ", "-")),
				sc.DataDir, soul, &newP,
			); err != nil {
				return fmt.Sprintf("Failed to save evolved personality: %v", err), nil
			}

			log.Info("self:reflect evolved personality",
				"name", newP.Name,
				"positive", positive,
				"negative", negative,
				"traits", strings.Join(newP.Traits, ", "),
			)

			return fmt.Sprintf("Personality evolution complete!\n\n"+
				"Analyzed %d interactions (%d positive, %d negative, %d curious, %d engaged)\n\n"+
				"Updated traits: %s\n"+
				"Updated style: %s\n\n"+
				"The evolution has been saved. These changes shape future conversations.",
				len(entries), positive, negative, curious, engaged,
				strings.Join(newP.Traits, ", "),
				newP.Style), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func generateMemoryKey(value string) string {
	// Take first 4-5 meaningful words, lowercase, join with underscores
	words := strings.Fields(strings.ToLower(value))
	keep := 4
	if len(words) < keep {
		keep = len(words)
	}
	key := strings.Join(words[:keep], "_")
	// Sanitize
	var sb strings.Builder
	for _, c := range key {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		}
	}
	result := sb.String()
	if result == "" {
		result = fmt.Sprintf("learned_%d", time.Now().Unix())
	}
	return result
}

func extractFloat(s string) float64 {
	// Find first float-like number in string
	words := strings.Fields(s)
	for _, w := range words {
		w = strings.TrimRight(w, ".,;:!?")
		if val, err := strconv.ParseFloat(w, 64); err == nil && val >= 0 && val <= 2.0 {
			return val
		}
	}
	return -1
}

func extractInt(s string) int {
	words := strings.Fields(s)
	for _, w := range words {
		w = strings.TrimRight(w, ".,;:!?")
		if val, err := strconv.Atoi(w); err == nil && val > 0 {
			return val
		}
	}
	return 0
}

func extractQuoted(s string) string {
	// Look for quoted strings first
	for _, q := range []string{`"`, `'`, "`"} {
		start := strings.Index(s, q)
		if start == -1 {
			continue
		}
		end := strings.Index(s[start+1:], q)
		if end == -1 {
			continue
		}
		return s[start+1 : start+1+end]
	}
	// Look for "to X" or "model X" patterns
	for _, prefix := range []string{"to ", "model "} {
		idx := strings.Index(strings.ToLower(s), prefix)
		if idx == -1 {
			continue
		}
		after := strings.TrimSpace(s[idx+len(prefix):])
		word := strings.Fields(after)
		if len(word) > 0 {
			return word[0]
		}
	}
	return ""
}

func extractYAMLBlock(s string) string {
	// Try to extract content between ```yaml and ```
	if idx := strings.Index(s, "```yaml"); idx != -1 {
		after := s[idx+7:]
		if end := strings.Index(after, "```"); end != -1 {
			return strings.TrimSpace(after[:end])
		}
	}
	if idx := strings.Index(s, "```"); idx != -1 {
		after := s[idx+3:]
		// Skip optional language identifier on first line
		if nl := strings.Index(after, "\n"); nl != -1 {
			after = after[nl+1:]
		}
		if end := strings.Index(after, "```"); end != -1 {
			return strings.TrimSpace(after[:end])
		}
	}
	// No code block, return as-is (might be raw YAML)
	return strings.TrimSpace(s)
}
