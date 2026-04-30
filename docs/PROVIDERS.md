# Providers

Mini Krill starts with local Ollama and can switch to subscription-backed CLIs. API-key providers are kept for later/advanced use.

## Local Ollama

Best for private local chat and planning.

```bash
minikrill ollama ensure
minikrill chat
/use local
```

Recommended model: `gemma3:4b`

Low-memory fallback: `llama3.2:3b`

## Codex CLI

Best for coding and repo-aware work when the user has a ChatGPT subscription.

```bash
codex login
minikrill chat
/use codex
```

Current CLI choices seen in subscription installs:

- `auto`
- `gpt-5.5`
- `gpt-5.4`
- `gpt-5.4-mini`
- `gpt-5.3-codex`
- `gpt-5.2`

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
/use local
/use codex
/use claude
/auth codex
/auth claude
```
