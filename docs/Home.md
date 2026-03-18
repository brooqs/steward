# Steward — AI Personal Assistant

> Self-hosted AI assistant that runs as a single binary. Multi-provider, voice-enabled, with remote satellite clients.

## ✨ Features

- 🧠 **6 LLM Providers** — Claude, OpenAI, Groq, Gemini, Ollama, OpenRouter
- 🎙️ **Voice System** — STT (Groq Whisper, OpenAI, whisper.cpp) + TTS (OpenAI, ElevenLabs, Piper)
- 🛰️ **Satellite System** — JARVIS-style remote clients with audio, shell exec, system management
- 🔌 **Hot-Reload Integrations** — Home Assistant, Jellyfin, qBittorrent (add your own!)
- 💾 **Flexible Memory** — BadgerDB (default) or PostgreSQL with pgvector
- 🌐 **Web Admin Panel** — Dark theme dashboard with config editor
- 📱 **Channels** — Telegram, WhatsApp
- 🔐 **Security-First** — Shell blocklist, token auth, basic auth, TLS support

## 📚 Documentation

| Page | Description |
|------|-------------|
| [Installation](Installation.md) | Binary, deb/rpm, Docker, install script |
| [Configuration](Configuration.md) | core.yml reference, environment variables |
| [Providers](Providers.md) | LLM provider setup (Claude, OpenAI, Groq, etc.) |
| [Voice](Voice.md) | STT/TTS setup (Whisper, ElevenLabs, Piper) |
| [Satellite](Satellite.md) | Remote client setup, JARVIS-style interaction |
| [Integrations](Integrations.md) | Built-in + custom integration development |
| [Admin Panel](Admin-Panel.md) | Web UI for configuration and monitoring |
| [Security](Security.md) | Hardening guide and best practices |
| [Architecture](Architecture.md) | Module structure and design decisions |

## 🚀 Quick Start

```bash
# Install
curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash

# Configure
sudo cp /etc/steward/core.yml.example /etc/steward/core.yml
sudo nano /etc/steward/core.yml  # set provider + api_key

# Start
sudo systemctl enable --now steward

# Logs
journalctl -u steward -f
```

## 📂 Project Structure

```
steward/
├── cmd/
│   ├── steward/         # Main binary
│   └── satellite/       # Satellite client binary
├── config/              # Example configs
├── internal/
│   ├── admin/           # Web admin panel (embedded)
│   ├── channel/         # Telegram, WhatsApp
│   ├── config/          # Configuration loader
│   ├── core/            # Provider-agnostic agent loop
│   ├── embedding/       # Vector embeddings (ONNX, OpenAI, Ollama)
│   ├── integration/     # Hot-reload integration system
│   ├── memory/          # BadgerDB + PostgreSQL stores
│   ├── provider/        # LLM provider adapters
│   ├── satellite/       # WebSocket server + tools
│   ├── tools/           # Tool registry + shell tool
│   └── voice/           # STT + TTS engine
├── init/                # systemd service
└── docs/                # Documentation (this wiki)
```
