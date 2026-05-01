I built an AI agent from scratch. Today it's open source.

Mini Krill is a local-first AI agent that runs entirely on your machine. No API keys needed. No cloud dependency. Just you, your terminal, and an LLM running locally through Ollama.

Why did I build it? Because I wanted to understand how autonomous agents actually work — not by reading papers, but by implementing the full loop: reasoning, planning, approval, execution, and memory.

What makes it different:

- Runs on Windows, Linux, and macOS — single binary, zero dependencies
- Starts with Ollama (fully private), switches to ChatGPT or Claude via official CLIs
- Plan-before-execute: always shows its plan, waits for your approval
- Chat from anywhere — Telegram bot, terminal CLI, TUI dashboard, or Discord bot — with unified memory across all interfaces. Start a conversation on Telegram from your phone, continue it in your terminal
- Built-in security: untrusted content sandboxing, SSRF protection, no telemetry

Built in Go. ~15,000 lines. 18 internal packages. MIT licensed.

Inspired by Jarvis and OpenClaw.

If you're interested in how AI agents reason, plan, and act — or if you just want a practical local AI assistant — check it out:

https://github.com/srvsngh99/mini-krill

One-liner install:
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.sh | bash

I'll be writing more about the architecture decisions and what I learned building this on souravailabs.ai.

#OpenSource #AI #AIAgents #GoLang #LocalFirst #BuildInPublic #SouravAILabs
