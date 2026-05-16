package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
)

// Regression: a /name rename carries an in-memory AgentConfig whose
// Personality is empty. persistAgentConfig must NOT let that erase the
// init-chosen personality on disk (it previously did, after which Load
// silently defaulted personality to "krill" (a silent identity downgrade).
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

// Regression: the agent_name/personality keys must be emitted even when the
// values are empty. This is the assertion that actually pins the omitempty
// removal: with `,omitempty` restored these keys vanish from the file for an
// empty struct, so this test fails. (A non-empty round-trip would pass either
// way and would not guard the regression.)
func TestMarshalYAML_IdentityFieldsNotOmitted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KRILL_DATA_DIR", dir)

	c := config.DefaultConfig()
	c.Agent.AgentName = "" // the exact condition omitempty would have dropped
	c.Agent.Personality = ""
	if err := config.Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	text := string(raw)
	// agent_name is unique to the agent block, so presence is sufficient.
	if !strings.Contains(text, "agent_name:") {
		t.Error("agent_name: key missing from config.yaml on empty value (omitempty regression)")
	}
	// personality: appears in BOTH the agent and brain blocks; the brain
	// one is never omitempty, so it is always present. With the agent
	// field's omitempty removed there must be two occurrences; with it
	// restored and the value empty there is only the brain one.
	if n := strings.Count(text, "personality:"); n < 2 {
		t.Errorf("agent personality: key missing (found %d personality: lines, want >=2; omitempty regression)", n)
	}
}
