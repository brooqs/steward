# Satellite System

The satellite system enables JARVIS-style remote interaction. Run the `steward-satellite` client on any machine and control it through the main Steward server.

## How It Works

```
┌─────────────────┐     WebSocket      ┌─────────────────┐
│   Your Laptop   │◄──────────────────►│  Steward Server  │
│ steward-satellite│    (TLS optional)  │    steward       │
│                  │                    │                  │
│  🎙️ Microphone   │  ── audio ──►     │  STT → LLM      │
│  🔊 Speaker      │  ◄── audio ──     │  TTS ← Response  │
│  💻 Shell        │  ◄── commands ──  │  Tool Dispatch   │
│  📊 System Info  │  ── reports ──►   │  Dashboard       │
└─────────────────┘                    └─────────────────┘
```

## Server Setup

Enable the satellite server in `core.yml`:

```yaml
satellite:
  enabled: true
  listen_addr: "0.0.0.0:9090"
  auth_tokens:
    - "my-secret-token-1"
    - "another-token-for-laptop"
  # tls_cert: "/etc/steward/certs/server.crt"
  # tls_key: "/etc/steward/certs/server.key"
```

Restart: `sudo systemctl restart steward`

## Client Setup

Install `steward-satellite` on the remote machine:

```bash
# From release (auto-included in the archive)
./steward-satellite --server ws://steward.local:9090/ws --token my-secret-token-1
```

### CLI Flags

```
steward-satellite [flags]

  --server string    WebSocket URL (default "ws://localhost:9090/ws")
  --token string     authentication token
  --log-level string debug | info | warn | error (default "info")
  --version          print version and exit
```

### Interactive Commands

Once connected, you get an interactive prompt:

```
🤖 Connected to Steward
> Hello, what can you do?
Steward: I can help with smart home control, media management...

> /sysinfo
📊 Reporting system info to server...

> /quit
👋 Goodbye!
```

## Capabilities

### Text Chat
Type messages and receive AI responses in the terminal.

### Voice (Audio)
When voice is configured on the server, satellite can stream audio:
- Audio is captured from the satellite's microphone
- Sent to server → STT → LLM → TTS
- Response audio played through satellite's speaker (mpv/ffplay/aplay)

### Remote Command Execution
The AI can execute shell commands on the satellite machine:

```
You: "Check the disk space on my laptop"
Steward: [executes 'df -h' on your satellite]
```

### System Info Reporting
The satellite reports: hostname, OS, architecture, CPU, memory, disk usage, and uptime.

## Security

- **Token authentication** — each satellite needs a valid token
- **TLS support** — encrypt the WebSocket connection
- **Command execution** — commands from server to satellite are logged
- **One token per device** — revoke access by removing the token

## Running as a Service

Create `/etc/systemd/system/steward-satellite.service`:

```ini
[Unit]
Description=Steward Satellite Client
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/steward-satellite --server wss://steward.example.com:9090/ws --token YOUR_TOKEN
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now steward-satellite
```

## LLM Tools

The AI can interact with satellites through built-in tools:

| Tool | Description |
|------|-------------|
| `satellite_list` | List all connected satellites |
| `satellite_exec` | Run a command on a satellite |
| `satellite_sysinfo` | Request system info from a satellite |
