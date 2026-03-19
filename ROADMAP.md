# Steward Roadmap

## ✅ Completed (v1.2.x)

- [x] Multi-provider LLM support (Claude, OpenAI, Groq, Gemini, Ollama, OpenRouter)
- [x] WhatsApp & Telegram channels
- [x] Admin panel with dashboard, settings, integrations, channels
- [x] Integration system (Spotify, Home Assistant, Jellyfin, qBittorrent, Google)
- [x] Voice support (STT/TTS with Whisper, Piper, ElevenLabs)
- [x] Satellite devices (remote microphone/speaker)
- [x] Scheduler / Cron jobs
- [x] Embedding-based dynamic tool selection
- [x] First-run onboarding wizard
- [x] Date/time injection into system prompt
- [x] WhatsApp allow list (phone number based)
- [x] Admin panel restart button
- [x] Embedding model download UI (HuggingFace, Ollama)

## 🔜 Next Up

### Hybrid AI: BitNet + LLM (Two-Tier Architecture)
Use a local 1-bit BitNet model for fast tool dispatch, keep the cloud LLM for personality and conversation.

```
User message → BitNet (local, instant, free)
  ├─ intent: "tool_call" → dispatch tool directly (no LLM needed)
  └─ intent: "conversation" → forward to LLM (Claude/GPT personality)
```

- **BitNet b1.58 2B4T** (~0.4GB RAM) or **Llama3-8B-1.58** (~1.5GB) via `bitnet.cpp`
- Intent classification + tool parameter extraction runs locally on CPU
- Simple commands ("ışığı aç", "müzik çal") never hit the cloud API
- Complex/conversational queries go to LLM as usual
- Drastically reduces API costs and latency for home automation

### Tool Knowledge Base (Embedding Cache)
Cache tool results as embeddings for semantic retrieval.

- Cache HA entity lists, Spotify playlists, Jellyfin libraries
- Semantic search: "yatak odası ışığı" → `light.yatak_odasi`
- TTL-based cache invalidation with manual refresh
- Reduces API calls and token usage

### Multi-User Support
Per-user memory, sessions, and permissions.

- User identification per channel
- Separate conversation history per user
- Admin can manage users from panel
- Role-based tool access (e.g., disable shell for certain users)

## 💡 Ideas

- **RAG (Retrieval Augmented Generation)** — index documents/PDFs, query via embeddings
- **Plugin marketplace** — community-contributed integrations
- **Web UI chat** — chat with Steward from the admin panel
- **Notification system** — proactive alerts via WhatsApp/Telegram
- **Multi-language voice** — auto-detect language for STT/TTS
- **Docker Compose one-liner** — Steward + WhatsApp bridge + Ollama
- **iOS/Android companion app** — push notifications, quick actions

## 🔮 Vision

### AI-Driven NAS
Turn Steward into an intelligent file/media server manager.

- Smart file organization and search via natural language
- Media library management (photos, videos, music)
- Automated backups with AI-driven scheduling
- Storage monitoring and alerts
- "Find all photos from last summer" → semantic search

### AI-Driven Home Security
Network and home security powered by AI.

- **Firewall management** — AI-assisted rule creation and anomaly detection
- **DHCP/DNS management** — device tracking, custom DNS rules via chat
- **Alien device scanner** — detect unknown devices on the network, alert via WhatsApp
- **VPN management** — create/revoke WireGuard/OpenVPN profiles via chat
- Intrusion detection with AI-powered log analysis
- "Show me all devices on my network" → scan + classify + alert on unknowns
