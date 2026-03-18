// Package admin provides an embedded web admin panel for Steward.
// The panel is served directly from the Go binary using embed.FS —
// no external files or dependencies needed.
package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed static/*
var staticFiles embed.FS

// Config holds admin panel configuration.
type Config struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"` // e.g., "0.0.0.0:8080"
	Username   string `yaml:"username"`    // basic auth
	Password   string `yaml:"password"`    // basic auth
	BridgeURL  string `yaml:"bridge_url"`  // WhatsApp bridge URL (e.g., http://127.0.0.1:3000)
}

// StatusProvider gives the admin panel access to runtime state.
type StatusProvider struct {
	mu             sync.RWMutex
	Version        string
	Uptime         time.Time
	Provider       string
	Model          string
	MemoryBackend  string
	Channel        string
	ToolCount      int
	Integrations   []string
	VoiceSTT       string
	VoiceTTS       string
	SatelliteCount int
}

// Server runs the admin web panel.
type Server struct {
	cfg             Config
	configPath      string
	integrationsDir string
	status          *StatusProvider
}

// NewServer creates an admin panel server.
func NewServer(cfg Config, configPath, integrationsDir string, status *StatusProvider) *Server {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:8080"
	}
	return &Server{
		cfg:             cfg,
		configPath:      configPath,
		integrationsDir: integrationsDir,
		status:          status,
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
	mux.HandleFunc("/api/logs", s.requireAuth(s.handleLogs))
	mux.HandleFunc("/api/whatsapp/", s.requireAuth(s.handleBridgeProxy))

	// Static files (embedded)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("creating static FS: %w", err)
	}
	mux.Handle("/", s.requireAuthHandler(http.FileServer(http.FS(staticFS))))

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
		"voice_stt":       s.status.VoiceSTT,
		"voice_tts":       s.status.VoiceTTS,
		"satellite_count": s.status.SatelliteCount,
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

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate YAML
	var test map[string]any
	if err := yaml.Unmarshal([]byte(req.Content), &test); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Invalid YAML: %s", err)})
		return
	}

	// Backup current config
	backup := s.configPath + ".bak"
	if data, err := os.ReadFile(s.configPath); err == nil {
		os.WriteFile(backup, data, 0o600)
	}

	// Write new config
	if err := os.WriteFile(s.configPath, []byte(req.Content), 0o600); err != nil {
		http.Error(w, fmt.Sprintf("writing config: %s", err), http.StatusInternalServerError)
		return
	}

	slog.Info("config updated via admin panel")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "saved",
		"message": "Configuration saved. Restart Steward to apply changes.",
		"backup":  backup,
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Use 'journalctl -u steward -n 100' on the server to view logs",
	})
}

// handleBridgeProxy forwards requests to the WhatsApp bridge.
func (s *Server) handleBridgeProxy(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BridgeURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"error": "bridge_url not configured in admin config",
		})
		return
	}

	// Strip /api/whatsapp/ prefix → forward to bridge
	bridgePath := strings.TrimPrefix(r.URL.Path, "/api/whatsapp")
	if bridgePath == "" {
		bridgePath = "/"
	}
	targetURL := strings.TrimRight(s.cfg.BridgeURL, "/") + bridgePath

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
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}

	var integrations []integrationInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		intName := strings.TrimSuffix(e.Name(), ".yml")

		// Read to check if enabled
		data, _ := os.ReadFile(filepath.Join(s.integrationsDir, e.Name()))
		var cfg map[string]any
		yaml.Unmarshal(data, &cfg)
		enabled, _ := cfg["enabled"].(bool)

		integrations = append(integrations, integrationInfo{
			Name:    intName,
			Enabled: enabled,
		})
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
