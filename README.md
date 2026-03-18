# Steward — AI Personal Assistant

> A self-hosted, modular AI assistant built in Go. Runs as a single binary.
> Supports multiple LLM providers, smart home integrations, and chat channels.

---

## Features

- **Multi-Provider** — Claude, OpenAI, Groq, Gemini, Ollama, OpenRouter
- **Smart Memory** — BadgerDB-backed conversation history (PostgreSQL optional)
- **Tool Use** — Native LLM function calling for real actions
- **Shell Access** — Execute system commands (security-hardened, disabled by default)
- **Pluggable Integrations** — Home Assistant, Jellyfin, qBittorrent
- **Hot-Reload** — Add/remove integrations without restarting
- **Chat Channels** — Telegram, WhatsApp
- **Security-First** — User whitelisting, command blocklist, webhook secrets
- **Single Binary** — No runtime dependencies, cross-platform

---

## Quick Start

### Build & Run (Native)

```bash
# Build
go build -o steward ./cmd/steward

# Configure
cp config/core.yml.example config/core.yml
# Edit config/core.yml — add API key, Telegram token, etc.

# Run
./steward --config config/core.yml --channel telegram
```

### Docker (Optional)

```bash
cp config/core.yml.example config/core.yml
# Edit config/core.yml

docker compose up -d
docker compose logs -f steward
```

---

## Configuration

### `config/core.yml`

| Key | Description | Default |
|-----|-------------|---------|
| `provider` | LLM provider | `claude` |
| `api_key` | Provider API key | env `STEWARD_API_KEY` |
| `model` | Model ID | `claude-sonnet-4-5` |
| `base_url` | Custom endpoint (Ollama, etc.) | — |
| `memory.backend` | Storage backend | `badger` |
| `shell.enabled` | Enable shell tool | `false` |
| `telegram.token` | Telegram bot token | env `TELEGRAM_TOKEN` |
| `telegram.allowed_ids` | Authorized user/chat IDs | `[]` (all) |

### Integrations

Enable an integration by copying its example config:

```bash
cp config/integrations/homeassistant.yml.example config/integrations/homeassistant.yml
# Edit and fill in credentials
```

**Hot-reload**: Add or remove YAML files while Steward is running — integrations load/unload automatically.

---

## Supported Providers

| Provider | Tool Use | Config |
|----------|----------|--------|
| **Claude** (Anthropic) | ✅ | `provider: claude` |
| **OpenAI** | ✅ | `provider: openai` |
| **Groq** | ✅ | `provider: groq` |
| **Gemini** (Google) | ✅ | `provider: gemini` |
| **Ollama** (Local) | ✅ | `provider: ollama`, `base_url: http://localhost:11434/v1` |
| **OpenRouter** | ✅ | `provider: openrouter` |

---

## Security

- **Telegram whitelisting**: Set `telegram.allowed_ids` to restrict access
- **Shell tool**: Disabled by default, has command blocklist and timeout
- **WhatsApp webhook secret**: Set `whatsapp.webhook_secret` for validation
- **Integration isolation**: Each integration has its own config; broken configs don't affect others

---

## Architecture

```
cmd/steward/main.go           → Entry point, CLI, wiring
internal/
├── config/                    → YAML + env config loader
├── provider/                  → LLM adapters (Claude, OpenAI, Gemini)
├── core/                      → Agent loop (provider-agnostic)
├── tools/                     → Tool registry + shell tool
├── memory/                    → Conversation storage (Badger, Postgres)
├── integration/               → Integration interface + hot-reload
│   ├── homeassistant/
│   ├── jellyfin/
│   └── qbittorrent/
└── channel/                   → Chat adapters
    ├── telegram/
    └── whatsapp/
```

---

## Roadmap

- [ ] Voice: STT (Whisper) + TTS (Piper, ElevenLabs, OpenAI)
- [ ] Satellite client: remote voice I/O + system management
- [ ] Embedding-based long-term memory (ONNX)
- [ ] IDE agent for code assistance
- [ ] AI-first NAS and home security

---

## License

MIT
