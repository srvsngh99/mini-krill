# Changelog

All notable changes to Mini Krill will be documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [0.1.3] - 2026-05-16

Closed-loop learning, cross-session memory, single-owner safety, and an honest provider catalog. The v0.1.3 cycle (PRs A, A2, B, C).

### Closed-loop learning (PR A)
- Post-turn THANKED/FIXED callback. The affinity learner was one-directional — an auto-run produced no verdict, so a calibrated cluster's score could only ever rise and a cluster that started producing bad output stayed auto-run forever. The user's next message after an un-gated turn is now the verdict: a correction credits FIXED (drift back toward planning), anything else credits THANKED. Restores symmetric drift; closes the freeze flagged in the PR #33 review.
- #16: `looksLikeCorrection` caught `"don't"` but not `doesn't`/`didn't`/`isn't`/`wasn't`/`won't`/`can't`/… so real corrections went uncredited. Extended the contraction list (word-bounded) plus curly-apostrophe normalisation.
- `affinity:cluster:` is now a reserved memory namespace: `FileMemory.Store` refuses writes there unless they come from the affinity store, so `/remember` can't corrupt a learned record.

### Cross-session memory & user-state (PR A2)
- Episodic memory (#29 / D6): on a >30 min activity gap the prior session is summarised into one episode; the most recent episode <7 days old is injected into chat context as an explicitly point-in-time, read-only line so the agent resumes instead of starting cold.
- User-state (#37): focus area, last task cluster, light mood, last-seen — persisted and injected read-only as point-in-time context.

### Single-owner safety (PR B)
- New `telegram.owner_id` / `discord.owner_id`. When set, only the owner drives the bot; a bystander in a shared group/server is replied to with a fixed decline only if they directly address the bot, and their message never reaches the agent — no tasks, memory writes, or destructive actions from a stranger. The safety counterpart to "no approval prompts by default". Unset = legacy behaviour with a one-time hint.
- #22: residual `[CROSSPOST:..]` literals from a malformed directive are scrubbed so the raw token never leaks into chat.

### Provider honesty & hardening (PR C)
- #23: removed the fabricated Codex model catalog (`gpt-5.5`, `gpt-5.4`, …). Those IDs don't exist and picking one failed ~90s later. Codex now advertises only `auto`, delegating model choice to the Codex CLI.
- `secretPathPatterns` (self:read-code / read-logs) anchored to real filename boundaries instead of a raw substring, so a benign `notconfig.yaml.txt` is no longer refused while real secrets still are.

### Notes
- #7 (Codex stderr/timeout surfacing) was largely resolved by the PR #27 `runCLI` rewrite (separate stderr, distinct ctx-cancel surfacing, idle monitor).
- Deferred to a future introspection cycle: `self:explain-decision`, `self:dashboard`, replay-from-log, active-listening primitive, per-space context scoping, Telegram reply-to capture, and a real Telegram backoff (the live polling loop uses the library's `GetUpdatesChan`).

## [0.1.2] - 2026-05-16

No more nagging. The agent stops asking for manual permission on routine work.

### Autonomy
- Manual approval prompts are **off by default**. Under the default `act`/`evolve` autonomy floor, a non-destructive plan never gates — step count, task vagueness, and an un-calibrated affinity cluster are no longer reasons to ask "Approve this plan? (yes/no)". This was the top "why does it keep asking permission" complaint and contradicted the documented v0.1.1 `act` contract. Users who want a gate set `autonomy_floor: suggest`.
- Unchanged safety: destructive plans (delete, `git push`, deploy, `rm`, drop, …) still always gate, regardless of floor. `autonomy_floor: suggest`/`observe` still gate on demand.

### Fixed
- Version constant and README badge corrected — the v0.1.1 release shipped its CHANGELOG and merged code but never bumped `core.Version` (still reported `0.1.0`) or the README badge. Both now report `0.1.2`.

## [0.1.1] - 2026-05-15

Trust, autonomy, and identity. The agent now learns which task types you want planned, acts decisively on the rest, and is no longer hard-wired as a krill.

### Trust
- Plan executor threads the original user task into every step's context (was: only previous step outputs). Step 1 used to hallucinate "no link was provided" when the link was right there in the message; that path is closed.
- New `NEED_INPUT:` / `BLOCKED:` step statuses. The executor stops reporting "5/5 steps succeeded" for steps that the LLM admitted it could not run.
- Provenance tags: every reply that cites `[web:url]` is checked against URLs actually fetched during the same turn. Unmatched cites are stripped and a warning is prepended. Closes the fabricated-source class of failures.
- Search-tool errors now surface verbatim instead of falling through to a hallucinated "permission denied" excuse.

### Autonomy
- New `autonomy_floor` config (`observe | suggest | act | evolve`) replaces `plan_approval`. Default flips to `act` — non-destructive plans run without an approval gate. Destructive plans always gate, regardless of floor.
- Per-task-type plan-affinity learning. The agent clusters requests by `verb/object` and learns from APPROVED / MODIFIED / REJECTED / IGNORED outcomes whether to plan-then-act, plan-in-parallel, or skip the plan entirely.
- "No need for taking my approval" mid-pending-plan now flips the pref AND approves the current plan AND credits the cluster — all in one shot. No more "Yea" → "Yes" double-tap.
- When the planner's first step is "Ask the user for X", the agent now skips the plan and asks the question directly.
- Approval and rejection regex extended with emoji acks (👍 ✅ 👎 ❌ etc.) so Telegram thumbs-up approves a pending plan without an LLM round-trip.

### Identity & personality
- Setup wizard now asks for personality (Buddy / Jarvis / Friday / Samantha / Tars / Krill) and an agent name. Default for new users is Buddy.
- Default soul decoupled from krill: `genericSystemPrompt` is the new neutral fallback. Krill voice becomes one personality among many, not THE default.
- Runtime rename on any interface: `/name Edwin` slash command + natural-language triggers ("call you Edwin", "your name is Sage", "from now on you're …"). Allowlist sanitiser blocks injection-shaped names.
- Persona overlay: `/persona <description>` (or `/persona list | undo | reset`) layers natural-language style directives on top of the base personality. Lightweight injection check refuses overlay attempts that look like prompt overrides.
- `/personality <name>` switches voice mid-session.

### Emoji
- New `emoji_style` config (`none | sparse | playful`). Output filter strips or trims emoji to honour the choice.
- `/emoji none|sparse|playful` slash command + natural-language triggers ("stop with the emojis", "more emojis").
- Emoji signals (👍 ❤️ 🔥 / 👎 🙄 ❌) feed the adaptive personality loop.

### Memory & scrollback
- `ConversationStore.Search` scans the entire JSONL, not just the sliding window. Powers "remember when we talked about X" recall (Issue #19).
- `/recall last N` / `/recall yesterday` / `/recall today` / `/recall about <topic>` slash commands + natural-language triggers ("what did I say yesterday", "go back to our last 5 messages", "remember when we discussed …").

### Eyes on self (read-only introspection)
- New `self:read-code` skill: read / grep / tree the agent's own source. Sandboxed to the repo root (resolved via `MINIKRILL_REPO` env var or `go.mod` walk). Refuses paths matching the secret allowlist (`config.yaml`, `.env`, `*.key`, etc).
- New `self:read-logs` skill: tail / grep / "errors" over `~/.mini-krill/logs/`.
- Both registered automatically; both strictly read-only. Self-modification still requires explicit human review.

### Quick wins (PR #1)
- macOS `MallocStackLogging` warnings filtered at the source (env) AND post-source (log writer). `dive.log` no longer fills with thousands of dyld noise lines.
- Codex CLI deprecated `--full-auto` replaced with `--sandbox workspace-write`.
- Dead `SetChannel` no-op deleted.
- `recordFeedback` correction trigger tightened: "Nope just find me X" is redirection, not dissatisfaction.
- Freshness-keyword router match now requires a co-occurring external-info noun, so "what's the current state of my plan" no longer trips a web search.

### Breaking changes
- `plan_approval` config field is migrated to `autonomy_floor` on first load. Old YAML configs continue to work; migration is logged once.
- `PlanApproval=never` no longer bypasses destructive checks. No autonomy floor authorises `rm -rf` silently. Operators who want gating can set `autonomy_floor: suggest`.

## [0.1.0] - 2026-05-02

### Added
- Initial release of Mini Krill
- `minikrill run "<prompt>"` - one-shot chat command: sends a prompt through the agent and prints the plain text response to stdout, then exits (60s timeout)
- `minikrill notify <message>` - sends a Telegram message using `KRILL_TELEGRAM_TOKEN` and `KRILL_TELEGRAM_CHAT_ID` env vars
- Core agent with plan-before-execute workflow
- Ollama integration for local LLM inference
- Cloud provider support (OpenAI, Anthropic, Google)
- Brain system with JSON-file memory, personality, soul, and heartbeat
- Plugin system with YAML-based skill registry
- MCP (Model Context Protocol) server registry
- Telegram bot integration
- Discord bot integration (optional)
- Krill-themed terminal UI (TUI) with Bubble Tea
- Interactive CLI chat mode
- Setup wizard (minikrill init)
- Health diagnostics (minikrill doctor)
- Quick health ping (minikrill sonar)
- Ollama management commands (install, start, stop, pull, list, status)
- Brain inspection commands (status, recall, forget, search)
- Sub-krill spawning for focused subtasks
- Cross-platform support (Linux, macOS, Windows)
- One-line installation script
- Docker support with docker-compose
- 20 hidden krill facts throughout the codebase
