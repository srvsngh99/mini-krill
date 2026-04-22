package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.LLM.Provider != "ollama" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "ollama")
	}
	if cfg.Agent.PlanApproval != true {
		t.Error("Agent.PlanApproval = false, want true")
	}
	if cfg.Agent.Name != "krill" {
		t.Errorf("Agent.Name = %q, want %q", cfg.Agent.Name, "krill")
	}
	if cfg.Agent.MaxSubKrills != 3 {
		t.Errorf("Agent.MaxSubKrills = %d, want 3", cfg.Agent.MaxSubKrills)
	}
	if cfg.LLM.Model == "" {
		t.Error("LLM.Model is empty, want a default model to be set")
	}
	if cfg.LLM.Model != cfg.Ollama.DefaultModel {
		t.Errorf("LLM.Model = %q, want it to match Ollama.DefaultModel %q", cfg.LLM.Model, cfg.Ollama.DefaultModel)
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("LLM.Temperature = %f, want 0.7", cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("LLM.MaxTokens = %d, want 2048", cfg.LLM.MaxTokens)
	}
	if cfg.Ollama.Host != "http://localhost:11434" {
		t.Errorf("Ollama.Host = %q, want %q", cfg.Ollama.Host, "http://localhost:11434")
	}
	if cfg.Brain.MaxMemories != 1000 {
		t.Errorf("Brain.MaxMemories = %d, want 1000", cfg.Brain.MaxMemories)
	}
	if cfg.Brain.HeartbeatSec != 30 {
		t.Errorf("Brain.HeartbeatSec = %d, want 30", cfg.Brain.HeartbeatSec)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.TUI.Theme != "ocean" {
		t.Errorf("TUI.Theme = %q, want %q", cfg.TUI.Theme, "ocean")
	}
	if cfg.MCP.Servers == nil {
		t.Error("MCP.Servers is nil, want initialized map")
	}
}

func TestDataDir(t *testing.T) {
	dir := DataDir()
	if dir == "" {
		t.Error("DataDir() returned empty string")
	}
}

func TestDataDirEnvOverride(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", custom)

	dir := DataDir()
	if dir != custom {
		t.Errorf("DataDir() = %q, want %q", dir, custom)
	}
}

func TestEnsureDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", tmpDir)

	if err := EnsureDataDir(); err != nil {
		t.Fatalf("EnsureDataDir() error: %v", err)
	}

	// Verify subdirectories were created
	expectedDirs := []string{
		tmpDir,
		filepath.Join(tmpDir, "brain"),
		filepath.Join(tmpDir, "brain", "memories"),
		filepath.Join(tmpDir, "logs"),
		filepath.Join(tmpDir, "skills"),
	}

	for _, d := range expectedDirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", d)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", tmpDir)

	original := DefaultConfig()
	original.Agent.Name = "test-krill"
	original.LLM.Provider = "openai"
	original.LLM.APIKey = "test-key-123"
	original.LLM.Model = "gpt-4"

	if err := Save(original); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify the file was created
	configPath := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file not created at %q: %v", configPath, err)
	}

	// Load it back
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.Agent.Name != original.Agent.Name {
		t.Errorf("Agent.Name = %q, want %q", loaded.Agent.Name, original.Agent.Name)
	}
	if loaded.LLM.Provider != original.LLM.Provider {
		t.Errorf("LLM.Provider = %q, want %q", loaded.LLM.Provider, original.LLM.Provider)
	}
	if loaded.LLM.APIKey != original.LLM.APIKey {
		t.Errorf("LLM.APIKey = %q, want %q", loaded.LLM.APIKey, original.LLM.APIKey)
	}
	if loaded.LLM.Model != original.LLM.Model {
		t.Errorf("LLM.Model = %q, want %q", loaded.LLM.Model, original.LLM.Model)
	}
}

func TestEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", tmpDir)
	t.Setenv("KRILL_LLM_API_KEY", "env-secret-key")
	t.Setenv("KRILL_LLM_PROVIDER", "anthropic")
	t.Setenv("KRILL_LLM_MODEL", "claude-3")
	t.Setenv("KRILL_LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LLM.APIKey != "env-secret-key" {
		t.Errorf("LLM.APIKey = %q, want %q", cfg.LLM.APIKey, "env-secret-key")
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-3" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-3")
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestFillDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", tmpDir)

	cfg := &Config{}
	fillDefaults(cfg)

	if cfg.Brain.DataDir == "" {
		t.Error("fillDefaults did not set Brain.DataDir")
	}
	if cfg.Plugins.Dir == "" {
		t.Error("fillDefaults did not set Plugins.Dir")
	}
	if cfg.Log.File == "" {
		t.Error("fillDefaults did not set Log.File")
	}
	if cfg.MCP.Servers == nil {
		t.Error("fillDefaults did not initialize MCP.Servers map")
	}

	// Verify paths include the data dir
	expectedBrainDir := filepath.Join(tmpDir, "brain")
	if cfg.Brain.DataDir != expectedBrainDir {
		t.Errorf("Brain.DataDir = %q, want %q", cfg.Brain.DataDir, expectedBrainDir)
	}
}

func TestEnvOverrideTelegramToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", tmpDir)
	t.Setenv("KRILL_TELEGRAM_TOKEN", "tg-token-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Telegram.Token != "tg-token-123" {
		t.Errorf("Telegram.Token = %q, want %q", cfg.Telegram.Token, "tg-token-123")
	}
	if !cfg.Telegram.Enabled {
		t.Error("Telegram.Enabled = false, want true when token is set via env")
	}
}
