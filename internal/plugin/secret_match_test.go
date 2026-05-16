package plugin

import (
	"path/filepath"
	"testing"
)

// PR #32 nit: the secret check was a raw substring over the whole path, so a
// benign file containing a pattern as a fragment was wrongly refused.
// secretMatch must anchor to real filename boundaries.
func TestSecretMatch(t *testing.T) {
	j := filepath.Join
	refuse := []string{
		j("/repo", "config.yaml"),
		j("/repo", "config.yaml.bak"),
		j("/repo", ".env"),
		j("/repo", "prod.env"),
		j("/repo", "server.pem"),
		j("/repo", "id_rsa.key"),
		j("/repo", "secrets.json"),
		j("/repo", "private_key"),
		j("/repo", "service_account"),
		j("/repo", "credentials", "db.txt"), // path component
		j("/repo", "deploy", "credentials"), // trailing component
	}
	for _, p := range refuse {
		if _, hit := secretMatch(p); !hit {
			t.Errorf("secretMatch(%q) = false, want refused", p)
		}
	}

	allow := []string{
		j("/repo", "notconfig.yaml.txt"), // the regression this fixes
		j("/repo", "internal", "agent", "agent.go"),
		j("/repo", "envparser.go"),       // contains "env" but not a .env file
		j("/repo", "keyboard.go"),        // contains "key" but not a .key file
		j("/repo", "configurator.md"),    // starts like "config" but not config.yaml
		j("/repo", "my_credentials.md"),  // "credentials" mid-name, not a component
	}
	for _, p := range allow {
		if pat, hit := secretMatch(p); hit {
			t.Errorf("secretMatch(%q) = refused on %q, want allowed", p, pat)
		}
	}
}
