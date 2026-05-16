package chat

import (
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
)

// TestDecideOwnerGate is the boundary regression owed from PR #37 review
// (follow-up 1): with owner-gating on, anything that is not a confirmed owner
// — crucially the msg.From == nil case, which presents as isOwner=false —
// must never resolve to gateProceed, so the agent is never reached.
func TestDecideOwnerGate(t *testing.T) {
	cases := []struct {
		name                       string
		ownerSet, isOwner, address bool
		want                       ownerGate
	}{
		{"gating off → always proceed", false, false, true, gateProceed},
		{"gating off, unaddressed → proceed", false, false, false, gateProceed},
		{"owner proceeds", true, true, false, gateProceed},
		{"owner proceeds even if addressed", true, true, true, gateProceed},
		{"bystander addressing → decline", true, false, true, gateDecline},
		{"bystander silent → ignore", true, false, false, gateIgnore},
	}
	for _, c := range cases {
		if got := decideOwnerGate(c.ownerSet, c.isOwner, c.address); got != c.want {
			t.Errorf("%s: decideOwnerGate(%v,%v,%v)=%d want %d",
				c.name, c.ownerSet, c.isOwner, c.address, got, c.want)
		}
	}

	// The owed invariant, stated directly: gating on + not the owner (the
	// From==nil regime) never proceeds to the agent, addressed or not.
	for _, addressed := range []bool{true, false} {
		if got := decideOwnerGate(true, false, addressed); got == gateProceed {
			t.Fatalf("owner-gating on + non-owner (From==nil) must never proceed (addressed=%v)", addressed)
		}
	}
}

func TestTelegramIsOwner(t *testing.T) {
	// Unset → owner-gating off: nobody is "the owner", so the caller falls
	// through to legacy behaviour.
	off := &TelegramBot{cfg: config.TelegramConfig{}}
	if off.isOwner(123) {
		t.Error("unset OwnerID must never report an owner")
	}

	on := &TelegramBot{cfg: config.TelegramConfig{OwnerID: 42}}
	if !on.isOwner(42) {
		t.Error("configured owner must be recognised")
	}
	if on.isOwner(43) {
		t.Error("a non-owner must not be recognised as owner")
	}
}

func TestDiscordIsOwner(t *testing.T) {
	off := &DiscordBot{cfg: config.DiscordConfig{}}
	if off.isOwner("anyone") {
		t.Error("unset OwnerID must never report an owner")
	}
	on := &DiscordBot{cfg: config.DiscordConfig{OwnerID: "owner#1"}}
	if !on.isOwner("owner#1") {
		t.Error("configured owner must be recognised")
	}
	if on.isOwner("someone-else") {
		t.Error("a non-owner must not be recognised as owner")
	}
}

func TestScrubCrosspostLiterals(t *testing.T) {
	// Clean text → untouched, no scrub.
	if got, did := scrubCrosspostLiterals("just a normal reply"); did || got != "just a normal reply" {
		t.Errorf("clean text changed: got=%q did=%v", got, did)
	}

	// Well-formed directives are removed by extractCrossPostDirectives first;
	// scrub only mops residue. Unclosed header → must not leak the token.
	got, did := scrubCrosspostLiterals("here you go [CROSSPOST:12345] and the rest")
	if !did || strings.Contains(got, "CROSSPOST") {
		t.Errorf("unclosed header leaked: got=%q did=%v", got, did)
	}

	// Stray closing tag → stripped.
	got, did = scrubCrosspostLiterals("done[/CROSSPOST]")
	if !did || strings.Contains(got, "CROSSPOST") {
		t.Errorf("stray close tag leaked: got=%q did=%v", got, did)
	}

	// Unterminated header with no closing bracket → cut to end of line, no leak.
	got, did = scrubCrosspostLiterals("line one\n[CROSSPOST broken here\nline three")
	if !did || strings.Contains(got, "CROSSPOST") {
		t.Errorf("unterminated header leaked: got=%q did=%v", got, did)
	}
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line three") {
		t.Errorf("scrub removed too much: got=%q", got)
	}
	// Follow-up 2: the broken line's own newline is consumed, so no blank
	// line is left where the directive was.
	if strings.Contains(got, "\n\n") {
		t.Errorf("scrub left a blank line: got=%q", got)
	}

	// Bracket-less close fragment (mirror of the unterminated-header case) →
	// must not leak the token either.
	got, did = scrubCrosspostLiterals("keep[/CROSSPOST")
	if !did || strings.Contains(got, "CROSSPOST") {
		t.Errorf("bracket-less close leaked: got=%q did=%v", got, did)
	}
	if !strings.Contains(got, "keep") {
		t.Errorf("scrub removed too much: got=%q", got)
	}
}
