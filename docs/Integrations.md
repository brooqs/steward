# Integrations

Steward supports hot-reloadable integrations. Drop a YAML config file into the integrations directory — no restart needed.

## Built-in Integrations

### Home Assistant

```yaml
# /etc/steward/integrations/homeassistant.yml
name: homeassistant
enabled: true
url: "http://homeassistant.local:8123"
token: "your-long-lived-access-token"
```

**Capabilities:** Control lights, switches, scenes. Query sensor values. Run automations.

### Jellyfin

```yaml
# /etc/steward/integrations/jellyfin.yml
name: jellyfin
enabled: true
url: "http://jellyfin.local:8096"
api_key: "your-api-key"
```

**Capabilities:** Search media library. Get recently added items. Play media.

### qBittorrent

```yaml
# /etc/steward/integrations/qbittorrent.yml
name: qbittorrent
enabled: true
url: "http://qbittorrent.local:8080"
username: "admin"
password: "adminadmin"
```

**Capabilities:** List/add/remove torrents. Check download progress. Manage categories.

## Hot-Reload

Integrations are automatically loaded when you add/modify/remove YAML files:

```bash
# Add a new integration (no restart needed!)
cp jellyfin.yml.example /etc/steward/integrations/jellyfin.yml
nano /etc/steward/integrations/jellyfin.yml

# steward log: "integration loaded: jellyfin"
```

## Writing Custom Integrations

### 1. Create the Go Package

```go
// internal/integration/myservice/myservice.go
package myservice

import (
    "github.com/brooqs/steward/internal/integration"
    "github.com/brooqs/steward/internal/tools"
)

func init() {
    integration.Register("myservice", factory)
}

func factory(cfg integration.Config) ([]tools.Tool, error) {
    url := cfg.Settings["url"]
    
    return []tools.Tool{
        {
            Name:        "myservice_status",
            Description: "Check MyService status",
            Parameters: map[string]tools.Parameter{},
            Execute: func(args map[string]any) (string, error) {
                // Your API call here
                return "MyService is running", nil
            },
        },
    }, nil
}
```

### 2. Register via Import

```go
// cmd/steward/main.go
import (
    _ "github.com/brooqs/steward/internal/integration/myservice"
)
```

### 3. Create Config

```yaml
# /etc/steward/integrations/myservice.yml
name: myservice
enabled: true
url: "http://myservice.local:8080"
api_key: "..."
```

### Integration Interface

```go
type Config struct {
    Name     string            `yaml:"name"`
    Enabled  bool              `yaml:"enabled"`
    Settings map[string]string `yaml:",inline"`
}

// Factory function signature:
type Factory func(cfg Config) ([]tools.Tool, error)
```

Each integration registers a factory function via `init()`. The factory receives the parsed YAML config and returns a list of tools that the LLM can use.
