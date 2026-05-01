I built an AI agent from scratch. Today it's open source.

Meet Mini Krill — a local-first AI agent that runs entirely on your machine.

No API keys for the default local path. No cloud account required. Minimal setup.

Install it, run `minikrill init`, and you're chatting with an AI that can live on your hardware. With Ollama, your prompts, conversations, and data stay on your machine.

Here's what makes it different:

-> Runs on Windows, Linux, and macOS. Single binary. ~15MB. Local inference uses Ollama.

-> Talk to it from Telegram, your terminal, a TUI dashboard, or Discord. All interfaces share one unified memory — start a conversation on Telegram from your phone, pick it up in your terminal.

-> Powered by Ollama for fully private local inference. Want more power? Switch to ChatGPT or Claude with one command using your existing subscription. No API keys needed either way.

-> Plan-before-execute. The agent always shows you its plan and waits for your approval before acting. Transparent by design.

-> Lightweight and clean. Built in Go. 18 internal packages. No bloat, no framework overhead.

Inspired by Jarvis and OpenClaw.

https://github.com/srvsngh99/mini-krill

One-liner install:
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.sh | bash

A side note — I paused my 52 Weeks of GenAI Testing journey to build this. The pace of development in AI agents and harnesses has been relentless, and I felt I couldn't afford to just watch from the sidelines. I needed to build, not just test. The GenAI testing series will resume, but in a different form — informed by everything I learned building Mini Krill from the ground up.

I'll be writing more about the architecture and what I learned building this at souravailabs.ai.

I'd love your feedback — try it out and let me know what you think.

#OpenSource #AI #AIAgents #GoLang #LocalFirst #BuildInPublic #SouravAILabs

---
First comment:

Got questions, feature requests, or found a bug? Open an issue here:
https://github.com/srvsngh99/mini-krill/issues

Read the full story behind Mini Krill on my blog:
https://souravailabs.ai/posts/mini-krill-goes-open-source/
