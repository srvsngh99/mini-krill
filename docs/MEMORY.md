# Memory and Preferences

Mini Krill has local memory. The model itself is not fine-tuned and does not permanently learn new weights.

Memory works by saving local data under `~/.mini-krill` and sending relevant context to the active provider.

## What Is Stored

- Recent chat turns in `~/.mini-krill/brain/conversations.jsonl`
- Durable memories and preferences in `~/.mini-krill/brain/memories/`
- Config in `~/.mini-krill/config.yaml`

Memory files are written with owner-only permissions where supported by the OS.

## How To Teach It

```text
remember that I prefer short answers
learn that my default coding language is Go
I prefer concise replies
what do you remember
```

The same memory is used across terminal chat, TUI, Telegram, and Discord because Mini Krill stores conversation continuity in a unified channel.

## Provider Switching

Memory belongs to Mini Krill, not to Ollama, Codex, or Claude. Switching providers keeps the same local memories:

```text
/use local
/use codex
/use claude
```

When using Codex or Claude, prompts and injected memory context go through the selected provider CLI.
