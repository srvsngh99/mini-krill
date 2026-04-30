# Installation and Setup

## Supported Platforms

Mini Krill release builds target:

- macOS: Intel and Apple Silicon
- Linux: amd64 and arm64
- Windows: amd64 and arm64

## Install

macOS/Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.ps1 | iex
```

Go install:

```bash
go install github.com/srvsngh99/mini-krill/cmd/minikrill@latest
```

## First Run

```bash
minikrill init
minikrill doctor
minikrill chat
```

Mandatory setup:

- Choose one provider in `minikrill init`.

Optional setup:

- Install and run Ollama for local/private use.
- Run `codex login` for ChatGPT subscription-backed Codex.
- Run `claude auth login` for Claude subscription-backed Claude Code.
- Add Telegram or Discord bot tokens for chat integrations.

Recommended local provider:

```bash
minikrill ollama ensure
minikrill ollama pull gemma3:4b
```

Low-memory local fallback:

```bash
minikrill ollama pull llama3.2:3b
```
