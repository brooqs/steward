# LLM Providers

Steward supports 6 LLM providers through a unified interface. All providers support tool/function calling.

## Claude (Anthropic)

```yaml
provider: "claude"
api_key: "sk-ant-..."
model: "claude-sonnet-4-5"
max_tokens: 4096
```

**Models:** `claude-sonnet-4-5`, `claude-sonnet-4-20250514`, `claude-3-5-haiku-20241022`

## OpenAI

```yaml
provider: "openai"
api_key: "sk-..."
model: "gpt-4o"
```

**Models:** `gpt-4o`, `gpt-4o-mini`, `gpt-4-turbo`, `o1`, `o3-mini`

## Groq

```yaml
provider: "groq"
api_key: "gsk_..."
model: "llama-3.3-70b-versatile"
```

Uses OpenAI-compatible API. **Models:** `llama-3.3-70b-versatile`, `llama-3.1-8b-instant`, `mixtral-8x7b-32768`, `gemma2-9b-it`

> **Tip:** Groq offers free API access with generous rate limits.

## Google Gemini

```yaml
provider: "gemini"
api_key: "..."
model: "gemini-2.0-flash"
```

**Models:** `gemini-2.0-flash`, `gemini-1.5-pro`, `gemini-1.5-flash`

## Ollama (Local)

```yaml
provider: "ollama"
model: "llama3.2"
base_url: "http://localhost:11434/v1"
# api_key not required
```

**Setup:**
```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull a model
ollama pull llama3.2

# Steward connects automatically
```

**Models:** Any model available via `ollama list`.

## OpenRouter

```yaml
provider: "openrouter"
api_key: "sk-or-..."
model: "anthropic/claude-sonnet-4-5"
base_url: "https://openrouter.ai/api/v1"
```

Access 100+ models through a single API. See [openrouter.ai/models](https://openrouter.ai/models).

## Switching Providers

You can switch providers at any time by editing `core.yml` and restarting:

```bash
sudo nano /etc/steward/core.yml
sudo systemctl restart steward
```

Or via the [Admin Panel](Admin-Panel.md) web UI.
