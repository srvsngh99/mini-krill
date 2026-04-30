# Interfaces

Mini Krill can be used from pure CLI by default, then extended to Telegram and Discord.

## Pure CLI

This is the recommended default path.

Install:

```bash
curl -fsSL https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.sh | bash
```

Windows:

```powershell
irm https://raw.githubusercontent.com/srvsngh99/mini-krill/main/scripts/install.ps1 | iex
```

Initialize:

```bash
minikrill init
```

Use local Ollama:

```bash
minikrill ollama ensure
minikrill run /models
minikrill chat
```

Use subscription CLIs:

```bash
codex login
claude auth login
minikrill chat
```

Inside chat:

```text
/models
/use local
/use codex
/use claude
remember that I prefer short answers
what do you remember
```

One-shot CLI usage:

```bash
minikrill run "summarize this repo"
minikrill run "remember that I prefer concise replies"
minikrill run "what do you remember"
```

## Telegram

1. Create a bot with Telegram BotFather.
2. Copy the bot token.
3. Configure Mini Krill.

Using environment variables:

```bash
export KRILL_TELEGRAM_TOKEN="123456:bot-token"
minikrill dive --foreground
```

Using setup wizard:

```bash
minikrill init
# choose provider
# answer yes to "Enable Telegram bot?"
minikrill dive --foreground
```

Optional allow-list:

```bash
export KRILL_TELEGRAM_ALLOWED_IDS="123456789,987654321"
export KRILL_TELEGRAM_TOKEN="123456:bot-token"
minikrill dive --foreground
```

Telegram commands:

```text
/start
/help
/status
/model
/models
/switch local
/switch codex gpt-5.5
/switch claude sonnet
/fact
/plan
```

Send a one-off Telegram notification:

```bash
export KRILL_TELEGRAM_TOKEN="123456:bot-token"
export KRILL_TELEGRAM_CHAT_ID="123456789"
minikrill notify "Mini Krill is running"
```

## Discord

1. Create a Discord application in the Discord Developer Portal.
2. Create a bot and copy the bot token.
3. Enable the Message Content Intent for the bot.
4. Invite the bot to your server with permission to read and send messages.
5. Configure Mini Krill.

Using environment variables:

```bash
export KRILL_DISCORD_TOKEN="discord-bot-token"
minikrill dive --foreground
```

Using setup wizard:

```bash
minikrill init
# choose provider
# answer yes to "Enable Discord bot?"
minikrill dive --foreground
```

Optional server/channel filtering in `~/.mini-krill/config.yaml`:

```yaml
discord:
  enabled: true
  token: ""
  guild_id: "your-server-id"
  channel_id: "your-channel-id"
```

Discord commands:

```text
!help
!status
!fact
!plan
```

You can also mention the bot or DM it. Provider switching and memory commands are handled as normal chat text:

```text
/models
/use codex
remember that I prefer concise replies
what do you remember
```
