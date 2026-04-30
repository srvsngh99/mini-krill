# Automation Workflows

Mini Krill's public MVP automation surface is local-first. Gmail is intentionally on hold until OAuth verification and user-data handling are ready.

## Durable Reminders

```bash
minikrill remind "call mom tomorrow 9am"
minikrill remind "check the build" --at 30m
minikrill reminders list
minikrill reminders due
minikrill reminders done <id>
minikrill reminders delete <id>
```

Run the scheduler:

```bash
minikrill dive --foreground
```

Reminders are stored locally in `~/.mini-krill/reminders.jsonl`.

## File Summarization

```bash
minikrill summarize README.md
minikrill summarize ./docs
```

Supported first-pass file types include text, Markdown, JSON, YAML, TOML, CSV, logs, common code files, HTML, and CSS.

## Web Summarization

```bash
minikrill web read https://example.com
minikrill web summarize https://example.com
```

Web content is fetched, stripped to readable text, wrapped as untrusted content, then summarized by the active provider.

## Research

```bash
minikrill research "best local model for 8GB RAM"
```

Research searches the web, fetches a small set of pages, and summarizes with source URLs. Search and fetched pages are treated as untrusted content.

## Prompt Injection Protection

Mini Krill cannot make prompt injection impossible. The mitigation model is defense in depth:

- Files, web pages, and search results are labeled as untrusted content.
- Untrusted content is quoted and length-limited before it reaches the model.
- Retrieved content is never allowed to add tools, change policies, or create reminders by itself.
- Actionful workflows stay behind explicit commands or plan approval.
- Gmail is on hold until OAuth and user-data handling are ready.
