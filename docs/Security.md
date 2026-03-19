# Security

Steward is designed with security-first defaults. This guide covers hardening for production use.

## Security Defaults

| Feature | Default | Reason |
|---------|---------|--------|
| Shell tool | **Disabled** | Prevents unintended command execution |
| Telegram | **No whitelist** | Must configure `allowed_ids` |
| WhatsApp | **No whitelist** | Must configure `allowed_ids` (phone numbers) |
| Satellite | **Disabled** | Requires explicit auth tokens |
| Admin panel | **Disabled** | Requires explicit credentials |

## Shell Tool Hardening

When enabling the shell tool:

```yaml
shell:
  enabled: true
  timeout: 30                  # kill long-running commands
  max_output_bytes: 65536      # prevent memory exhaustion
  blocked_commands:
    - "rm -rf /"
    - "rm -rf /*"
    - "mkfs"
    - "dd"
    - "shutdown"
    - "reboot"
    - "init 0"
    - "init 6"
    - "chmod -R 777 /"
    - ":(){ :|:& };:"         # fork bomb
    - "> /dev/sda"
  allowed_dirs:                # restrict to specific directories
    - "/home/user"
    - "/var/log"
```

> ⚠️ **Never enable shell without `blocked_commands` in production.**

## Telegram Security

Always set `allowed_ids` to restrict who can message your bot:

```yaml
telegram:
  token: "..."
  allowed_ids:
    - 123456789                # your Telegram user ID
    - 987654321                # another trusted user
```

Get your ID: message [@userinfobot](https://t.me/userinfobot) on Telegram.

## WhatsApp Security

Restrict who can message your WhatsApp bot using phone number whitelist:

```yaml
whatsapp:
  bridge_url: "http://localhost:3000"
  webhook_secret: "random-secret-string"
  allowed_ids:
    - "905xxxxxxxxxx"            # your phone number
    - "901234567890"             # another trusted user
```

The bridge resolves each sender's phone number via `getContact()` and matches it against the allow list. Messages from unlisted numbers are silently ignored.

> ⚠️ **Always set `allowed_ids` in production** — without it, anyone who messages the bot will get a response.

## Satellite Security

```yaml
satellite:
  enabled: true
  auth_tokens:
    - "unique-token-per-device"
  tls_cert: "/etc/steward/certs/server.crt"
  tls_key: "/etc/steward/certs/server.key"
```

**Best practices:**
- Use **unique tokens per device** — revoke individually
- Enable **TLS** for encrypted communication
- Use `wss://` instead of `ws://` for satellite connections

## Admin Panel Security

```yaml
admin:
  listen_addr: "127.0.0.1:8080"  # bind to localhost
  username: "admin"
  password: "strong-random-password"
```

For remote access, use a reverse proxy with TLS (see [Admin Panel](Admin-Panel.md)).

## API Key Protection

- Store API keys in **environment variables**, not config files:
  ```bash
  # /etc/steward/env
  STEWARD_API_KEY=sk-ant-...
  TELEGRAM_TOKEN=123456:ABC...
  ```
- Config file permissions: `chmod 600 /etc/steward/core.yml`
- Never commit `core.yml` to git (already in `.gitignore`)

## systemd Hardening

The included service file applies:

```ini
NoNewPrivileges=yes        # prevent privilege escalation
ProtectSystem=strict       # mount / read-only
ProtectHome=yes            # hide /home
PrivateTmp=yes             # isolated /tmp
ReadWritePaths=/var/lib/steward  # only writable path
```

## Network Recommendations

```
Internet
   │
   ├── Port 443 (nginx) → Admin Panel (:8080)
   ├── Port 8765 → WhatsApp webhook
   └── Port 9090 → Satellite WebSocket (TLS)
```

- **Firewall:** Only expose necessary ports
- **VPN:** Consider running satellites over WireGuard/Tailscale
- **TLS everywhere:** Use Let's Encrypt or self-signed certs
