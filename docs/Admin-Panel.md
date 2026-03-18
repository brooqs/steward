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
| POST | `/api/config/save` | Save config (body: `{"content": "..."}`) |

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
