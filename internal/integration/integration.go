// Package integration defines the integration interface and hot-reload loader.
package integration

import (
	"github.com/brooqs/steward/internal/tools"
)

// Integration is the interface for all Steward integrations
// (Home Assistant, Jellyfin, qBittorrent, etc.).
type Integration interface {
	// Name returns the integration identifier.
	Name() string
	// LoadConfig parses integration-specific config from a YAML map.
	LoadConfig(cfg map[string]any) error
	// GetTools returns the tools this integration provides.
	GetTools() []tools.ToolSpec
	// HealthCheck returns true if the remote service is reachable.
	HealthCheck() bool
	// Enabled returns whether this integration is active.
	Enabled() bool
	// ToolPrefix returns the prefix used for tool names (e.g., "ha_").
	ToolPrefix() string
}
