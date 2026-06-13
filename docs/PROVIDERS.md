# Providers

Mini Krill defaults to **KrillLM**, its local stack running gemma 12b, and can switch to subscription-backed CLIs. API-key providers are kept for later/advanced use.

## KrillLM (default)

Mini Krill's branded local provider: an Ollama-backed stack whose primary model is **gemma 12b** (`gemma4:12b-mlx`). Best for private local chat and planning out of the box. Identifies as `krilllm` so switching and persistence treat it as a first-class target, but runs on the same local Ollama daemon as the `ollama` provider.

```bash
minikrill ollama ensure        # installs Ollama + pulls gemma 12b
minikrill chat
/use krilllm
```

Aliases: `krilllm`, `krill`, `krill-lm`. Model shorthand: `gemma12b`.

## Ollama (custom local model)

The plain Ollama provider when you want to pick your own local model instead of the gemma 12b default — e.g. a lighter model on a low-RAM machine.

```bash
minikrill ollama ensure
minikrill chat
/use ollama
/use llama3.2:3b
```

Lighter alternatives: `gemma3:4b`, or `llama3.2:3b` as a low-memory fallback.

## Codex CLI

Best for coding and repo-aware work when the user has a ChatGPT subscription.

```bash
codex login
minikrill chat
/use codex
```

Mini Krill advertises only `auto` for Codex and delegates the actual model choice to the Codex CLI — it does not hard-code model IDs, which drift between CLI releases.

Mini Krill does not read or store Codex OAuth tokens. The official Codex CLI owns authentication.

## Claude Code

Best for coding, analysis, and terminal workflows when the user has a Claude subscription.

```bash
claude auth login
minikrill chat
/use claude
```

Current CLI aliases seen in subscription installs:

- `auto`
- `opus`
- `sonnet`
- `haiku`

Mini Krill does not read or store Claude OAuth tokens. The official Claude CLI owns authentication.

## Chat Commands

```text
/model
/models
/use krilllm
/use ollama
/use codex
/use claude
/auth codex
/auth claude
```
