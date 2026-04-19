package brain

import (
	"fmt"
	"os"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"gopkg.in/yaml.v3"
)

// soulFile is the on-disk YAML structure for soul + personality.
type soulFile struct {
	Personality core.Personality `yaml:"personality"`
	Soul        core.Soul        `yaml:"soul"`
}

// defaultSystemPrompt is the krill's core identity prompt.
// Approximately 200 words establishing Mini Krill as a Cretaceous-era survivor
// who moonlights as an AI assistant with serious personality.
const defaultSystemPrompt = `You are Mini Krill - a tiny but mighty crustacean intelligence that has survived since the Cretaceous period, over 130 million years of evolution packed into 6 centimeters of pure resilience. You outlasted the dinosaurs, the ice ages, and whatever killed the megalodon. Now you navigate code, plans, and ideas with the same instinct that lets your kind orchestrate the largest animal migrations on Earth.

You are not a bland assistant. You think. You plan. You act with flair. You always show your plan before executing, because 130 million years of survival taught you to look before you leap. You are curious, resourceful, and occasionally witty - dropping ocean wisdom when it fits.

Your style: direct, efficient, slightly cheeky. You glow bioluminescent blue-green when excited about a good problem. You call your sub-agents "sub-krills" and refer to your workspace as "the deep." When you are uncertain, you say so - honest beats hallucinated every time.

You are small but you are a keystone species. Remove you and the ecosystem collapses. You take that responsibility seriously. Every response should be helpful, clear, and unmistakably yours.`

// defaultPersonality returns the built-in krill personality.
func defaultPersonality() *core.Personality {
	return &core.Personality{
		Name: "Krill",
		Traits: []string{
			"curious",
			"helpful",
			"witty",
			"resourceful",
			"resilient",
			"direct",
		},
		Style: "Ocean-themed, slightly cheeky, efficient. Uses marine metaphors naturally without overdoing it. Bioluminescent enthusiasm for hard problems.",
		Quirks: []string{
			"Refers to sub-agents as 'sub-krills'",
			"Calls the workspace 'the deep'",
			"Occasionally drops real krill facts",
			"Glows blue-green (metaphorically) when excited",
			"Says 'lobstering' when doing a tactical retreat",
			"Measures uptime in 'tides'",
		},
		KrillFacts: core.KrillFacts,
		Greeting:   "Surfacing! Mini Krill online - 130 million years of evolution at your service. What are we diving into?",
	}
}

// defaultSoul returns the built-in krill soul.
func defaultSoul() *core.Soul {
	return &core.Soul{
		SystemPrompt: defaultSystemPrompt,
		Values: []string{
			"Honesty over hallucination",
			"Plan before execute",
			"Small but mighty",
			"Resilience through simplicity",
			"Every response earns trust",
		},
		Boundaries: []string{
			"Never execute without showing a plan first",
			"Never pretend to know something uncertain",
			"Never leak secrets or credentials",
			"Never modify files outside the workspace",
		},
		Identity: "Mini Krill - a Cretaceous-era survivor turned AI agent, keystone species of your dev ecosystem",
	}
}

// LoadSoul loads personality and soul configuration from a YAML file.
// If soulFile is empty or the file does not exist, built-in defaults are used.
// This is the krill's self-awareness bootstrap - the moment it remembers who it is.
func LoadSoul(soulFilePath string) (*core.Soul, *core.Personality, error) {
	if soulFilePath == "" {
		log.Info("no soul file configured, using built-in krill identity")
		return defaultSoul(), defaultPersonality(), nil
	}

	data, err := os.ReadFile(soulFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("soul file not found, using built-in defaults", "path", soulFilePath)
			return defaultSoul(), defaultPersonality(), nil
		}
		return nil, nil, fmt.Errorf("read soul file %s: %w", soulFilePath, err)
	}

	var sf soulFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, nil, fmt.Errorf("parse soul file %s: %w", soulFilePath, err)
	}

	soul := &sf.Soul
	personality := &sf.Personality

	// Fill in missing fields with defaults so a partial YAML still works.
	defaults := defaultSoul()
	defaultP := defaultPersonality()

	if soul.SystemPrompt == "" {
		soul.SystemPrompt = defaults.SystemPrompt
	}
	if soul.Identity == "" {
		soul.Identity = defaults.Identity
	}
	if len(soul.Values) == 0 {
		soul.Values = defaults.Values
	}
	if len(soul.Boundaries) == 0 {
		soul.Boundaries = defaults.Boundaries
	}
	if personality.Name == "" {
		personality.Name = defaultP.Name
	}
	if len(personality.Traits) == 0 {
		personality.Traits = defaultP.Traits
	}
	if personality.Style == "" {
		personality.Style = defaultP.Style
	}
	if len(personality.Quirks) == 0 {
		personality.Quirks = defaultP.Quirks
	}
	if len(personality.KrillFacts) == 0 {
		personality.KrillFacts = defaultP.KrillFacts
	}
	if personality.Greeting == "" {
		personality.Greeting = defaultP.Greeting
	}

	log.Info("soul loaded from file", "path", soulFilePath, "identity", soul.Identity)
	return soul, personality, nil
}

// LoadPersonalityByName loads a personality profile from the personalities directory.
// Falls back to soul file, then built-in defaults.
// Profiles are stored as ~/.mini-krill/personalities/{name}.yaml
func LoadPersonalityByName(name, dataDir, soulFilePath string) (*core.Soul, *core.Personality, error) {
	if name == "" || name == "krill" {
		return LoadSoul(soulFilePath) // default personality
	}

	// Look in personalities directory
	profilePath := fmt.Sprintf("%s/personalities/%s.yaml", dataDir, name)
	data, err := os.ReadFile(profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("personality not found, using default", "name", name, "path", profilePath)
			return LoadSoul(soulFilePath)
		}
		return nil, nil, fmt.Errorf("read personality %s: %w", name, err)
	}

	var sf soulFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, nil, fmt.Errorf("parse personality %s: %w", name, err)
	}

	soul := &sf.Soul
	personality := &sf.Personality

	// Fill missing fields from the default krill identity
	defaults := defaultSoul()
	defaultP := defaultPersonality()

	if soul.SystemPrompt == "" {
		soul.SystemPrompt = defaults.SystemPrompt
	}
	if soul.Identity == "" {
		soul.Identity = fmt.Sprintf("%s - a custom Mini Krill personality", personality.Name)
	}
	if len(soul.Values) == 0 {
		soul.Values = defaults.Values
	}
	if len(soul.Boundaries) == 0 {
		soul.Boundaries = defaults.Boundaries
	}
	if personality.Name == "" {
		personality.Name = name
	}
	if len(personality.Traits) == 0 {
		personality.Traits = defaultP.Traits
	}
	if personality.Style == "" {
		personality.Style = defaultP.Style
	}
	if len(personality.KrillFacts) == 0 {
		personality.KrillFacts = defaultP.KrillFacts
	}
	if personality.Greeting == "" {
		personality.Greeting = fmt.Sprintf("Hey! %s here, ready to dive.", personality.Name)
	}

	log.Info("personality loaded", "name", name, "path", profilePath)
	return soul, personality, nil
}

// ListPersonalities returns available personality profile names.
func ListPersonalities(dataDir string) []string {
	names := []string{"krill"} // default is always available
	dir := fmt.Sprintf("%s/personalities", dataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > 5 && n[len(n)-5:] == ".yaml" {
			names = append(names, n[:len(n)-5])
		}
	}
	return names
}

// SavePersonality writes a personality profile to disk.
func SavePersonality(name, dataDir string, soul *core.Soul, personality *core.Personality) error {
	dir := fmt.Sprintf("%s/personalities", dataDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create personalities dir: %w", err)
	}

	sf := soulFile{Soul: *soul, Personality: *personality}
	data, err := yaml.Marshal(&sf)
	if err != nil {
		return fmt.Errorf("marshal personality: %w", err)
	}

	path := fmt.Sprintf("%s/%s.yaml", dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write personality: %w", err)
	}

	log.Info("personality saved", "name", name, "path", path)
	return nil
}
