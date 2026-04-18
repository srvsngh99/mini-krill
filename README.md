<!-- Logo placeholder -->

<div align="center">

# Mini Krill

### Your Crustaceous AI Buddy

**A lightweight, open-source AI agent that runs locally or via cloud providers. Thinks, plans, and acts - with personality.**

[![Version](https://img.shields.io/badge/version-0.1.0-blue.svg)](https://github.com/srvsngh99/mini-krill/releases)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey.svg)]()

</div>

---

## Feature Highlights

- **Local-first** - runs via Ollama, no cloud account needed
- **Cloud optional** - supports OpenAI, Anthropic, and Google with affordable plans
- **Plan-before-execute** - always shows its plan, waits for your approval before acting
- **Personality** - not a boring assistant, a crustaceous AI buddy with soul
- **Plugin system** - YAML-based skill registry for extensible capabilities
- **MCP support** - Model Context Protocol integration for tool interop
- **Telegram and Discord** - built-in chat bot support for both platforms
- **Krill-themed TUI** - beautiful terminal dashboard with real-time status
- **Brain with memory** - remembers context and conversations across sessions
- **Heartbeat monitoring** - always knows its own health and resource usage
- **Doctor and sonar** - diagnostic health checks and quick pings
- **One-command install** - single binary, no runtime dependencies
- **Cross-platform** - Linux, macOS, and Windows
- **Lightweight** - roughly 15MB binary, minimal memory footprint

---

## Quick Start

```bash
# Install (one-liner)
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/master/scripts/install.sh | bash

# Or via Go
go install github.com/srvsngh99/mini-krill/cmd/minikrill@latest

# Setup
minikrill init

# Start chatting
minikrill chat
```

---

## Installation

### Binary Release (recommended)

Download the latest release for your platform from the
[Releases page](https://github.com/srvsngh99/mini-krill/releases).

Extract and move the binary to your PATH:

```bash
tar -xzf mini-krill_<version>_<os>_<arch>.tar.gz
sudo mv minikrill /usr/local/bin/
```

### Curl Installer

```bash
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/master/scripts/install.sh | bash
```

This detects your OS and architecture, downloads the correct binary, and places it in `/usr/local/bin`.

### Go Install

Requires Go 1.22 or later:

```bash
go install github.com/srvsngh99/mini-krill/cmd/minikrill@latest
```

### Docker

```bash
docker pull ghcr.io/srvsngh99/mini-krill:latest
docker run -it --rm ghcr.io/srvsngh99/mini-krill:latest chat
```

Or use docker-compose (see the [Docker section](#docker) below).

### Build from Source

```bash
git clone https://github.com/srvsngh99/mini-krill.git
cd mini-krill
go build -o minikrill ./cmd/minikrill
sudo mv minikrill /usr/local/bin/
```

---

## Usage

### `minikrill init`

Interactive setup wizard. Walks you through choosing an LLM provider (Ollama, OpenAI, Anthropic, or Google), configuring API keys, and setting personality options.

```bash
minikrill init
```

### `minikrill chat`

Start an interactive chat session with your krill agent. The agent thinks, plans, and responds with personality.

```bash
minikrill chat
```

### `minikrill dive`

Start Mini Krill's background services (heartbeat, Telegram/Discord bots, scheduled tasks).

```bash
minikrill dive
```

### `minikrill surface`

Stop a running Mini Krill instance gracefully.

```bash
minikrill surface
```

### `minikrill tui`

Launch the krill-themed terminal dashboard. Shows live status, recent conversations, memory state, and health metrics.

```bash
minikrill tui
```

### `minikrill doctor`

Run a full health diagnostic. Checks LLM connectivity, memory integrity, plugin status, and system resources.

```bash
minikrill doctor
```

### `minikrill sonar`

Quick health ping. Returns a one-line status - useful for scripts and monitoring.

```bash
minikrill sonar
```

### `minikrill version`

Print version, build info, and Go runtime details.

```bash
minikrill version
```

### `minikrill ollama`

Manage the local Ollama installation and models:

```bash
minikrill ollama install    # Install Ollama if not present
minikrill ollama start      # Start the Ollama server
minikrill ollama stop       # Stop the Ollama server
minikrill ollama pull       # Pull a model (e.g., llama3, mistral)
minikrill ollama list       # List downloaded models
minikrill ollama status     # Check if Ollama is running
```

### `minikrill skill`

Manage skills and plugins:

```bash
minikrill skill list        # List all registered skills
```

### `minikrill brain`

Inspect and manage the brain (memory) system:

```bash
minikrill brain status      # Show memory stats and health
minikrill brain recall      # Recall a specific memory by key
minikrill brain forget      # Remove a memory entry
minikrill brain search      # Search memories by keyword
```

---

## Configuration

Mini Krill stores its configuration and data in `~/.mini-krill/`.

### Directory Structure

```
~/.mini-krill/
  config.yaml          # Main configuration
  brain/
    memory.json        # Persistent memory store
    soul.yaml          # Personality and behavior config
    heartbeat.json     # Health state
  skills/              # User-defined skill files
  logs/                # Runtime logs
```

### config.yaml

```yaml
llm:
  provider: ollama          # ollama | openai | anthropic | google
  model: llama3             # Model name
  api_key: ""               # Required for cloud providers
  base_url: ""              # Override endpoint (optional)
  temperature: 0.7
  max_tokens: 4096

personality:
  name: Krill
  style: friendly           # friendly | professional | chaotic
  krill_facts: true         # Sprinkle krill facts in responses

telegram:
  enabled: false
  token: ""

discord:
  enabled: false
  token: ""

mcp:
  servers: []               # MCP server configurations

heartbeat:
  interval: 30s
  checks:
    - llm
    - memory
    - disk
```

### Environment Variables

All config values can be overridden with environment variables:

| Variable | Description |
|---|---|
| `KRILL_LLM_PROVIDER` | LLM provider (ollama, openai, anthropic, google) |
| `KRILL_LLM_API_KEY` | API key for cloud providers |
| `KRILL_LLM_MODEL` | Model name |
| `KRILL_TELEGRAM_TOKEN` | Telegram bot token |
| `KRILL_DISCORD_TOKEN` | Discord bot token |
| `KRILL_DATA_DIR` | Override data directory (default: ~/.mini-krill) |
| `KRILL_LOG_LEVEL` | Log level: debug, info, warn, error |

---

## Architecture

Mini Krill is built as a set of loosely-coupled Go packages:

```
cmd/minikrill/       Entry point and CLI wiring (Cobra)
internal/
  agent/             Core agent loop: think, plan, act
  brain/             Memory, soul, personality, heartbeat
  llm/               LLM provider abstraction (Ollama, OpenAI, etc.)
  plugin/            Skill registry and YAML skill loader
  chat/              Interactive chat session management
  tui/               Terminal UI (Bubble Tea)
  doctor/            Health diagnostics and sonar
  telegram/          Telegram bot adapter
  discord/           Discord bot adapter
  mcp/               MCP client and server registry
  config/            Configuration loading and validation
```

The agent follows a **plan-before-execute** workflow:

1. Receive user input
2. Think - analyze intent and context, consult memory
3. Plan - generate a step-by-step plan
4. Present plan to user and wait for approval
5. Execute - carry out approved steps, update memory
6. Respond - deliver results with personality

---

## Skills and Plugins

### Built-in Skills

Mini Krill ships with a handful of built-in skills:

- **recall** - search and retrieve memories from the brain
- **sysinfo** - report system information (OS, CPU, memory, disk)
- **time** - current time, timezone conversions, countdowns

### Creating a Custom Skill

Skills are defined as YAML files in `~/.mini-krill/skills/` or the project's `skills/` directory:

```yaml
name: weather
description: Get the current weather for a location
trigger: "weather in {location}"
steps:
  - type: http
    method: GET
    url: "https://wttr.in/{{.location}}?format=3"
    capture: result
  - type: respond
    message: "{{.result}}"
```

Skills support these step types:

- `http` - make HTTP requests
- `shell` - run shell commands (sandboxed)
- `respond` - send a message back to the user
- `llm` - call the LLM for sub-reasoning
- `branch` - conditional logic

### MCP Server Configuration

Add MCP servers in `config.yaml`:

```yaml
mcp:
  servers:
    - name: filesystem
      command: npx
      args: ["-y", "@anthropic/mcp-filesystem", "/home/user/documents"]
    - name: github
      command: npx
      args: ["-y", "@anthropic/mcp-github"]
      env:
        GITHUB_TOKEN: "your-token"
```

Mini Krill automatically discovers tools from connected MCP servers and makes them available to the agent.

---

## Docker

A `docker-compose.yaml` is provided for running Mini Krill alongside Ollama:

```bash
# Start Mini Krill + Ollama
docker-compose up -d

# Pull a model inside the Ollama container
docker-compose exec ollama ollama pull llama3

# Chat
docker-compose exec minikrill minikrill chat

# Stop
docker-compose down
```

The compose file mounts `~/.mini-krill` as a volume so configuration and memory persist across restarts.

---

## Development

### Prerequisites

- Go 1.22 or later
- (Optional) Ollama for local LLM testing

### Building

```bash
git clone https://github.com/srvsngh99/mini-krill.git
cd mini-krill
go build -o minikrill ./cmd/minikrill
```

### Running Tests

```bash
# All tests
go test ./...

# With verbose output
go test -v ./...

# Specific package
go test -v ./internal/agent/...

# With coverage
go test -cover ./...
```

### Linting

```bash
golangci-lint run
```

### Project Structure

```
mini-krill/
  cmd/minikrill/         CLI entry point
  internal/              All internal packages
  config/                Default config files
  skills/                Built-in skill definitions
  scripts/               Install and utility scripts
  testdata/              Test fixtures
  .github/workflows/     CI and release pipelines
  go.mod                 Go module definition
  LICENSE                MIT license
  README.md              This file
  CHANGELOG.md           Version history
  VERSION                Current version
```

---

## FAQ

**Q: Do I need a cloud API key to use Mini Krill?**

No. Mini Krill works fully offline with Ollama. Run `minikrill ollama install` and `minikrill ollama pull llama3` to get started with no cloud dependency.

**Q: How much memory does it use?**

The Mini Krill binary itself uses around 20-40MB of RAM. LLM memory usage depends on your provider - Ollama models vary by size (a 7B model uses roughly 4-8GB of RAM).

**Q: Can I use it as a Telegram or Discord bot?**

Yes. Run `minikrill init` and enable Telegram or Discord during setup. Then run `minikrill dive` to start the bot in the background.

**Q: Is my data sent to the cloud?**

Only if you choose a cloud LLM provider. With Ollama, everything stays on your machine. Mini Krill never phones home or sends telemetry.

**Q: How do I update Mini Krill?**

Run the install script again, or `go install github.com/srvsngh99/mini-krill/cmd/minikrill@latest` for the latest version.

---

## Contributing

Contributions are welcome. Here is how to get started:

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/your-feature`
3. Make your changes and add tests
4. Run `go test ./...` and `golangci-lint run`
5. Commit with a clear message
6. Open a Pull Request

Please read the code of conduct and ensure your PR passes CI before requesting review.

---

## Credits

Created by **Sourav Singh** | **Sourav AI Labs**

Inspired by [DeepKrill](https://github.com/srvsngh99/deepkrill) - the full-featured AI agent platform.

---

## License

[MIT](LICENSE)

---

<div align="center">
<sub>
Fun krill fact: Antarctic krill (Euphausia superba) have a combined biomass of around 379 million tonnes - making them one of the most abundant animal species on Earth. They are tiny but mighty, just like this CLI tool.
</sub>
</div>
