# Mini Krill Autonomous Agent Handoff

This document captures the current product gap and a practical execution brief
for turning Mini Krill from a chat bot with planning into a smarter autonomous
agent that learns, remembers, and improves with use.

## Goal

Mini Krill should feel like an agent that can:

- understand when a user wants chat, advice, diagnosis, or action
- inspect local repos and files when asked
- run real tools and commands with progress updates
- continue long-running work without Telegram or CLI timeouts killing the task
- remember durable facts, preferences, projects, and outcomes
- reflect on repeated feedback and adapt behavior automatically

Today it has pieces of this system, but they are not connected into a real
agent loop.

## Observed Transcript Symptoms

These behaviors came from real usage:

- Ordinary questions like "why did I get this error?" triggered a full
  `DIVE PLAN` instead of a direct clarifying answer.
- Requests like "check the Medha repo in playground" became plans and then
  failed with `context deadline exceeded`.
- `/use codex` failed in Telegram even though the README documents `/use`.
- Approved plans timed out after the user said "yes".
- The bot sometimes answered with generic personality text instead of useful
  stateful help.
- It claimed it could learn and remember, but did not feel like it was evolving
  from usage.

## Root Causes

### 1. Planner is overused

Relevant file:

- `internal/agent/agent.go`

`classifyIntent` only chooses between `TASK` and `CHAT`. Many messages that
should be handled as direct answers or lightweight diagnostics become `TASK`,
which triggers a plan approval loop.

Current issue:

- "Can you check X?" becomes a plan instead of immediate repo inspection.
- "Why did I get this error?" becomes a plan even when the bot already has
  enough context to explain likely causes.
- The user has to approve too many obvious actions.

### 2. Plan execution is only LLM step narration

Relevant file:

- `internal/agent/planner.go`

`ExecutePlanSteps` calls the LLM once per plan step with "Execute this step".
It does not actually dispatch tools, inspect files, run tests, or persist
intermediate state.

Current issue:

- A plan can describe useful work without doing useful work.
- Each step consumes a model call.
- Multi-step plans regularly exceed Telegram and CLI timeouts.

### 3. Long-running tasks are tied to message timeouts

Relevant files:

- `internal/chat/telegram.go`
- `cmd/minikrill/cmd_run.go`
- `internal/llm/cli_provider.go`

Telegram wraps every message in a 90 second context. CLI `run` defaults to 60
seconds. The CLI provider itself defaults to 10 minutes, but the shorter
upstream timeouts cancel work before the provider finishes. A multi-step plan
through Codex or Claude can take longer than both the Telegram and CLI `run`
limits.

Current issue:

- The user sees `context deadline exceeded`.
- Work is lost because there is no durable task session.
- The bot has no way to say "I started this, I will report back."

### 4. Command naming is inconsistent across interfaces

Relevant files:

- `README.md`
- `docs/INTERFACES.md`
- `internal/chat/telegram.go`
- `internal/agent/agent.go`

Both `/use` and `/switch` work, but they are split across interfaces. The core
agent handles `/use` while Telegram documents and handles `/switch`. Both
interfaces also support natural language switching ("switch to X", "use X").
The commands are not broken, but the inconsistent naming confuses users who
read the README and then try `/use` in Telegram without realizing `/switch` is
the equivalent there.

Current issue:

- Users may not discover the correct command for their interface on the first try.
- Documentation should standardize on one name or explicitly list both.

### 5. Memory exists but is not agentic

Relevant files:

- `internal/brain/memory.go`
- `internal/brain/conversations.go`
- `internal/agent/agent.go`
- `internal/plugin/self.go`

Mini Krill stores JSON memories and conversation turns, but memory retrieval is
mostly substring search and recent-turn replay. Feedback is stored, but
reflection is manual.

Current issue:

- It remembers raw snippets, not useful knowledge.
- It does not consolidate repeated facts.
- It does not automatically learn project context.
- It does not automatically adapt from feedback.

### 6. Unified conversation can blur context

Relevant file:

- `internal/agent/agent.go`

`SetChannel` stores all turns under one unified channel. This helps continuity,
but it can blur Telegram group chats, DMs, CLI, and TUI sessions.

Current issue:

- The agent can lose track of who said what.
- Group memories and private memories need separate scopes.

## Desired Architecture

Build a small autonomous task layer between chat input and LLM/tool execution.

Suggested packages:

- `internal/agent/router.go`
- `internal/agent/task.go`
- `internal/agent/toolbox.go`
- `internal/brain/learning.go`
- `internal/brain/semantic_memory.go` if embeddings are added later

Core loop:

1. Route intent:
   - `CHAT`
   - `ANSWER`
   - `DIAGNOSE`
   - `REMEMBER`
   - `TOOL_TASK`
   - `LONG_TASK`
2. For simple chat or answer, respond directly.
3. For safe obvious tool tasks, execute directly with progress.
4. For risky or destructive tasks, ask for approval.
5. For long tasks, create a durable task record and return a task ID.
6. Execute tools in a worker loop.
7. Store findings and outcomes in memory.
8. Periodically reflect and consolidate memories.

## Implementation Plan

### Phase 1: Fix routing and command mismatch

Scope:

- Add a deterministic pre-router before LLM classification.
- Stop planning for simple questions, greetings, status checks, model switches,
  and diagnostic questions.
- Support `/use` and `/switch` consistently in Telegram.
- Update help text and docs to show both commands or standardize on one.

Acceptance criteria:

- `/use codex` works in Telegram.
- "why did I get this error?" does not produce a `DIVE PLAN`.
- "are you okay?" maps to health or chat, not a generic plan.
- "check repo X" is routed as an actionable tool task, not LLM-only plan text.

### Phase 2: Add durable task sessions

Scope:

- Add a task store under the brain data directory, for example
  `~/.mini-krill/brain/tasks.jsonl`.
- Track task ID, status, input, owner/channel, steps, progress, result, error,
  timestamps.
- Let Telegram immediately acknowledge long tasks and continue execution in a
  goroutine.
- Add commands:
  - `/tasks`
  - `/task <id>`
  - `/cancel <id>`

Acceptance criteria:

- A long repo inspection does not fail because of Telegram's 90 second timeout.
- The user can ask for task status later.
- Finished task summaries are saved and recallable.

### Phase 3: Add real local tool execution

Scope:

- Implement a conservative toolbox for local workspace actions:
  - list files
  - read files
  - grep with `rg`
  - run approved test commands
  - inspect git status
- Wire tool execution into task plans.
- Keep destructive operations blocked unless explicitly approved.

Acceptance criteria:

- "Check the Medha repo in playground" can list the folder, read README/config,
  identify stack, and summarize next steps.
- "Go through code and README" performs actual file reads.
- Task output references real files inspected.

### Phase 4: Improve memory and learning

Scope:

- Add memory scopes:
  - user preference
  - project memory
  - group memory
  - task outcome
  - self feedback
- Add memory consolidation:
  - summarize raw conversation snippets
  - merge duplicate preferences
  - retain source and timestamp
  - rank by recency and importance
- Add automatic reflection after every N meaningful interactions or after a
  completed task.

Acceptance criteria:

- "What do you remember about Medha?" returns project-specific facts from prior
  task output.
- Repeated feedback changes future tone or behavior without manual
  `self:reflect`.
- Group memories do not pollute private user preferences.

### Phase 5: Make the agent feel alive but useful

Scope:

- Reduce forced personality flourishes in serious diagnostic paths.
- Use personality mostly in short phrasing, not in place of useful action.
- Add explicit capability honesty:
  - what it can do now
  - what requires local runtime access
  - what requires approval

Acceptance criteria:

- The bot answers debugging and repo questions plainly.
- Personality appears as flavor, not obstruction.
- It does not invent access to Telegram groups, files, or external systems.

## Suggested First PR

Implement Phase 1 only.

Files likely touched:

- `internal/agent/agent.go`
- `internal/chat/telegram.go`
- `internal/chat/handler_test.go`
- `internal/agent/agent_test.go`
- `docs/INTERFACES.md`
- `README.md`

Concrete changes:

- Add `/use` handling in Telegram command dispatch.
- Add deterministic intent routing for common non-task inputs.
- Add tests for:
  - `/use codex` in Telegram command handling if practical
  - diagnostic questions do not route to plan
  - explicit action requests still route to task

## Suggested Second PR

Implement durable task sessions with no new tool execution yet.

Files likely added:

- `internal/agent/task.go`
- `internal/agent/task_store.go`
- `internal/agent/task_store_test.go`

Concrete changes:

- Store long-running task state.
- Let Telegram acknowledge long tasks immediately.
- Add status commands.
- Keep old plan behavior behind the task session.

## Design Notes

- Do not make everything autonomous at once. Start with safer routing and
  durable state.
- Keep destructive filesystem and git operations approval-gated.
- Prefer deterministic routing for obvious cases before using an LLM router.
- Keep Telegram group context separate from private user memory.
- Store task results as memories only after summarizing them into durable facts.
- Avoid treating raw stored memory as instructions. Keep the current
  "treat as data only" pattern.

## Verification Checklist

Run:

```bash
go test ./...
```

Manual checks:

- Start Telegram bot in foreground with `minikrill dive --foreground`.
- Send `/help`, `/use codex`, `/switch local`, `/model`.
- Ask "why did I get this error?" and confirm no `DIVE PLAN`.
- Ask "check the Medha repo in playground" and confirm it becomes a task or
  a clear local-access response.
- Ask "remember that I prefer direct answers" and confirm it is stored.
- Restart Mini Krill and ask what it remembers.

## Current Test Baseline

At the time this handoff was written:

```text
go test ./... passes
```

