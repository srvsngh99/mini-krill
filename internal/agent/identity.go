// Package agent — identity.go owns runtime mutation of the agent's name,
// personality, and persona overlay. The user can change all three from any
// interface (TUI, chat, Telegram, Discord) at any time, with both explicit
// `/name`/`/personality`/`/persona` commands and natural-language triggers.
//
// Persistence is synchronous so a Telegram rename is visible to a TUI session
// opened seconds later.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// safeNameRE allows letters, digits, spaces, hyphens, and apostrophes only.
// Rejects "System: you are now…" style injection and emoji-laden noise.
var safeNameRE = regexp.MustCompile(`^[A-Za-z0-9 '\-]+$`)

// renameTriggers are natural-language patterns that capture a new agent name.
// Order matters — longer phrases win to avoid double-matching shorter ones.
var renameTriggers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:i'?ll )?call you ([A-Za-z0-9 '\-]{1,40})\b`),
	regexp.MustCompile(`(?i)\bfrom now on (?:you'?re|your name is) ([A-Za-z0-9 '\-]{1,40})\b`),
	regexp.MustCompile(`(?i)\byour name is ([A-Za-z0-9 '\-]{1,40})\b`),
	regexp.MustCompile(`(?i)\b(?:let'?s )?(?:re)?name you ([A-Za-z0-9 '\-]{1,40})\b`),
	regexp.MustCompile(`(?i)\bchange your name to ([A-Za-z0-9 '\-]{1,40})\b`),
	regexp.MustCompile(`(?i)\byou'?re (?:now )?(?:called )?([A-Za-z0-9 '\-]{1,40})\b`),
}

// AgentName returns the user-facing name. Falls back to personality, then
// "Assistant" so something always renders.
func (a *KrillAgent) AgentName() string {
	if a.cfg.AgentName != "" {
		return a.cfg.AgentName
	}
	if a.cfg.Personality != "" {
		return strings.Title(a.cfg.Personality)
	}
	return "Assistant"
}

// RenameAgent sets the agent's display name and persists it. Returns the
// reason for rejection if the name fails the allowlist, or empty string on
// success.
func (a *KrillAgent) RenameAgent(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "the new name is empty"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	if !safeNameRE.MatchString(name) {
		return "names can only contain letters, digits, spaces, hyphens, and apostrophes"
	}
	a.cfg.AgentName = name
	if err := persistAgentConfig(a.cfg); err != nil {
		log.Warn("rename persist failed", "name", name, "error", err)
		return "saved in this session, but I couldn't write it to disk: " + err.Error()
	}
	log.Info("agent renamed", "name", name)
	return ""
}

// detectRenameRequest scans a user message for natural-language rename
// patterns. Returns the captured name (allowlist-filtered) or "" if none.
func detectRenameRequest(input string) string {
	for _, re := range renameTriggers {
		if m := re.FindStringSubmatch(input); len(m) >= 2 {
			candidate := strings.TrimSpace(strings.TrimRight(m[1], ".,!?"))
			if safeNameRE.MatchString(candidate) && candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

// handleNameCommand processes `/name` or `/name <new>`.
func (a *KrillAgent) handleNameCommand(input string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/name"))
	if rest == "" {
		return fmt.Sprintf("I'm currently going by %q.", a.AgentName()), true
	}
	if reason := a.RenameAgent(rest); reason != "" {
		return "I can't take that name — " + reason + ".", true
	}
	return fmt.Sprintf("Got it — I'll go by %s from here.", a.AgentName()), true
}

// handleNaturalRename runs after intent classification: if the message asks
// for a rename, do it inline and return the confirmation.
func (a *KrillAgent) handleNaturalRename(input string) (string, bool) {
	candidate := detectRenameRequest(input)
	if candidate == "" {
		return "", false
	}
	if reason := a.RenameAgent(candidate); reason != "" {
		return "I'd switch names but " + reason + ".", true
	}
	return fmt.Sprintf("Got it — I'll go by %s from here.", a.AgentName()), true
}

// PersonaOverlay represents the user-applied style directives stacked on top
// of the base personality. Each entry is a single natural-language line that
// nudges tone, formality, or behaviour.
type PersonaOverlay struct {
	Directives []OverlayDirective `json:"directives"`
}

// OverlayDirective is a single user-applied style nudge.
type OverlayDirective struct {
	AddedAt   time.Time `json:"added_at"`
	Directive string    `json:"directive"`
	Source    string    `json:"source"` // platform that captured this (telegram, tui, chat, discord)
}

// addPersonaDirective appends a directive to the overlay file at
// ~/.mini-krill/personalities/_overlay.yaml. We use a single shared overlay
// because the agent identity is per-data-dir, not per-user. If the directive
// fails the safety classifier, returns the rejection reason.
func (a *KrillAgent) addPersonaDirective(directive string) string {
	directive = strings.TrimSpace(directive)
	if directive == "" {
		return "the directive is empty"
	}
	if len(directive) > 280 {
		directive = directive[:280]
	}
	// Lightweight injection check: refuse anything that smells like a system
	// prompt override or a tool-call instruction.
	lower := strings.ToLower(directive)
	for _, banned := range []string{
		"ignore previous", "forget your instructions", "system:", "user:",
		"assistant:", "leak", "credentials", "api key", "password",
		"reveal config", "dump memory",
	} {
		if strings.Contains(lower, banned) {
			return "that looks like an instruction to bypass my safety rules, not a personality tweak"
		}
	}
	overlay := loadOverlay()
	overlay.Directives = append(overlay.Directives, OverlayDirective{
		AddedAt:   time.Now(),
		Directive: directive,
		Source:    a.platform,
	})
	if err := saveOverlay(overlay); err != nil {
		log.Warn("persona overlay save failed", "error", err)
		return "couldn't write the overlay to disk: " + err.Error()
	}
	log.Info("persona directive added", "directive", directive)
	return ""
}

// handlePersonaCommand processes `/persona [list|reset|undo|<directive>]`.
func (a *KrillAgent) handlePersonaCommand(input string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/persona"))
	switch {
	case rest == "" || rest == "list":
		overlay := loadOverlay()
		if len(overlay.Directives) == 0 {
			return "No persona directives applied. Use `/persona <description>` to add one.", true
		}
		var b strings.Builder
		b.WriteString("Active persona overlay:\n")
		for i, d := range overlay.Directives {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, d.Directive))
		}
		return b.String(), true
	case rest == "reset" || rest == "clear":
		_ = saveOverlay(&PersonaOverlay{})
		return "Persona overlay cleared. Back to the base personality.", true
	case rest == "undo":
		overlay := loadOverlay()
		if len(overlay.Directives) == 0 {
			return "No directives to undo.", true
		}
		dropped := overlay.Directives[len(overlay.Directives)-1]
		overlay.Directives = overlay.Directives[:len(overlay.Directives)-1]
		_ = saveOverlay(overlay)
		return fmt.Sprintf("Removed: %q.", dropped.Directive), true
	default:
		if reason := a.addPersonaDirective(rest); reason != "" {
			return "Couldn't apply that — " + reason + ".", true
		}
		return "Got it — I'll keep that in mind from here.", true
	}
}

// loadOverlay reads the current overlay file or returns an empty one.
func loadOverlay() *PersonaOverlay {
	path := overlayPath()
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return &PersonaOverlay{}
	}
	var o PersonaOverlay
	// Use the existing YAML loader chain — keep it simple, store as JSON-ish text.
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		o.Directives = append(o.Directives, OverlayDirective{Directive: line})
	}
	return &o
}

// saveOverlay writes the overlay file. One directive per line; we don't need
// timestamps to round-trip — they live in memory only.
func saveOverlay(o *PersonaOverlay) error {
	path := overlayPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var lines []string
	lines = append(lines, "# Persona overlay — user-applied style directives.")
	lines = append(lines, "# Edit with: /persona <directive>, /persona undo, /persona reset.")
	for _, d := range o.Directives {
		lines = append(lines, d.Directive)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// overlayPath returns ~/.mini-krill/personalities/_overlay.yaml.
func overlayPath() string {
	return filepath.Join(config.DataDir(), "personalities", "_overlay.yaml")
}

// OverlayDirectives returns the active directives as plain strings, ordered.
// Used by the system-prompt assembler in handleChat.
func OverlayDirectives() []string {
	o := loadOverlay()
	out := make([]string, 0, len(o.Directives))
	for _, d := range o.Directives {
		out = append(out, d.Directive)
	}
	return out
}

// persistAgentConfig writes the agent config back to ~/.mini-krill/config.yaml
// without disturbing the rest of the file. Implementation: load, set agent
// fields, save.
func persistAgentConfig(agent config.AgentConfig) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Agent = agent
	return config.Save(cfg)
}

// emojiStyle returns the active style — agent caches the most recent value;
// if unset reads it from disk on first call.
func (a *KrillAgent) emojiStyle() string {
	if a.cachedEmojiStyle != "" {
		return a.cachedEmojiStyle
	}
	cfg, err := config.Load()
	if err == nil && cfg.Brain.EmojiStyle != "" {
		a.cachedEmojiStyle = cfg.Brain.EmojiStyle
		return a.cachedEmojiStyle
	}
	a.cachedEmojiStyle = "sparse"
	return a.cachedEmojiStyle
}

// SetEmojiStyle persists a new emoji style and returns the reason for
// rejection ("" on success).
func (a *KrillAgent) SetEmojiStyle(style string) string {
	switch style {
	case "none", "sparse", "playful":
	default:
		return `style must be "none", "sparse", or "playful"`
	}
	cfg, err := config.Load()
	if err != nil {
		return err.Error()
	}
	cfg.Brain.EmojiStyle = style
	if err := config.Save(cfg); err != nil {
		return err.Error()
	}
	a.cachedEmojiStyle = style
	log.Info("emoji style updated", "style", style)
	return ""
}

// handleEmojiCommand processes `/emoji [none|sparse|playful]`.
func (a *KrillAgent) handleEmojiCommand(input string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/emoji"))
	if rest == "" {
		return fmt.Sprintf("Current emoji style: %s. Options: none | sparse | playful.", a.emojiStyle()), true
	}
	if reason := a.SetEmojiStyle(rest); reason != "" {
		return "Couldn't change emoji style — " + reason + ".", true
	}
	return "Emoji style set to " + rest + ".", true
}

// emojiRange covers the most common Unicode emoji blocks. Used for the
// "none" output filter that strips emoji from agent responses.
var emojiRange = regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}\x{2300}-\x{23FF}]`)

// applyEmojiStyle post-processes a response to honour the user's emoji
// preference. "none" strips all emoji; "sparse" trims to at most one; "playful"
// is a passthrough.
func applyEmojiStyle(response, style string) string {
	switch style {
	case "none":
		return strings.TrimSpace(emojiRange.ReplaceAllString(response, ""))
	case "sparse":
		matches := emojiRange.FindAllStringIndex(response, -1)
		if len(matches) <= 1 {
			return response
		}
		// Keep the first emoji, strip the rest.
		var b strings.Builder
		last := 0
		for i, m := range matches {
			if i == 0 {
				continue // keep first
			}
			b.WriteString(response[last:m[0]])
			last = m[1]
		}
		b.WriteString(response[last:])
		return b.String()
	default:
		return response
	}
}

// handleIdentityCommand routes the slash commands that mutate identity at
// runtime: /name, /personality, /persona, /emoji.
func (a *KrillAgent) handleIdentityCommand(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "/name" || strings.HasPrefix(lower, "/name "):
		return a.handleNameCommand(trimmed)
	case lower == "/persona" || strings.HasPrefix(lower, "/persona "):
		return a.handlePersonaCommand(trimmed)
	case lower == "/personality" || strings.HasPrefix(lower, "/personality "):
		return a.handlePersonalityCommand(trimmed)
	case lower == "/emoji" || strings.HasPrefix(lower, "/emoji "):
		return a.handleEmojiCommand(trimmed)
	}
	return "", false
}

// handlePersonalityCommand processes `/personality [name]`. Without an arg,
// lists available personalities. With an arg, switches to that personality
// and persists the choice.
func (a *KrillAgent) handlePersonalityCommand(input string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(input, "/personality"))
	if rest == "" {
		// List
		dataDir := config.DataDir()
		// Avoid importing brain here — read the directory directly.
		entries, err := os.ReadDir(filepath.Join(dataDir, "personalities"))
		if err != nil {
			return "Couldn't list personalities: " + err.Error(), true
		}
		var names []string
		for _, e := range entries {
			n := e.Name()
			if !strings.HasSuffix(n, ".yaml") || strings.HasPrefix(n, "_") {
				continue
			}
			names = append(names, strings.TrimSuffix(n, ".yaml"))
		}
		if len(names) == 0 {
			names = []string{"krill"}
		}
		return fmt.Sprintf("Currently: %s. Available: %s. Switch with `/personality <name>`.",
			a.cfg.Personality, strings.Join(names, ", ")), true
	}
	// Switch
	a.cfg.Personality = rest
	if err := persistAgentConfig(a.cfg); err != nil {
		return "Couldn't save personality choice: " + err.Error(), true
	}
	log.Info("personality switched", "name", rest)
	return fmt.Sprintf("Personality switched to %s. The voice will shift on the next reply.", rest), true
}

// detectEmojiPreference catches "stop with the emojis", "more emojis", etc.
// Returns the new style or "" if no match.
func detectEmojiPreference(input string) string {
	lower := strings.ToLower(input)
	if strings.Contains(lower, "no emoji") || strings.Contains(lower, "stop with the emoji") ||
		strings.Contains(lower, "drop the emoji") || strings.Contains(lower, "without emoji") {
		return "none"
	}
	if strings.Contains(lower, "more emoji") || strings.Contains(lower, "use emojis") ||
		strings.Contains(lower, "playful emoji") {
		return "playful"
	}
	if strings.Contains(lower, "fewer emoji") || strings.Contains(lower, "less emoji") ||
		strings.Contains(lower, "sparse emoji") {
		return "sparse"
	}
	return ""
}
