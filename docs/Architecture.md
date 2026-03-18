# Architecture

## System Overview

```
                          ┌──────────────────────────────────────────────┐
                          │              Steward Server                  │
                          │                                              │
 Telegram ───────────►    │  ┌──────────┐    ┌──────────┐               │
                          │  │ Channel  │───►│  Core    │               │
 WhatsApp ───────────►    │  │ Layer    │    │  Agent   │               │
                          │  └──────────┘    │          │               │
                          │                  │ ┌──────┐ │  ┌──────────┐ │
 Satellite ──(WebSocket)──│──────────────────│ │ Tool │─│─►│ Provider │ │
                          │                  │ │ Exec │ │  │ (LLM)   │ │
                          │                  │ └──────┘ │  └──────────┘ │
                          │                  │          │               │
 Admin Panel ─(HTTP)──────│──────────────────│ ┌──────┐ │               │
                          │                  │ │Memory│ │               │
                          │                  │ └──────┘ │               │
                          │                  └──────────┘               │
                          └──────────────────────────────────────────────┘
```

## Module Dependency Graph

```
cmd/steward/main.go
  ├── config        (YAML + env loading)
  ├── provider      (LLM adapters)
  │   ├── claude
  │   ├── openai    (+ Groq, Ollama, OpenRouter)
  │   └── gemini
  ├── core          (agent loop, tool dispatch)
  ├── memory        (BadgerDB, PostgreSQL, semantic)
  ├── tools         (registry, shell)
  ├── integration   (loader, hot-reload)
  │   ├── homeassistant
  │   ├── jellyfin
  │   └── qbittorrent
  ├── channel       (telegram, whatsapp)
  ├── voice         (engine, STT, TTS)
  ├── satellite     (server, protocol, tools)
  ├── embedding     (interface, OpenAI, Ollama)
  └── admin         (web panel, embedded static)
```

## Key Design Decisions

### Single Binary
Everything compiles into one executable. The admin panel HTML is embedded via `go:embed`. No runtime dependencies except optional external tools (whisper.cpp, piper, ffmpeg).

### Interface-First
Each subsystem defines a Go interface, then provides implementations:

```go
// provider.Provider → claude, openai, gemini
// memory.Store     → badger, postgres
// embedding.Embedder → openai, ollama
// stt.Provider     → groq, openai, local
// tts.Provider     → openai, elevenlabs, piper
```

This makes adding new providers trivial — implement the interface, add a case to the factory.

### Hot-Reload Integrations
Integrations use `init()` self-registration + `fsnotify` for file watching:

```
init() → integration.Register("name", factory)
                              ↓
fsnotify watches integrations_dir
                              ↓
YAML added → factory(config) → []Tool → registry.RegisterAll()
```

### Security by Default
Dangerous features are disabled by default:
- Shell tool: `enabled: false`
- Satellite: `enabled: false`
- Admin panel: `enabled: false`

### Agent Loop

```
User Message
     ↓
Core.Chat(message)
     ↓
Add to memory → Build context (system prompt + history + tools)
     ↓
Provider.Chat(context) → LLM API call
     ↓
Response has tool calls? ─── No ──→ Return text response
     │
     Yes
     ↓
Execute tools via Registry
     ↓
Append tool results → Loop back to Provider.Chat()
```

The agent loop continues until the LLM returns a text response without tool calls (max 10 iterations to prevent infinite loops).

## Data Flow

### Text Message (Telegram/WhatsApp)
```
User → Channel → Core.Chat() → Provider → LLM → Response → Channel → User
```

### Voice (Satellite)
```
Mic → Satellite → WebSocket → Server → STT → Core.Chat() → TTS → WebSocket → Satellite → Speaker
```

### Remote Command
```
AI decides → satellite_exec tool → WebSocket → Satellite.executeCommand() → stdout/stderr → WebSocket → AI
```
