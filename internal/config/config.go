// Package config handles loading and merging Mini Krill configuration from
// YAML files and environment variables. Zero external dependencies beyond
// gopkg.in/yaml.v3 to keep the binary small.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for Mini Krill.
type Config struct {
	Agent    AgentConfig    `yaml:"agent"`
	LLM      LLMConfig      `yaml:"llm"`
	Brain    BrainConfig    `yaml:"brain"`
	Telegram TelegramConfig `yaml:"telegram"`
	Discord  DiscordConfig  `yaml:"discord"`
	Ollama   OllamaConfig   `yaml:"ollama"`
	Plugins  PluginsConfig  `yaml:"plugins"`
	MCP      MCPConfig      `yaml:"mcp"`
	Log      LogConfig      `yaml:"log"`
	Doctor   DoctorConfig   `yaml:"doctor"`
	TUI      TUIConfig      `yaml:"tui"`
}

// AgentConfig controls the main krill agent behaviour.
type AgentConfig struct {
	Name          string `yaml:"name"`
	Personality   string `yaml:"personality"` // active personality profile (default: "krill")
	MaxSubKrills  int    `yaml:"max_sub_krills"`
	PlanApproval  string `yaml:"-"` // legacy: "auto"|"always"|"never"; superseded by AutonomyFloor
	AutonomyFloor string `yaml:"-"` // "observe"|"suggest"|"act"|"evolve"; default "act"
	RecoveryTurns int    `yaml:"recovery_turns"`
}

// agentConfigRaw is used during YAML unmarshalling to accept plan_approval
// as either a bool (legacy) or string (new), and autonomy_floor as the new field.
type agentConfigRaw struct {
	Name          string      `yaml:"name"`
	Personality   string      `yaml:"personality"`
	MaxSubKrills  int         `yaml:"max_sub_krills"`
	PlanApproval  interface{} `yaml:"plan_approval"`
	AutonomyFloor string      `yaml:"autonomy_floor"`
	RecoveryTurns int         `yaml:"recovery_turns"`
}

// UnmarshalYAML handles backward-compatible parsing of plan_approval,
// which was a bool in earlier versions, became a 3-way string, and is now
// folded into AutonomyFloor. plan_approval is preserved during migration so
// users see one deprecation log line and then we move on.
func (a *AgentConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw agentConfigRaw
	if err := unmarshal(&raw); err != nil {
		return err
	}
	a.Name = raw.Name
	a.Personality = raw.Personality
	a.MaxSubKrills = raw.MaxSubKrills
	a.RecoveryTurns = raw.RecoveryTurns
	a.AutonomyFloor = raw.AutonomyFloor

	switch v := raw.PlanApproval.(type) {
	case bool:
		if v {
			a.PlanApproval = "always"
		} else {
			a.PlanApproval = "never"
		}
	case string:
		a.PlanApproval = v
	default:
		a.PlanApproval = ""
	}
	return nil
}

// MarshalYAML writes AutonomyFloor as the canonical autonomy knob. PlanApproval
// is no longer persisted — the migration in fillDefaults flips legacy values
// into AutonomyFloor on first load.
func (a AgentConfig) MarshalYAML() (interface{}, error) {
	return &struct {
		Name          string `yaml:"name"`
		Personality   string `yaml:"personality,omitempty"`
		MaxSubKrills  int    `yaml:"max_sub_krills"`
		AutonomyFloor string `yaml:"autonomy_floor"`
		RecoveryTurns int    `yaml:"recovery_turns,omitempty"`
	}{
		Name:          a.Name,
		Personality:   a.Personality,
		MaxSubKrills:  a.MaxSubKrills,
		AutonomyFloor: a.AutonomyFloor,
		RecoveryTurns: a.RecoveryTurns,
	}, nil
}

// LLMConfig selects and configures the LLM provider.
type LLMConfig struct {
	Provider    string  `yaml:"provider"` // ollama, codex, claude, openai, anthropic, google
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
	APIKey      string  `yaml:"api_key"`
	BaseURL     string  `yaml:"base_url"`
}

// BrainConfig controls memory, soul, and heartbeat.
type BrainConfig struct {
	DataDir       string `yaml:"data_dir"`
	SoulFile      string `yaml:"soul_file"`
	Personality   string `yaml:"personality"` // active personality profile name
	MaxMemories   int    `yaml:"max_memories"`
	HeartbeatSec  int    `yaml:"heartbeat_interval_sec"`
	RecoveryTurns int    `yaml:"recovery_turns"` // turns to load on cold start (default 10)
}

// TelegramConfig for the Telegram bot integration.
type TelegramConfig struct {
	Enabled     bool    `yaml:"enabled"`
	Token       string  `yaml:"token"`
	AllowedIDs  []int64 `yaml:"allowed_ids"`
	BotMaxTurns int     `yaml:"bot_max_turns"` // max bot-to-bot exchanges before waiting for human (0=unlimited, default 3)
}

// DiscordConfig for the Discord bot integration.
type DiscordConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Token     string `yaml:"token"`
	GuildID   string `yaml:"guild_id"`
	ChannelID string `yaml:"channel_id"`
}

// OllamaConfig for local Ollama management.
type OllamaConfig struct {
	Host         string `yaml:"host"`
	AutoInstall  bool   `yaml:"auto_install"`
	AutoStart    bool   `yaml:"auto_start"`
	DefaultModel string `yaml:"default_model"`
}

// PluginsConfig for the skill registry.
type PluginsConfig struct {
	Dir     string   `yaml:"dir"`
	Enabled []string `yaml:"enabled"`
}

// MCPConfig for MCP server connections.
type MCPConfig struct {
	Servers map[string]MCPServerConfig `yaml:"servers"`
}

// MCPServerConfig defines a single MCP server.
type MCPServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	Enabled bool              `yaml:"enabled"`
}

// LogConfig for structured logging.
type LogConfig struct {
	Level string `yaml:"level"` // debug, info, warn, error
	File  string `yaml:"file"`
	JSON  bool   `yaml:"json"`
}

// DoctorConfig lists which health checks to run.
type DoctorConfig struct {
	Checks []string `yaml:"checks"`
}

// TUIConfig for the terminal UI theme.
type TUIConfig struct {
	Theme string `yaml:"theme"` // ocean, deep, bioluminescent
}

// DataDir returns the mini-krill data directory (~/.mini-krill).
func DataDir() string {
	if d := os.Getenv("KRILL_DATA_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mini-krill"
	}
	return filepath.Join(home, ".mini-krill")
}

// DefaultConfig returns a Config with sensible defaults that work out of the box.
func DefaultConfig() *Config {
	dataDir := DataDir()
	return &Config{
		Agent: AgentConfig{
			Name:          "krill",
			MaxSubKrills:  3,
			PlanApproval:  "auto",
			AutonomyFloor: "act",
		},
		LLM: LLMConfig{
			Provider:    "ollama",
			Model:       "gemma3:4b",
			Temperature: 0.7,
			MaxTokens:   2048,
		},
		Brain: BrainConfig{
			DataDir:      filepath.Join(dataDir, "brain"),
			SoulFile:     "",
			MaxMemories:  1000,
			HeartbeatSec: 30,
		},
		Ollama: OllamaConfig{
			Host:         "http://localhost:11434",
			AutoInstall:  true,
			AutoStart:    true,
			DefaultModel: "gemma3:4b",
		},
		Plugins: PluginsConfig{
			Dir: filepath.Join(dataDir, "skills"),
		},
		MCP: MCPConfig{
			Servers: make(map[string]MCPServerConfig),
		},
		Log: LogConfig{
			Level: "info",
			File:  filepath.Join(dataDir, "logs", "krill.log"),
		},
		Doctor: DoctorConfig{
			Checks: []string{"ollama", "llm", "brain", "disk", "memory"},
		},
		TUI: TUIConfig{
			Theme: "ocean",
		},
	}
}

// Load reads config from YAML files and overrides with environment variables.
// Search order: $KRILL_DATA_DIR/config.yaml, ./config.yaml, ./config/default.yaml
func Load() (*Config, error) {
	cfg := DefaultConfig()

	paths := []string{
		filepath.Join(DataDir(), "config.yaml"),
		"config.yaml",
		filepath.Join("config", "default.yaml"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		break
	}

	applyEnvOverrides(cfg)
	fillDefaults(cfg)
	return cfg, nil
}

// fillDefaults re-applies defaults for any path fields that ended up empty
// after YAML parsing (YAML files may set them to "" explicitly).
func fillDefaults(cfg *Config) {
	dataDir := DataDir()
	if cfg.Brain.DataDir == "" {
		cfg.Brain.DataDir = filepath.Join(dataDir, "brain")
	}
	if cfg.Plugins.Dir == "" {
		cfg.Plugins.Dir = filepath.Join(dataDir, "skills")
	}
	if cfg.Log.File == "" {
		cfg.Log.File = filepath.Join(dataDir, "logs", "krill.log")
	}
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = make(map[string]MCPServerConfig)
	}
	if cfg.Agent.Personality == "" {
		cfg.Agent.Personality = "krill"
	}
	// Normalize PlanApproval: support legacy bool values from old configs.
	switch strings.ToLower(cfg.Agent.PlanApproval) {
	case "auto", "always", "never":
		// valid
	case "true":
		cfg.Agent.PlanApproval = "always"
	case "false":
		cfg.Agent.PlanApproval = "never"
	default:
		cfg.Agent.PlanApproval = "auto"
	}
	// AutonomyFloor migration: if absent, derive from PlanApproval (one-time)
	// and let it become the canonical knob from here on.
	if cfg.Agent.AutonomyFloor == "" {
		switch cfg.Agent.PlanApproval {
		case "always":
			cfg.Agent.AutonomyFloor = "suggest"
		case "never":
			cfg.Agent.AutonomyFloor = "act"
		case "auto":
			cfg.Agent.AutonomyFloor = "act"
		default:
			cfg.Agent.AutonomyFloor = "act"
		}
	}
	switch cfg.Agent.AutonomyFloor {
	case "observe", "suggest", "act", "evolve":
		// valid
	default:
		cfg.Agent.AutonomyFloor = "act"
	}
	if cfg.Telegram.BotMaxTurns == 0 {
		cfg.Telegram.BotMaxTurns = 3
	}
	if cfg.Brain.RecoveryTurns == 0 {
		cfg.Brain.RecoveryTurns = 10
	}
}

// Save writes the config to the data directory.
func Save(cfg *Config) error {
	dir := DataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}

// EnsureDataDir creates the data directory tree if it does not exist.
func EnsureDataDir() error {
	dirs := []string{
		DataDir(),
		filepath.Join(DataDir(), "brain"),
		filepath.Join(DataDir(), "brain", "memories"),
		filepath.Join(DataDir(), "logs"),
		filepath.Join(DataDir(), "skills"),
		filepath.Join(DataDir(), "personalities"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("KRILL_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("KRILL_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("KRILL_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("KRILL_LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("KRILL_TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.Token = v
		cfg.Telegram.Enabled = true
	}
	if v := os.Getenv("KRILL_DISCORD_TOKEN"); v != "" {
		cfg.Discord.Token = v
		cfg.Discord.Enabled = true
	}
	if v := os.Getenv("KRILL_OLLAMA_HOST"); v != "" {
		cfg.Ollama.Host = v
	}
	if v := os.Getenv("KRILL_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("KRILL_TELEGRAM_ALLOWED_IDS"); v != "" {
		ids := strings.Split(v, ",")
		cfg.Telegram.AllowedIDs = nil
		for _, s := range ids {
			if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				cfg.Telegram.AllowedIDs = append(cfg.Telegram.AllowedIDs, id)
			}
		}
	}
}
