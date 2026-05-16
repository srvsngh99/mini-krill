package agent

import (
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
)

// Regression: a /name rename carries an in-memory AgentConfig whose
// Personality is empty. persistAgentConfig must NOT let that erase the
// init-chosen personality on disk (it previously did, after which Load
// silently defaulted personality to "krill" — a silent identity downgrade).
func TestPersistAgentConfig_PreservesPersonalityOnRename(t *testing.T) {
	t.Setenv("KRILL_DATA_DIR", t.TempDir())

	seed := config.DefaultConfig()
	seed.Agent.Name = "test-krill"
	seed.Agent.AgentName = "Jarvis"
	seed.Agent.Personality = "jarvis"
	if err := config.Save(seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// Simulate RenameAgent's call: only AgentName set, Personality blank.
	if err := persistAgentConfig(config.AgentConfig{AgentName: "Friday"}); err != nil {
		t.Fatalf("persistAgentConfig: %v", err)
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Agent.Personality != "jarvis" {
		t.Errorf("personality = %q, want %q (must survive a rename)", got.Agent.Personality, "jarvis")
	}
	if got.Agent.AgentName != "Friday" {
		t.Errorf("agent_name = %q, want %q (rename must still apply)", got.Agent.AgentName, "Friday")
	}
}

// Regression: agent_name/personality must be written even when set, never
// dropped by omitempty — a round-trip through Save/Load preserves them.
func TestMarshalYAML_IdentityFieldsNotOmitted(t *testing.T) {
	t.Setenv("KRILL_DATA_DIR", t.TempDir())

	c := config.DefaultConfig()
	c.Agent.AgentName = "Jarvis"
	c.Agent.Personality = "jarvis"
	if err := config.Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Agent.AgentName != "Jarvis" || got.Agent.Personality != "jarvis" {
		t.Errorf("identity not round-tripped: agent_name=%q personality=%q",
			got.Agent.AgentName, got.Agent.Personality)
	}
}
