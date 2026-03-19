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

### Tool Knowledge Base (Embedding Cache)
Cache tool results as embeddings for semantic retrieval. Instead of calling integrations every time, search cached results first.

- Cache HA entity lists, Spotify playlists, Jellyfin libraries
- Semantic search: "yatak odası ışığı" → `light.yatak_odasi`
- TTL-based cache invalidation with manual refresh
- Reduces API calls and token usage

### Local ONNX Inference
Run embedding models locally without Ollama dependency.

- Download ONNX model + runtime from HuggingFace
- Pure Go tokenizer (WordPiece for BERT)
- `onnxruntime_go` integration (CGO or purego)
- Fully offline embedding support

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
