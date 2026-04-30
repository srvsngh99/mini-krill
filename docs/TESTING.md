# Testing Checklist

Run these before tagging or publishing a release.

## Basic CLI

```bash
minikrill --version
minikrill --help
minikrill doctor
minikrill run /models
```

## Provider Switching

Inside `minikrill chat`:

```text
/models
/use local
/model
/use codex
/model
/use claude
/model
```

Expected:

- Ollama works after `minikrill ollama ensure`.
- Codex requires `codex login`.
- Claude requires `claude auth login`.
- Mini Krill does not ask for API keys for the initial subscription CLI path.

## Memory

```bash
minikrill run "remember that I prefer short answers"
minikrill run "what do you remember"
```

Expected:

- The preference appears in memory.
- A later chat can use that preference.

## Build Checks

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 go build -o /tmp/minikrill-static-review ./cmd/minikrill
```

## Website

From the Sourav AI Labs website repo:

```bash
hugo --minify --destination /tmp/souravailabs-public
```
