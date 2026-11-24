# Memory and Preferences

Mini Krill has local memory. The model itself is not fine-tuned and does not permanently learn new weights.

Memory works by saving local data under `~/.mini-krill` and sending relevant context to the active provider.

## What Is Stored

- Recent chat turns in `~/.mini-krill/brain/conversations.jsonl`
- Durable memories and preferences in `~/.mini-krill/brain/memories/`
- Config in `~/.mini-krill/config.yaml`

Memory files are written with owner-only permissions where supported by the OS.

`conversations.jsonl` is append-only in the current release. If you run a long-lived bot, periodically archive or remove older conversation history until built-in rotation is added.

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

## Semantic memory embedder

Semantic social memory embeds with EmbeddingGemma 2 (`embed_url` `http://127.0.0.1:57458`, model `embeddinggemma-2-mlx-4bit-dyn-text`, 768-d, unit-normalised). Stored text is embedded with task `Document`, lookups with `SearchQuery`. Vectors live in `<collection>_eg2`; the old bge collections stay untouched.

Roll back: set `social_memory.embed_url` to the old embedder, `embed_model: bge-base-en`, and for the brain path the env vars `COLONY_EMBED_URL` and `COLONY_EMBED_MODEL=bge-base-en`, then restart. The bge model name selects the old collection names.
