# Admin Panel

Steward includes an embedded web admin panel for monitoring and configuration. It's compiled directly into the binary — no external files needed.

## Setup

```yaml
admin:
  enabled: true
  listen_addr: "0.0.0.0:8080"
  username: "admin"
  password: "your-strong-password"
```

Access at `http://your-server:8080` with the configured credentials.

## Dashboard

The dashboard shows real-time status:

- **Status** — running state and uptime
- **Provider** — active LLM provider and model
- **Tools** — number of registered tools
- **Channel** — active messaging channel
- **Services** — STT, TTS, memory, satellite status

Status auto-refreshes every 5 seconds.

## Channels Page

Manage WhatsApp and Telegram channel configuration:

- **WhatsApp Bridge Status** — shows connected/disconnected state, QR code for pairing, message count
- **WhatsApp Configuration** — listen address, bridge URL, webhook secret, allowed phone numbers
- **Telegram Configuration** — bot token, allowed user IDs

When the bridge is disconnected, a QR code is displayed for scanning with WhatsApp → Settings → Linked Devices → Link a Device.

## Restart Button

The 🔄 **Restart** button in the top-right corner restarts the Steward service:

1. Click the button → confirmation dialog appears
2. Confirm → service restarts (systemd automatically brings it back)
3. Page auto-reloads when the service is back online

Useful after config changes, which require a restart to take effect.

## Settings Editor

The settings page provides a YAML editor for `core.yml`:

- Syntax highlighting with monospace font
- **YAML validation** before saving — invalid YAML is rejected
- **Auto-backup** — creates `core.yml.bak` before each save
- Changes require service restart: `systemctl restart steward`

## API Endpoints

All endpoints require Basic Auth.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/status` | Runtime status (JSON) |
| GET | `/api/config` | Current config (JSON + raw YAML) |
| POST | `/api/config/save` | Save config changes |
| POST | `/api/restart` | Restart Steward service |
| GET | `/api/integrations` | List all integrations |
| POST | `/api/integrations/save` | Save integration config |
| GET | `/api/integrations/templates` | List available templates |
| GET | `/api/cron/jobs` | List cron jobs |
| POST | `/api/cron/delete` | Delete a cron job |
| GET | `/api/whatsapp/*` | Proxy to WhatsApp bridge |
| GET | `/api/logs` | Log viewing info |
| GET | `/api/spotify/authorize` | Start Spotify OAuth |
| POST | `/api/spotify/exchange` | Complete Spotify OAuth |
| GET | `/api/gmail/authorize` | Start Google OAuth |
| POST | `/api/gmail/exchange` | Complete Google OAuth |

### Example

```bash
# Get status
curl -u admin:password http://localhost:8080/api/status | jq .

# Save config
curl -u admin:password -X POST http://localhost:8080/api/config/save \
  -H "Content-Type: application/json" \
  -d '{"content": "provider: groq\napi_key: ..."}'
```

## Security

- **Basic Auth** — always set a strong password
- **Bind to localhost** if only accessing locally: `listen_addr: "127.0.0.1:8080"`
- **Reverse proxy** recommended for production (nginx/caddy with TLS)
- **Firewall** — restrict port access to trusted IPs

### Nginx Reverse Proxy

```nginx
server {
    listen 443 ssl;
    server_name steward.example.com;
    ssl_certificate /etc/ssl/certs/steward.crt;
    ssl_certificate_key /etc/ssl/private/steward.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```
