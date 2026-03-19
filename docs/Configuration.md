# Configuration

Steward uses a single YAML configuration file, typically at `/etc/steward/core.yml`.

## Full Reference

```yaml
# ── LLM Provider ──────────────────────────────────────────────
provider: "claude"               # claude | openai | groq | gemini | ollama | openrouter
api_key: ""                      # API key (or set STEWARD_API_KEY env)
model: "claude-sonnet-4-5"     # model identifier
max_tokens: 4096                 # max response tokens
base_url: ""                     # custom endpoint (for ollama/openrouter)

# ── System Prompt ─────────────────────────────────────────────
system_prompt: |
  You are Steward, a helpful AI personal assistant.

# ── Memory ────────────────────────────────────────────────────
memory:
  backend: "badger"              # badger | postgres
  data_dir: "/var/lib/steward/badger"
  short_term_limit: 10           # recent messages in context
  postgres_url: ""               # required if backend=postgres
  embedding:
    enabled: false
    provider: "ollama"           # ollama | openai
    model: "nomic-embed-text"

# ── Shell Tool ────────────────────────────────────────────────
shell:
  enabled: false                 # ⚠️ disabled by default
  timeout: 30                    # seconds
  max_output_bytes: 65536
  blocked_commands:              # always blocked
    - "rm -rf /"
    - "mkfs"
    - "dd"
    - "shutdown"
    - "reboot"
  allowed_dirs: []               # restrict to directories

# ── Voice ─────────────────────────────────────────────────────
voice:
  stt:
    provider: ""                 # groq | openai | local
    api_key: ""
    model: ""
  tts:
    provider: ""                 # openai | elevenlabs | piper
    api_key: ""
    model: ""
    voice: ""

# ── Satellite ─────────────────────────────────────────────────
satellite:
  enabled: false
  listen_addr: "0.0.0.0:9090"
  auth_tokens: []
  tls_cert: ""
  tls_key: ""

# ── Admin Panel ───────────────────────────────────────────────
admin:
  enabled: false
  listen_addr: "0.0.0.0:8080"
  username: "admin"
  password: ""                   # set a strong password!

# ── Telegram ──────────────────────────────────────────────────
telegram:
  token: ""                      # or set TELEGRAM_TOKEN env
  allowed_ids: []                # user/chat ID whitelist

# ── WhatsApp ──────────────────────────────────────────────────
whatsapp:
  listen_addr: "0.0.0.0:8765"
  bridge_url: "http://localhost:3000"
  webhook_secret: ""
  allowed_ids:                   # phone number whitelist
    - "905xxxxxxxxxx"

# ── Integrations ──────────────────────────────────────────────
integrations_dir: "/etc/steward/integrations"
```

## Environment Variables

Environment variables **override** config file values:

| Variable | Overrides | Example |
|----------|-----------|---------|
| `STEWARD_API_KEY` | `api_key` | `sk-...` |
| `STEWARD_PROVIDER` | `provider` | `groq` |
| `STEWARD_MODEL` | `model` | `llama-3.3-70b` |
| `STEWARD_BASE_URL` | `base_url` | `http://localhost:11434/v1` |
| `TELEGRAM_TOKEN` | `telegram.token` | `123456:ABC...` |
| `ANTHROPIC_API_KEY` | `api_key` (fallback) | `sk-ant-...` |
| `OPENAI_API_KEY` | `api_key` (fallback) | `sk-...` |

## Path Resolution

Relative paths in the config file are resolved against the **config file's directory**:

```yaml
# If config is at /etc/steward/core.yml:
data_dir: "data/badger"    # → /etc/steward/data/badger
data_dir: "/var/lib/data"  # → /var/lib/data (absolute, unchanged)
```

> **Tip:** When using systemd with `ProtectSystem=strict`, use absolute paths pointing to `/var/lib/steward/`.

## CLI Flags

```
steward [flags]

  --config string    config file path (default "config/core.yml")
  --channel string   channel: telegram | whatsapp (default "telegram")
  --log-level string debug | info | warn | error (default "info")
  --version          print version and exit
```
