// Package config handles loading and validating the Steward configuration
// from YAML files and environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brooqs/steward/internal/voice"
	"gopkg.in/yaml.v3"
)

// Config holds the full Steward configuration.
type Config struct {
	// Provider settings
	Provider string `yaml:"provider"` // claude | openai | groq | gemini | ollama | openrouter
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"` // custom endpoint for ollama/openrouter
	MaxTokens int   `yaml:"max_tokens"`

	// Tool Router (local sub-agent for tool calling)
	ToolRouter ToolRouterConfig `yaml:"tool_router"`

	// System prompt
	SystemPrompt string `yaml:"system_prompt"`

	// Memory settings
	Memory MemoryConfig `yaml:"memory"`

	// Shell tool settings
	Shell ShellConfig `yaml:"shell"`

	// Voice settings
	Voice voice.Config `yaml:"voice"`

	// Satellite settings
	Satellite SatelliteConfig `yaml:"satellite"`

	// Admin panel settings
	Admin AdminConfig `yaml:"admin"`

	// Channel settings
	Telegram TelegramConfig `yaml:"telegram"`
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`

	// AI Policies
	Policies []string `yaml:"policies"`

	// Paths
	IntegrationsDir string `yaml:"integrations_dir"`
}

// ToolRouterConfig configures the local FunctionGemma sub-agent for tool calling.
type ToolRouterConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Model     string `yaml:"model"`      // GGUF model filename
	ModelsDir string `yaml:"models_dir"` // path to models directory
}

// MemoryConfig configures the memory subsystem.
type MemoryConfig struct {
	Backend        string         `yaml:"backend"`          // badger | postgres
	DataDir        string         `yaml:"data_dir"`         // badger data directory
	PostgresURL    string         `yaml:"postgres_url"`     // postgres connection string
	ShortTermLimit int            `yaml:"short_term_limit"` // recent messages to keep in context
	Embedding      EmbeddingConfig `yaml:"embedding"`
}

// EmbeddingConfig configures the embedding provider for long-term memory.
type EmbeddingConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // local | openai | ollama
	Model    string `yaml:"model"`    // ONNX model name or API model ID
	APIKey   string `yaml:"api_key"`  // for openai embeddings
	BaseURL  string `yaml:"base_url"` // for ollama embeddings
}

// ShellConfig configures the shell execution tool.
type ShellConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Timeout         int      `yaml:"timeout"`          // seconds
	MaxOutputBytes  int      `yaml:"max_output_bytes"` // max stdout+stderr capture
	BlockedCommands []string `yaml:"blocked_commands"`
	AllowedDirs     []string `yaml:"allowed_dirs"`     // restrict execution to these dirs
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Token      string  `yaml:"token"`
	AllowedIDs []int64 `yaml:"allowed_ids"` // restrict to specific user/chat IDs (security)
}

// WhatsAppConfig holds WhatsApp bridge settings.
type WhatsAppConfig struct {
	ListenAddr    string   `yaml:"listen_addr"`
	BridgeURL     string   `yaml:"bridge_url"`
	WebhookSecret string   `yaml:"webhook_secret"`
	AllowedIDs    []string `yaml:"allowed_ids"` // phone numbers (e.g. 905xxxxxxxxxx)
}

// SatelliteConfig holds satellite server settings.
type SatelliteConfig struct {
	Enabled    bool     `yaml:"enabled"`
	ListenAddr string   `yaml:"listen_addr"` // WebSocket listen address
	AuthTokens []string `yaml:"auth_tokens"` // allowed satellite tokens
	TLSCert    string   `yaml:"tls_cert"`
	TLSKey     string   `yaml:"tls_key"`
}

// AdminConfig holds admin panel settings.
type AdminConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"` // default: 0.0.0.0:8080
	Username   string `yaml:"username"`    // basic auth username
	Password   string `yaml:"password"`    // basic auth password
	BridgeURL  string `yaml:"bridge_url"`  // WhatsApp bridge URL
}

// DefaultConfig returns a Config with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider:  "claude",
		Model:     "claude-sonnet-4-5",
		MaxTokens: 4096,
		SystemPrompt: "You are Steward, a helpful AI personal assistant. " +
			"Be concise, accurate, and friendly. When using tools, explain what you did.",
		Memory: MemoryConfig{
			Backend:        "badger",
			DataDir:        "data/badger",
			ShortTermLimit: 10,
			Embedding: EmbeddingConfig{
				Enabled:  false, // disabled by default until ONNX model is set up
				Provider: "local",
				Model:    "all-MiniLM-L6-v2",
			},
		},
		Shell: ShellConfig{
			Enabled:        false, // disabled by default for security
			Timeout:        30,
			MaxOutputBytes: 65536,
			BlockedCommands: []string{
				"rm -rf /",
				"rm -rf /*",
				"mkfs",
				"dd",
				":(){ :|:& };:",
				"> /dev/sda",
				"chmod -R 777 /",
				"shutdown",
				"reboot",
				"init 0",
				"init 6",
			},
		},
		Telegram: TelegramConfig{},
		WhatsApp: WhatsAppConfig{
			ListenAddr: "0.0.0.0:8765",
			BridgeURL:  "http://localhost:3000",
		},
		IntegrationsDir: "config/integrations",
	}
}

// Load reads a YAML config file and merges it with defaults and
// environment variable overrides. Environment variables take precedence
// over the config file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — use defaults + env vars
			cfg.applyEnvOverrides()
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Resolve ${ENV_VAR} references before parsing
	data = resolveEnvVars(data)

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg.applyEnvOverrides()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	// Resolve relative paths against config directory
	configDir := filepath.Dir(path)
	cfg.resolvePaths(configDir)

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides.
// Env vars always take precedence over file values.
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("STEWARD_PROVIDER"); v != "" {
		c.Provider = v
	}
	if v := os.Getenv("STEWARD_API_KEY"); v != "" {
		c.APIKey = v
	}
	// Legacy env var support
	if c.APIKey == "" {
		if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
			c.APIKey = v
		}
	}
	if c.APIKey == "" {
		if v := os.Getenv("OPENAI_API_KEY"); v != "" {
			c.APIKey = v
		}
	}
	if v := os.Getenv("STEWARD_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("STEWARD_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("TELEGRAM_TOKEN"); v != "" {
		c.Telegram.Token = v
	}
}

// validate checks that required fields are present.
func (c *Config) validate() error {
	validProviders := map[string]bool{
		"claude": true, "openai": true, "groq": true,
		"gemini": true, "ollama": true, "openrouter": true,
		"llamacpp": true,
	}
	if !validProviders[strings.ToLower(c.Provider)] {
		return fmt.Errorf("unknown provider %q, must be one of: claude, openai, groq, gemini, ollama, openrouter, llamacpp", c.Provider)
	}

	// ollama and llamacpp don't require api_key
	if c.APIKey == "" && c.Provider != "ollama" && c.Provider != "llamacpp" {
		return fmt.Errorf("api_key is required for provider %q (set in config or STEWARD_API_KEY env)", c.Provider)
	}

	validBackends := map[string]bool{"badger": true, "postgres": true}
	if !validBackends[c.Memory.Backend] {
		return fmt.Errorf("unknown memory backend %q, must be: badger or postgres", c.Memory.Backend)
	}

	if c.Memory.Backend == "postgres" && c.Memory.PostgresURL == "" {
		return fmt.Errorf("postgres_url is required when memory backend is postgres")
	}

	if c.Shell.Timeout <= 0 {
		c.Shell.Timeout = 30
	}
	if c.Shell.MaxOutputBytes <= 0 {
		c.Shell.MaxOutputBytes = 65536
	}
	if c.Memory.ShortTermLimit <= 0 {
		c.Memory.ShortTermLimit = 10
	}

	return nil
}

// resolvePaths converts relative paths to absolute, anchored at configDir.
func (c *Config) resolvePaths(configDir string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(configDir, p)
	}
	c.Memory.DataDir = resolve(c.Memory.DataDir)
	c.IntegrationsDir = resolve(c.IntegrationsDir)
}

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveEnvVars replaces ${ENV_VAR} references in raw config bytes
// with their corresponding environment variable values.
// If a referenced env var is not set, the placeholder is kept as-is.
func resolveEnvVars(data []byte) []byte {
	return envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		// Extract var name from ${VAR_NAME}
		varName := string(match[2 : len(match)-1])
		if val, ok := os.LookupEnv(varName); ok {
			return []byte(val)
		}
		return match // keep original if env var not set
	})
}

// ResolveEnvVars is the exported version for use by integration loader.
func ResolveEnvVars(data []byte) []byte {
	return resolveEnvVars(data)
}
