# Steward — AI Personal Assistant

> Built in protest of Sam Altman's acquisition of OpenClaw.
> Powered by Claude — by choice, not by chance.

Steward is a modular, self-hosted AI personal assistant that uses the
**Anthropic Claude API** as its brain. It connects to your smart home,
media server, and download client — all through a chat interface on
Telegram or WhatsApp.

---

## Features

- **Claude-powered** — `claude-3-5-sonnet-20241022` out of the box
- **Persistent memory** — SQLite-backed conversation history per user/session
- **Tool use** — Native Claude tool-use for real actions (not just text)
- **Pluggable integrations** — Home Assistant, Jellyfin, qBittorrent
- **Pluggable channels** — Telegram (ready), WhatsApp (webhook adapter)
- **Docker-first** — single `docker compose up` deployment
- **Fault-isolated** — each integration has its own config; one broken config never takes down the others

---

## Architecture

```
steward/
├── steward/
│   ├── core.py            # Main agent loop, Claude API
│   ├── memory.py          # SQLite conversation memory
│   ├── tools.py           # Tool registry & dispatcher
│   └── integrations/
│       ├── base.py        # Abstract base integration
│       ├── homeassistant.py
│       ├── jellyfin.py
│       └── qbittorrent.py
├── channels/
│   ├── telegram.py        # Telegram bot (python-telegram-bot v20)
│   └── whatsapp.py        # WhatsApp webhook adapter
├── config/
│   ├── core.yml           # API keys, model, system prompt
│   └── integrations/
│       ├── homeassistant.yml.example
│       ├── jellyfin.yml.example
│       └── qbittorrent.yml.example
├── Dockerfile
├── docker-compose.yml
├── install.sh
└── requirements.txt
```

---

## Quick Start

### Option A — Docker (recommended)

```bash
# 1. Clone
git clone https://github.com/brooqs/steward.git && cd steward

# 2. Configure
#    Edit config/core.yml — add anthropic_api_key and telegram_token
#    Copy and edit any integration configs you want to enable:
cp config/integrations/homeassistant.yml.example config/integrations/homeassistant.yml
# then fill in url + token

# 3. Run
docker compose up -d

# 4. Logs
docker compose logs -f steward
```

### Option B — One-command installer

```bash
curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash
```

### Option C — Manual Python

```bash
git clone https://github.com/brooqs/steward.git && cd steward
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
mkdir -p data

# Edit config/core.yml then:
python -m channels.telegram
```

---

## Configuration

### `config/core.yml`

| Key | Description | Default |
|-----|-------------|---------|
| `anthropic_api_key` | Anthropic API key | env `ANTHROPIC_API_KEY` |
| `model` | Claude model ID | `claude-3-5-sonnet-20241022` |
| `max_tokens` | Max response tokens | `4096` |
| `system_prompt` | Persona for Claude | See file |
| `telegram_token` | Telegram bot token | env `TELEGRAM_TOKEN` |
| `db_path` | SQLite database path | `data/memory.db` |
| `max_history_messages` | Messages kept per session | `50` |

You can also set values via environment variables. A `.env` file is
supported by Docker Compose automatically.

---

## Integrations

Enable an integration by copying its `.yml.example` file and filling in
the credentials:

```bash
cp config/integrations/homeassistant.yml.example config/integrations/homeassistant.yml
```

### Home Assistant

```yaml
url: "http://homeassistant.local:8123"
token: "your-long-lived-access-token"
```

**Tools exposed:** `ha_get_entity_state`, `ha_call_service`, `ha_list_entities`

### Jellyfin

```yaml
url: "http://jellyfin.local:8096"
api_key: "your-api-key"
```

**Tools exposed:** `jellyfin_search`, `jellyfin_sessions`, `jellyfin_recently_added`

### qBittorrent

```yaml
url: "http://localhost:8080"
username: "admin"
password: "your-password"
```

**Tools exposed:** `qbt_list_torrents`, `qbt_add_torrent`, `qbt_pause_torrent`, `qbt_resume_torrent`

---

## Channels

### Telegram

1. Create a bot via [@BotFather](https://t.me/BotFather)
2. Add the token to `config/core.yml` → `telegram_token`
3. Start Steward — message your bot

**Bot commands:**
- `/start` — welcome message
- `/clear` — wipe your conversation history
- `/status` — list active tools

### WhatsApp

Requires a `whatsapp-web.js` bridge running locally. See
`channels/whatsapp.py` for details on setup. Then:

```bash
WA_BRIDGE_URL=http://localhost:3000 python -m channels.whatsapp
```

---

## Development

```bash
# Install dev deps
pip install -r requirements.txt

# Run tests (when present)
python -m pytest

# Lint
ruff check steward/ channels/
```

### Adding an integration

1. Create `steward/integrations/myservice.py`
2. Inherit from `BaseIntegration`
3. Implement `load_config()`, `get_tools()`, `health_check()`
4. Create `config/integrations/myservice.yml.example`
5. Register in `channels/telegram.py` (or your entry point)

---

## License

MIT — do whatever you want, just don't blame us.
