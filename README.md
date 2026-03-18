# 🤖 Steward — AI Personal Assistant

> Self-hosted AI assistant that runs as a single binary. Multi-provider, voice-enabled, with remote satellite clients.

[![CI](https://github.com/brooqs/steward/actions/workflows/ci.yml/badge.svg)](https://github.com/brooqs/steward/actions/workflows/ci.yml)
[![Release](https://github.com/brooqs/steward/releases/latest/badge.svg)](https://github.com/brooqs/steward/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## ✨ Features

- 🧠 **6 LLM Providers** — Claude, OpenAI, Groq, Gemini, Ollama, OpenRouter
- 🎙️ **Voice System** — STT (Groq Whisper, OpenAI, whisper.cpp) + TTS (OpenAI, ElevenLabs, Piper)
- 🛰️ **Satellite System** — JARVIS-style remote clients with audio, shell exec, system management
- 🔌 **Hot-Reload Integrations** — Home Assistant, Jellyfin, qBittorrent (add your own!)
- 💾 **Flexible Memory** — BadgerDB (default) or PostgreSQL with pgvector
- 🔍 **Semantic Search** — Embedding-based long-term memory (OpenAI, Ollama)
- 🌐 **Web Admin Panel** — Dark theme dashboard with config editor
- 📱 **Channels** — Telegram, WhatsApp
- 🔐 **Security-First** — Shell blocklist, token auth, basic auth, TLS support
- 📦 **Single Binary** — No runtime dependencies, cross-platform (Linux, macOS, Windows)

---

## 🚀 Quick Start

```bash
# One-liner install
curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash

# Configure
sudo cp /etc/steward/core.yml.example /etc/steward/core.yml
sudo nano /etc/steward/core.yml   # set provider + api_key

# Start
sudo systemctl enable --now steward

# Logs
journalctl -u steward -f
```

### From Source

```bash
git clone https://github.com/brooqs/steward.git
cd steward
go build -o steward ./cmd/steward
go build -o steward-satellite ./cmd/satellite
./steward --config config/core.yml --channel telegram
```

### Docker

```bash
cp config/core.yml.example config/core.yml
docker compose up -d
```

---

## 📚 Documentation

Full documentation available in the **[Wiki](https://github.com/brooqs/steward/wiki)**.

| Page | Description |
|------|-------------|
| [Installation](https://github.com/brooqs/steward/wiki/Installation) | Binary, deb/rpm, Docker, install script |
| [Configuration](https://github.com/brooqs/steward/wiki/Configuration) | core.yml reference, environment variables |
| [Providers](https://github.com/brooqs/steward/wiki/Providers) | LLM provider setup guide |
| [Voice](https://github.com/brooqs/steward/wiki/Voice) | STT/TTS setup |
| [Satellite](https://github.com/brooqs/steward/wiki/Satellite) | JARVIS-style remote client |
| [Integrations](https://github.com/brooqs/steward/wiki/Integrations) | Built-in + custom development |
| [Admin Panel](https://github.com/brooqs/steward/wiki/Admin-Panel) | Web UI for config & monitoring |
| [Security](https://github.com/brooqs/steward/wiki/Security) | Hardening guide |
| [Architecture](https://github.com/brooqs/steward/wiki/Architecture) | System design & module graph |

---

## 🏗️ Architecture

```
steward/
├── cmd/
│   ├── steward/         # Main server binary
│   └── satellite/       # Remote client binary
├── internal/
│   ├── admin/           # Web admin panel (embedded)
│   ├── channel/         # Telegram, WhatsApp
│   ├── config/          # YAML + env config loader
│   ├── core/            # Provider-agnostic agent loop
│   ├── embedding/       # Vector embeddings (OpenAI, Ollama)
│   ├── integration/     # Hot-reload integration system
│   ├── memory/          # BadgerDB, PostgreSQL, semantic search
│   ├── provider/        # LLM adapters (6 providers)
│   ├── satellite/       # WebSocket server + management tools
│   ├── tools/           # Tool registry + shell tool
│   └── voice/           # STT + TTS engine
├── config/              # Example configurations
├── docs/                # Wiki documentation
└── init/                # systemd service
```

---

## 🔐 Security

| Feature | Default | Notes |
|---------|---------|-------|
| Shell tool | **Disabled** | Command blocklist + timeout |
| Telegram | **No whitelist** | Configure `allowed_ids` |
| Satellite | **Disabled** | Token auth + TLS |
| Admin panel | **Disabled** | Basic auth required |

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

## 📝 Credits

Built with ❤️ by **[brooqs](https://github.com/brooqs)**

Architecture design, codebase implementation, and documentation proudly crafted with **[Claude Opus 4](https://www.anthropic.com)** — Anthropic's most capable AI model.

> *"From zero to a production-ready AI assistant in a single session."*

---

## 📄 License

[MIT](LICENSE)
