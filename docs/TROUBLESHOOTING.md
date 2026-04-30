# Troubleshooting

## `minikrill` command not found

Restart the terminal after installation. If installed to `~/.local/bin`, add it to PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

On Windows, restart PowerShell after the installer updates the user PATH.

## Ollama is not running

```bash
minikrill ollama ensure
minikrill ollama status
```

On Windows, install Ollama manually from `https://ollama.com/download` if automatic setup is unavailable.

## Codex login required

```bash
codex login
minikrill run /models
```

Mini Krill delegates subscription auth to the official Codex CLI.

## Claude login required

```bash
claude auth login
minikrill run /models
```

Mini Krill delegates subscription auth to the official Claude CLI.

## Memory is not visible

Check stored memories:

```bash
minikrill brain status
minikrill run "what do you remember"
```

For isolated testing, use a temporary data directory:

```bash
KRILL_DATA_DIR=/tmp/minikrill-test minikrill run "remember that I prefer short answers"
KRILL_DATA_DIR=/tmp/minikrill-test minikrill run "what do you remember"
```

## Reminder did not fire

Run the scheduler:

```bash
minikrill dive --foreground
```

Check local reminder state:

```bash
minikrill reminders list
minikrill reminders due
```
