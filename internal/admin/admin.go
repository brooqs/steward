// Package admin provides an embedded web admin panel for Steward.
// The panel is served directly from the Go binary using embed.FS —
// no external files or dependencies needed.
package admin

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"runtime"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed dist/*
var staticFiles embed.FS

// Config holds admin panel configuration.
type Config struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"` // e.g., "0.0.0.0:8080"
	Username   string `yaml:"username"`    // basic auth
	Password   string `yaml:"password"`    // basic auth
	BridgeURL  string `yaml:"bridge_url"`  // WhatsApp bridge URL (e.g., http://127.0.0.1:3000)
	SetupMode  bool   // true when running first-time setup (no auth required)
}

// StatusProvider gives the admin panel access to runtime state.
type StatusProvider struct {
	mu               sync.RWMutex
	Version          string
	Uptime           time.Time
	Provider         string
	Model            string
	MemoryBackend    string
	Channel          string
	ToolCount        int
	Integrations     []string
	VoiceSTT         string
	VoiceTTS         string
	SatelliteEnabled bool
	SatelliteCount   int
}

// Server runs the admin web panel.
type Server struct {
	cfg             Config
	configPath      string
	integrationsDir string
	status          *StatusProvider
	scheduler       CronJobProvider
}

// CronJobProvider is used by the admin panel to list/delete cron jobs.
type CronJobProvider interface {
	ListJobs() []CronJobInfo
	RemoveJob(id string) error
}

// CronJobInfo holds cron job data for the admin API.
type CronJobInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Prompt    string `json:"prompt"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// NewServer creates an admin panel server.
func NewServer(cfg Config, configPath, integrationsDir string, status *StatusProvider, sched CronJobProvider) *Server {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:8080"
	}
	return &Server{
		cfg:             cfg,
		configPath:      configPath,
		integrationsDir: integrationsDir,
		status:          status,
		scheduler:       sched,
	}
}

// Run starts the admin HTTP server.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	mux.HandleFunc("/api/config", s.requireAuth(s.handleConfig))
	mux.HandleFunc("/api/config/save", s.requireAuth(s.handleConfigSave))
	mux.HandleFunc("/api/integrations", s.requireAuth(s.handleIntegrations))
	mux.HandleFunc("/api/integrations/save", s.requireAuth(s.handleIntegrationSave))
	mux.HandleFunc("/api/integrations/templates", s.requireAuth(s.handleIntegrationTemplates))
	mux.HandleFunc("/api/spotify/authorize", s.requireAuth(s.handleSpotifyAuth))
	mux.HandleFunc("/api/spotify/exchange", s.requireAuth(s.handleSpotifyExchange))
	mux.HandleFunc("/api/gmail/authorize", s.requireAuth(s.handleGmailAuth))
	mux.HandleFunc("/api/gmail/exchange", s.requireAuth(s.handleGmailExchange))
	mux.HandleFunc("/api/logs", s.requireAuth(s.handleLogs))
	mux.HandleFunc("/api/policies", s.requireAuth(s.handlePolicies))
	mux.HandleFunc("/api/policies/save", s.requireAuth(s.handlePoliciesSave))
	mux.HandleFunc("/api/cron/jobs", s.requireAuth(s.handleCronJobs))
	mux.HandleFunc("/api/cron/delete", s.requireAuth(s.handleCronDelete))
	mux.HandleFunc("/api/restart", s.requireAuth(s.handleRestart))
	mux.HandleFunc("/api/embedding/status", s.requireAuth(s.handleEmbeddingStatus))
	mux.HandleFunc("/api/embedding/setup", s.requireAuth(s.handleEmbeddingSetup))
	mux.HandleFunc("/api/whatsapp/bridge/service", s.requireAuth(s.handleBridgeService))
	mux.HandleFunc("/api/whatsapp/", s.requireAuth(s.handleBridgeProxy))

	// Setup endpoint (no auth in setup mode)
	if s.cfg.SetupMode {
		mux.HandleFunc("/api/setup", s.handleSetup)
		mux.HandleFunc("/api/ollama/status", s.handleOllamaStatus)
		mux.HandleFunc("/api/ollama/install", s.handleOllamaInstall)
		mux.HandleFunc("/api/ollama/pull", s.handleOllamaPull)
	} else {
		mux.HandleFunc("/api/ollama/status", s.requireAuth(s.handleOllamaStatus))
		mux.HandleFunc("/api/ollama/install", s.requireAuth(s.handleOllamaInstall))
		mux.HandleFunc("/api/ollama/pull", s.requireAuth(s.handleOllamaPull))
	}

	// Serve Preact SPA (embedded)
	distFS, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		return fmt.Errorf("creating dist FS: %w", err)
	}
	fileServer := http.FileServer(http.FS(distFS))
	mux.Handle("/", s.requireAuthHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for non-file paths
		path := r.URL.Path
		if path != "/" && !strings.Contains(path, ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})))

	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("admin panel listening", "addr", s.cfg.ListenAddr)
	return srv.ListenAndServe()
}

func (s *Server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No auth in setup mode
		if s.cfg.SetupMode {
			handler(w, r)
			return
		}
		if s.cfg.Username != "" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != s.cfg.Username || pass != s.cfg.Password {
				w.Header().Set("WWW-Authenticate", `Basic realm="Steward Admin"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		handler(w, r)
	}
}

func (s *Server) requireAuthHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No auth in setup mode
		if s.cfg.SetupMode {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.Username != "" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != s.cfg.Username || pass != s.cfg.Password {
				w.Header().Set("WWW-Authenticate", `Basic realm="Steward Admin"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// In setup mode, return minimal status
	if s.cfg.SetupMode {
		json.NewEncoder(w).Encode(map[string]any{
			"setup_mode": true,
			"version":    "setup",
		})
		return
	}

	s.status.mu.RLock()
	defer s.status.mu.RUnlock()

	data := map[string]any{
		"version":         s.status.Version,
		"uptime_seconds":  time.Since(s.status.Uptime).Seconds(),
		"uptime_human":    time.Since(s.status.Uptime).Round(time.Second).String(),
		"provider":        s.status.Provider,
		"model":           s.status.Model,
		"memory_backend":  s.status.MemoryBackend,
		"channel":         s.status.Channel,
		"tool_count":      s.status.ToolCount,
		"integrations":    s.status.Integrations,
		"voice_stt":         s.status.VoiceSTT,
		"voice_tts":         s.status.VoiceTTS,
		"satellite_enabled": s.status.SatelliteEnabled,
		"satellite_count":   s.status.SatelliteCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("reading config: %s", err), http.StatusInternalServerError)
		return
	}

	// Parse to mask sensitive fields in the response
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		http.Error(w, "parsing config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"config": cfg,
		"raw":    string(data),
	})
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// Parse incoming JSON with only Settings-managed fields
	var incoming map[string]any
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Read existing config as raw YAML map (preserves all fields)
	existing := make(map[string]any)
	if data, err := os.ReadFile(s.configPath); err == nil {
		yaml.Unmarshal(data, &existing)
	}

	// Only merge Settings-managed keys — never touch whatsapp, admin, telegram, etc.
	settingsKeys := []string{
		"provider", "api_key", "model", "base_url", "max_tokens",
		"system_prompt", "memory", "shell",
		"telegram", "whatsapp",
	}
	for _, key := range settingsKeys {
		if val, ok := incoming[key]; ok {
			existing[key] = val
		}
	}

	// Backup current config
	backup := s.configPath + ".bak"
	if data, err := os.ReadFile(s.configPath); err == nil {
		os.WriteFile(backup, data, 0o600)
	}

	// Write merged config
	yamlData, err := yaml.Marshal(existing)
	if err != nil {
		http.Error(w, "failed to serialize config", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(s.configPath, yamlData, 0o600); err != nil {
		http.Error(w, fmt.Sprintf("writing config: %s", err), http.StatusInternalServerError)
		return
	}

	slog.Info("config updated via admin panel", "keys_updated", len(settingsKeys))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "saved",
		"message": "Settings saved. Restart Steward to apply changes.",
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Use 'journalctl -u steward -n 100' on the server to view logs",
	})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	slog.Info("restart requested via admin panel")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})

	// Exit after response is sent — systemd will restart us
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var setup struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		Provider     string `json:"provider"`
		APIKey       string `json:"api_key"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&setup); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Ollama and llamacpp don't need API keys
	needsAPIKey := setup.Provider != "ollama" && setup.Provider != "llamacpp"
	if setup.Username == "" || setup.Password == "" || setup.Provider == "" || (needsAPIKey && setup.APIKey == "") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "username, password, provider and api_key are required"})
		return
	}

	if setup.Model == "" {
		// Set sensible defaults
		switch setup.Provider {
		case "claude":
			setup.Model = "claude-sonnet-4-5-20241022"
		case "openai":
			setup.Model = "gpt-4o"
		case "groq":
			setup.Model = "llama-3.3-70b-versatile"
		case "gemini":
			setup.Model = "gemini-2.0-flash"
		case "ollama":
			setup.Model = "llama3.2"
		case "openrouter":
			setup.Model = "anthropic/claude-sonnet-4-5"
		}
	}

	if setup.SystemPrompt == "" {
		setup.SystemPrompt = "You are Steward, a helpful AI personal assistant.\nYou have access to smart home controls, media, downloads, and system management tools.\nBe concise, accurate, and friendly. When using tools, explain what you did."
	}

	// Detect OS-appropriate paths
	dataDir := "/var/lib/steward/badger"
	integrationsDir := filepath.Join(filepath.Dir(s.configPath), "integrations")
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "steward", "badger")
	}

	// Build config map
	cfg := map[string]any{
		"provider":      setup.Provider,
		"api_key":       setup.APIKey,
		"model":         setup.Model,
		"max_tokens":    4096,
		"system_prompt": setup.SystemPrompt,
		"admin": map[string]any{
			"enabled":     true,
			"listen_addr": "0.0.0.0:8080",
			"username":    setup.Username,
			"password":    setup.Password,
		},
		"memory": map[string]any{
			"backend":          "badger",
			"data_dir":         dataDir,
			"short_term_limit": 10,
		},
		"shell": map[string]any{
			"enabled":          false,
			"timeout":          30,
			"max_output_bytes": 65536,
			"blocked_commands": []string{"rm -rf /", "rm -rf /*", "mkfs", "dd", "shutdown", "reboot"},
		},
		"integrations_dir": integrationsDir,
	}

	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		http.Error(w, "failed to serialize config", http.StatusInternalServerError)
		return
	}

	// Ensure config directory exists
	configDir := filepath.Dir(s.configPath)
	os.MkdirAll(configDir, 0o755)

	if err := os.WriteFile(s.configPath, yamlData, 0o600); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to write config: " + err.Error()})
		return
	}

	// Create integrations directory
	os.MkdirAll(filepath.Join(configDir, "integrations"), 0o755)

	slog.Info("initial setup completed", "provider", setup.Provider, "config", s.configPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Setup complete! Steward is restarting..."})

	// Exit for systemd restart — will come back in normal mode
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

// handleBridgeProxy forwards requests to the WhatsApp bridge.
func (s *Server) handleBridgeProxy(w http.ResponseWriter, r *http.Request) {
	bridgeURL := s.cfg.BridgeURL

	// If not set in admin config, try reading from config file (whatsapp.bridge_url)
	if bridgeURL == "" {
		if data, err := os.ReadFile(s.configPath); err == nil {
			var cfg map[string]any
			if err := yaml.Unmarshal(data, &cfg); err == nil {
				if wa, ok := cfg["whatsapp"].(map[string]any); ok {
					if u, ok := wa["bridge_url"].(string); ok && u != "" {
						bridgeURL = u
					}
				}
			}
		}
	}

	if bridgeURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"error": "bridge_url not configured — set it in Channels page",
		})
		return
	}

	// Strip /api/whatsapp/ prefix → forward to bridge
	bridgePath := strings.TrimPrefix(r.URL.Path, "/api/whatsapp")
	if bridgePath == "" {
		bridgePath = "/"
	}
	targetURL := strings.TrimRight(bridgeURL, "/") + bridgePath

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "proxy error", http.StatusInternalServerError)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"error":  "bridge unreachable",
			"detail": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleBridgeService manages the WhatsApp bridge as a launchd service (macOS).
func (s *Server) handleBridgeService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.brooqs.steward-bridge.plist")

	if r.Method == "GET" {
		// Check if service is running
		running := false
		if _, err := os.Stat(plistPath); err == nil {
			out, _ := exec.Command("launchctl", "list", "com.brooqs.steward-bridge").Output()
			running = len(out) > 0
		}
		json.NewEncoder(w).Encode(map[string]any{
			"installed": fileExists(plistPath),
			"running":   running,
		})
		return
	}

	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action string `json:"action"` // start, stop
	}
	json.NewDecoder(r.Body).Decode(&req)

	switch req.Action {
	case "start":
		// Find node binary
		nodePath := findBinary("node", []string{"/opt/homebrew/bin/node", "/usr/local/bin/node"})
		if nodePath == "" {
			json.NewEncoder(w).Encode(map[string]any{"error": "Node.js not found — install with: brew install node"})
			return
		}

		// Find bridge index.js
		bridgePath := findBridgePath()
		if bridgePath == "" {
			json.NewEncoder(w).Encode(map[string]any{"error": "Bridge not found — reinstall with: brew reinstall steward"})
			return
		}

		// Create launchd plist
		logDir := "/opt/homebrew/var/log"
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			logDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "steward")
		}

		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.brooqs.steward-bridge</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s/steward-bridge.log</string>
  <key>StandardErrorPath</key>
  <string>%s/steward-bridge.err</string>
</dict>
</plist>`, nodePath, bridgePath, logDir, logDir)

		os.MkdirAll(filepath.Dir(plistPath), 0o755)
		if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
			json.NewEncoder(w).Encode(map[string]any{"error": "Failed to write plist: " + err.Error()})
			return
		}

		// Load the service
		exec.Command("launchctl", "unload", plistPath).Run() // unload first if exists
		if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
			json.NewEncoder(w).Encode(map[string]any{"error": "Failed to start service: " + err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{"message": "WhatsApp bridge service started!", "status": "running"})

	case "stop":
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			json.NewEncoder(w).Encode(map[string]any{"message": "Service not installed"})
			return
		}
		exec.Command("launchctl", "unload", plistPath).Run()
		os.Remove(plistPath)
		json.NewEncoder(w).Encode(map[string]any{"message": "WhatsApp bridge service stopped and removed"})

	default:
		json.NewEncoder(w).Encode(map[string]any{"error": "unknown action — use 'start' or 'stop'"})
	}
}

func findBinary(name string, candidates []string) string {
	// Try PATH first
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func findBridgePath() string {
	candidates := []string{}
	// Check brew cellar
	if out, err := exec.Command("/opt/homebrew/bin/brew", "--prefix", "steward").Output(); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(out))+"/libexec/bridge/whatsapp/index.js")
	}
	// Common install paths
	home := os.Getenv("HOME")
	candidates = append(candidates,
		"/opt/homebrew/Cellar/steward/*/libexec/bridge/whatsapp/index.js",
		filepath.Join(home, ".local", "share", "steward", "whatsapp-bridge", "index.js"),
	)
	for _, c := range candidates {
		if strings.Contains(c, "*") {
			matches, _ := filepath.Glob(c)
			if len(matches) > 0 {
				return matches[len(matches)-1]
			}
		} else if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// ── Ollama Management ──────────────────────────────────────

// handleOllamaStatus checks if Ollama is installed, running, and lists available models.
func (s *Server) handleOllamaStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	result := map[string]any{
		"installed": false,
		"running":   false,
		"models":    []string{},
	}

	// Check if ollama binary exists
	ollamaPath := findBinary("ollama", []string{"/opt/homebrew/bin/ollama", "/usr/local/bin/ollama"})
	if ollamaPath == "" {
		json.NewEncoder(w).Encode(result)
		return
	}
	result["installed"] = true

	// Check if Ollama API is responding
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		json.NewEncoder(w).Encode(result)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		result["running"] = true
		var tagsResp struct {
			Models []struct {
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err == nil {
			models := []map[string]any{}
			for _, m := range tagsResp.Models {
				models = append(models, map[string]any{
					"name": m.Name,
					"size": m.Size,
				})
			}
			result["models"] = models
		}
	}

	json.NewEncoder(w).Encode(result)
}

// handleOllamaInstall installs Ollama via brew (macOS) or guides user (Linux).
func (s *Server) handleOllamaInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	if runtime.GOOS == "darwin" {
		// Install via brew
		cmd := exec.Command("/opt/homebrew/bin/brew", "install", "ollama")
		out, err := cmd.CombinedOutput()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"error":  "Installation failed: " + err.Error(),
				"output": string(out),
			})
			return
		}
		// Start ollama service
		exec.Command("/opt/homebrew/bin/brew", "services", "start", "ollama").Run()
		// Wait a moment for it to start
		time.Sleep(2 * time.Second)

		json.NewEncoder(w).Encode(map[string]any{
			"message": "Ollama installed and started!",
			"status":  "ok",
		})
	} else {
		// Linux: use the official install script
		cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
		out, err := cmd.CombinedOutput()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"error":  "Installation failed: " + err.Error(),
				"output": string(out),
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"message": "Ollama installed!",
			"status":  "ok",
		})
	}
}

// handleOllamaPull downloads/pulls a model.
func (s *Server) handleOllamaPull(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		json.NewEncoder(w).Encode(map[string]any{"error": "model is required"})
		return
	}

	// Pull model via Ollama API
	client := &http.Client{Timeout: 30 * time.Minute}
	pullBody, _ := json.Marshal(map[string]any{
		"name":   req.Model,
		"stream": false,
	})
	resp, err := client.Post("http://localhost:11434/api/pull", "application/json",
		strings.NewReader(string(pullBody)))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"error": "Ollama not reachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		json.NewEncoder(w).Encode(map[string]any{
			"message": req.Model + " downloaded successfully!",
			"status":  "ok",
		})
	} else {
		body, _ := io.ReadAll(resp.Body)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "Pull failed: " + string(body),
		})
	}
}

func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	// GET ?name=xxx → return specific integration config
	// GET → return list of all integration configs
	name := r.URL.Query().Get("name")

	if name != "" {
		// Read specific integration config
		filePath := filepath.Join(s.integrationsDir, name+".yml")
		data, err := os.ReadFile(filePath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"error": "not found", "name": name})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": name, "raw": string(data)})
		return
	}

	// List all integration configs
	entries, err := os.ReadDir(s.integrationsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}

	type integrationInfo struct {
		Name         string `json:"name"`
		Enabled      bool   `json:"enabled"`
		NeedsConnect bool   `json:"needs_connect,omitempty"`
	}

	var integrations []integrationInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yml.example") {
			continue
		}
		intName := strings.TrimSuffix(e.Name(), ".yml")

		// Read to check if enabled
		data, _ := os.ReadFile(filepath.Join(s.integrationsDir, e.Name()))
		var cfg map[string]any
		yaml.Unmarshal(data, &cfg)

		// If enabled key exists, use it; otherwise default to true
		// (loader treats configs without explicit enabled:false as enabled)
		enabled := true
		if v, ok := cfg["enabled"]; ok {
			enabled, _ = v.(bool)
		}

		info := integrationInfo{Name: intName, Enabled: enabled}

		// Spotify-specific: check if refresh_token is empty
		if intName == "spotify" {
			rt, _ := cfg["refresh_token"].(string)
			if rt == "" {
				info.NeedsConnect = true
			}
		}

		integrations = append(integrations, info)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"integrations": integrations})
}

func (s *Server) handleIntegrationSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.Name == "" || payload.Content == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name and content required"})
		return
	}

	// Validate YAML
	var check map[string]any
	if err := yaml.Unmarshal([]byte(payload.Content), &check); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid YAML: " + err.Error()})
		return
	}

	// Sanitize name — only allow alphanumeric, dash, underscore
	safeName := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, payload.Name)

	filePath := filepath.Join(s.integrationsDir, safeName+".yml")
	if err := os.WriteFile(filePath, []byte(payload.Content), 0o644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	slog.Info("integration config saved", "name", safeName)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Saved! Integration will hot-reload."})
}

func (s *Server) handleIntegrationTemplates(w http.ResponseWriter, r *http.Request) {
	// Look for .yml.example files in the integrations dir AND the source config dir
	// Also check a "templates" subdirectory
	searchDirs := []string{s.integrationsDir}

	type templateInfo struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}

	var templates []templateInfo
	seen := make(map[string]bool)

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml.example") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".yml.example")
			if seen[name] {
				continue
			}
			seen[name] = true

			// Skip if already configured (non-example .yml exists)
			if _, err := os.Stat(filepath.Join(s.integrationsDir, name+".yml")); err == nil {
				continue
			}

			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			templates = append(templates, templateInfo{Name: name, Content: string(data)})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"templates": templates})
}

// ── Spotify OAuth2 ────────────────────────────────────────────

const (
	spotifyScopes      = "user-read-playback-state user-modify-playback-state user-read-currently-playing"
	spotifyRedirectURI = "http://127.0.0.1:8888/callback"
)

func (s *Server) readSpotifyConfig() (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(s.integrationsDir, "spotify.yml"))
	if err != nil {
		return nil, fmt.Errorf("spotify.yml not found — create it first via Add Integration")
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *Server) handleSpotifyAuth(w http.ResponseWriter, r *http.Request) {
	var clientID string

	// Accept POST with client_id/client_secret from frontend
	if r.Method == http.MethodPost {
		var payload struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.ClientID != "" {
			// Save to spotify.yml
			cfg := map[string]any{
				"enabled":       true,
				"client_id":     payload.ClientID,
				"client_secret": payload.ClientSecret,
			}
			yamlData, _ := yaml.Marshal(cfg)
			spotifyPath := filepath.Join(s.integrationsDir, "spotify.yml")
			os.WriteFile(spotifyPath, yamlData, 0o644)
			clientID = payload.ClientID
		}
	}

	// Fallback: read from spotify.yml
	if clientID == "" {
		cfg, err := s.readSpotifyConfig()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		clientID, _ = cfg["client_id"].(string)
	}

	if clientID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "client_id not set in spotify.yml"})
		return
	}

	authURL := fmt.Sprintf(
		"https://accounts.spotify.com/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=%s",
		clientID,
		url.QueryEscape(spotifyRedirectURI),
		url.QueryEscape(spotifyScopes),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"auth_url": authURL, "redirect_uri": spotifyRedirectURI})
}

func (s *Server) handleSpotifyExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		CallbackURL string `json:"callback_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid json"})
		return
	}

	// Parse the code from callback URL
	parsed, err := url.Parse(payload.CallbackURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL"})
		return
	}

	code := parsed.Query().Get("code")
	if code == "" {
		errMsg := parsed.Query().Get("error")
		if errMsg != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "Authorization denied: " + errMsg})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No authorization code found in URL"})
		return
	}

	cfg, err := s.readSpotifyConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	clientID, _ := cfg["client_id"].(string)
	clientSecret, _ := cfg["client_secret"].(string)
	if clientID == "" || clientSecret == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "client_id or client_secret missing"})
		return
	}

	// Exchange code for tokens
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {spotifyRedirectURI},
	}

	req, _ := http.NewRequest("POST", "https://accounts.spotify.com/api/token",
		strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "Token exchange failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Spotify error: %s", string(body))})
		return
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	json.Unmarshal(body, &tokenResp)

	if tokenResp.RefreshToken == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "No refresh_token received"})
		return
	}

	// Save refresh token to spotify.yml
	cfg["refresh_token"] = tokenResp.RefreshToken
	cfg["enabled"] = true

	yamlData, _ := yaml.Marshal(cfg)
	spotifyPath := filepath.Join(s.integrationsDir, "spotify.yml")
	if err := os.WriteFile(spotifyPath, yamlData, 0o644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save: " + err.Error()})
		return
	}

	slog.Info("spotify oauth2 completed", "refresh_token_length", len(tokenResp.RefreshToken))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Spotify connected! Integration will hot-reload."})
}

// ── Google OAuth2 (Gmail + Calendar + Drive) ──────────────────────

const (
	googleScopes      = "https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/calendar https://www.googleapis.com/auth/drive"
	googleRedirectURI = "http://127.0.0.1:8888/callback"
)

func (s *Server) readGoogleConfig() (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(s.integrationsDir, "google.yml"))
	if err != nil {
		return nil, fmt.Errorf("google.yml not found — create it first via Add Integration")
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *Server) handleGmailAuth(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.readGoogleConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	clientID, _ := cfg["client_id"].(string)
	if clientID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "client_id not set in google.yml"})
		return
	}

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&access_type=offline&prompt=consent",
		clientID,
		url.QueryEscape(googleRedirectURI),
		url.QueryEscape(googleScopes),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": authURL, "redirect_uri": googleRedirectURI})
}

func (s *Server) handleGmailExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		CallbackURL string `json:"callback_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid json"})
		return
	}

	parsed, err := url.Parse(payload.CallbackURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL"})
		return
	}

	code := parsed.Query().Get("code")
	if code == "" {
		errMsg := parsed.Query().Get("error")
		if errMsg != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "Authorization denied: " + errMsg})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No authorization code found in URL"})
		return
	}

	cfg, err := s.readGoogleConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	clientID, _ := cfg["client_id"].(string)
	clientSecret, _ := cfg["client_secret"].(string)
	if clientID == "" || clientSecret == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "client_id or client_secret missing"})
		return
	}

	data := url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {googleRedirectURI},
		"grant_type":    {"authorization_code"},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "Token exchange failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Google error: %s", string(body))})
		return
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	json.Unmarshal(body, &tokenResp)

	if tokenResp.RefreshToken == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "No refresh_token received — make sure prompt=consent is set"})
		return
	}

	cfg["refresh_token"] = tokenResp.RefreshToken
	cfg["enabled"] = true

	yamlData, _ := yaml.Marshal(cfg)
	googlePath := filepath.Join(s.integrationsDir, "google.yml")
	if err := os.WriteFile(googlePath, yamlData, 0o644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save: " + err.Error()})
		return
	}

	slog.Info("google oauth2 completed", "refresh_token_length", len(tokenResp.RefreshToken))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Google connected! Gmail, Calendar & Drive will hot-reload."})
}

// ── Policies Handlers ─────────────────────────────────────────

func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"policies": []string{}})
		return
	}

	var raw map[string]any
	yaml.Unmarshal(data, &raw)

	policies, _ := raw["policies"]
	if policies == nil {
		policies = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"policies": policies})
}

func (s *Server) handlePoliciesSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}

	var req struct {
		Policies []string `json:"policies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Read existing config
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		http.Error(w, "Failed to read config", 500)
		return
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		http.Error(w, "Failed to parse config", 500)
		return
	}

	raw["policies"] = req.Policies

	yamlData, _ := yaml.Marshal(raw)
	if err := os.WriteFile(s.configPath, yamlData, 0o644); err != nil {
		http.Error(w, "Failed to save", 500)
		return
	}

	slog.Info("policies updated", "count", len(req.Policies))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ── Cron Handlers ─────────────────────────────────────────────

func (s *Server) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.scheduler == nil {
		json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}, "count": 0})
		return
	}

	jobs := s.scheduler.ListJobs()
	json.NewEncoder(w).Encode(map[string]any{"jobs": jobs, "count": len(jobs)})
}

func (s *Server) handleCronDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}

	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Scheduler not available", 500)
		return
	}

	if err := s.scheduler.RemoveJob(req.JobID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
