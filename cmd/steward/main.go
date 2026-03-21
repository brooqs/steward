// Steward — AI Personal Assistant
//
// Usage:
//
//	steward --config config/core.yml --channel telegram
//	steward --channel whatsapp
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brooqs/steward/internal/admin"
	"github.com/brooqs/steward/internal/channel/telegram"
	"github.com/brooqs/steward/internal/channel/whatsapp"
	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/core"
	"github.com/brooqs/steward/internal/embedding"
	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/knowledge"
	"github.com/brooqs/steward/internal/memory"
	"github.com/brooqs/steward/internal/provider"
	"github.com/brooqs/steward/internal/satellite"
	"github.com/brooqs/steward/internal/scheduler"
	"github.com/brooqs/steward/internal/tools"
	"github.com/brooqs/steward/internal/tools/browse"
	"github.com/brooqs/steward/internal/tools/shell"
	"github.com/brooqs/steward/internal/voice"

	// Import integrations so their init() functions register them.
	_ "github.com/brooqs/steward/internal/integration/google"
	_ "github.com/brooqs/steward/internal/integration/homeassistant"
	_ "github.com/brooqs/steward/internal/integration/jellyfin"
	_ "github.com/brooqs/steward/internal/integration/qbittorrent"
	_ "github.com/brooqs/steward/internal/integration/spotify"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	// CLI flags
	configPath := flag.String("config", "config/core.yml", "path to configuration file")
	channel := flag.String("channel", "telegram", "channel to use: telegram | whatsapp")
	logLevel := flag.String("log-level", "info", "log level: debug | info | warn | error")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("steward %s (%s)\n", version, commit)
		os.Exit(0)
	}

	// Setup structured logging
	setupLogging(*logLevel)

	slog.Info("starting steward",
		"version", version,
		"config", *configPath,
		"channel", *channel,
	)

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		// If config doesn't exist, create a minimal one and enter setup
		if os.IsNotExist(err) {
			cfg = &config.Config{}
		} else {
			slog.Error("failed to load config", "error", err)
			os.Exit(1)
		}
	}

	// Setup mode: if no API key, run only the admin panel with onboarding wizard
	// (llamacpp provider doesn't need an API key — it runs locally)
	if cfg.APIKey == "" && cfg.Provider != "llamacpp" {
		slog.Info("no API key configured — entering setup mode", "config", *configPath)

		setupCtx, setupCancel := context.WithCancel(context.Background())
		defer setupCancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			setupCancel()
		}()

		adminCfg := admin.Config{
			Enabled:    true,
			ListenAddr: "0.0.0.0:8080",
			SetupMode:  true,
		}
		adminServer := admin.NewServer(adminCfg, *configPath, "", nil, nil)
		slog.Info("🚀 setup wizard available at http://0.0.0.0:8080")
		if err := adminServer.Run(setupCtx); err != nil && err != http.ErrServerClosed {
			slog.Error("setup server error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Create LLM provider
	llm, err := provider.New(cfg.Provider, cfg.APIKey, cfg.Model, cfg.BaseURL)
	if err != nil {
		slog.Error("failed to create provider", "error", err)
		os.Exit(1)
	}
	slog.Info("provider ready", "name", llm.Name(), "model", cfg.Model)

	// Create memory store
	store, err := createMemoryStore(cfg)
	if err != nil {
		slog.Error("failed to create memory store", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("memory store ready", "backend", cfg.Memory.Backend)

	// Create tool registry
	registry := tools.NewRegistry()

	// Register shell tool if enabled
	if cfg.Shell.Enabled {
		sh := shell.New(cfg.Shell)
		registry.RegisterAll(sh.GetTools())
		slog.Info("shell tool enabled",
			"timeout", cfg.Shell.Timeout,
			"blocked_commands", len(cfg.Shell.BlockedCommands),
		)
	} else {
		slog.Info("shell tool disabled (enable in config)")
	}

	// Register web browsing tools (always enabled)
	wb := browse.New()
	registry.RegisterAll(wb.GetTools())
	slog.Info("web browsing tools enabled", "tools", 2)

	// Initialize voice engine (STT/TTS)
	var voiceEngine *voice.Engine
	if cfg.Voice.STT.Provider != "" || cfg.Voice.TTS.Provider != "" {
		ve, err := voice.NewEngine(cfg.Voice)
		if err != nil {
			slog.Warn("voice engine init failed", "error", err)
		} else {
			voiceEngine = ve
			slog.Info("voice engine ready", "stt", ve.HasSTT(), "tts", ve.HasTTS())
		}
	}

	// Create embedder for dynamic tool selection (optional)
	var embedder embedding.Embedder
	if cfg.Memory.Embedding.Enabled {
		emb, err := embedding.New(cfg.Memory.Embedding)
		if err != nil {
			slog.Warn("embedder init failed, using all tools", "error", err)
		} else if emb != nil {
			embedder = emb
			slog.Info("embedder ready for tool selection", "provider", emb.Name(), "dimensions", emb.Dimensions())
		}
	}

	// Create tool selector
	toolSelector := tools.NewToolSelector(registry, embedder, 10)

	// Create knowledge store (reuse BadgerDB for tool result caching)
	var kb *knowledge.Store
	if embedder != nil {
		if bs, ok := store.(*memory.BadgerStore); ok {
			kb = knowledge.NewStore(bs.DB(), embedder)
			slog.Info("knowledge store ready", "entries", kb.Count())
		}
	}

	// Create tool router (local sub-agent for tool calling)
	var toolRouter provider.Provider
	if cfg.ToolRouter.Enabled {
		modelsDir := cfg.ToolRouter.ModelsDir
		if modelsDir == "" {
			modelsDir = "/var/lib/steward/models"
		}
		tr, err := provider.NewLlamaCpp(modelsDir, cfg.ToolRouter.Model)
		if err != nil {
			slog.Warn("tool router disabled", "error", err)
		} else {
			toolRouter = tr
			slog.Info("tool router ready", "model", cfg.ToolRouter.Model)
		}
	}

	// Create the agent
	steward := core.New(core.Config{
		Provider:     llm,
		ToolRouter:   toolRouter,
		Registry:     registry,
		ToolSelector: toolSelector,
		Knowledge:    kb,
		Memory:       store,
		Model:        cfg.Model,
		MaxTokens:    cfg.MaxTokens,
		SystemPrompt: cfg.SystemPrompt,
		Policies:     cfg.Policies,
	})

	// Load integrations
	loader := integration.NewLoader(cfg.IntegrationsDir, registry)
	if err := loader.LoadAll(); err != nil {
		slog.Warn("integration loading had errors", "error", err)
	}

	// Start watching for integration config changes (hot-reload)
	if err := loader.Watch(); err != nil {
		slog.Warn("integration hot-reload disabled", "error", err)
	} else {
		defer loader.Stop()
	}

	slog.Info("ready",
		"tools", registry.Count(),
		"integrations", loader.ActiveIntegrations(),
	)

	// Index tool embeddings for dynamic selection (after all tools registered)
	if embedder != nil {
		if err := toolSelector.IndexTools(context.Background()); err != nil {
			slog.Warn("tool indexing failed, using all tools", "error", err)
		}
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Start satellite server if enabled
	if cfg.Satellite.Enabled {
		satCfg := satellite.ServerConfig{
			ListenAddr: cfg.Satellite.ListenAddr,
			AuthTokens: cfg.Satellite.AuthTokens,
			TLSCert:    cfg.Satellite.TLSCert,
			TLSKey:     cfg.Satellite.TLSKey,
		}
		if satCfg.ListenAddr == "" {
			satCfg.ListenAddr = "0.0.0.0:9090"
		}
		satServer := satellite.NewServer(satCfg, steward, voiceEngine)
		registry.RegisterAll(satServer.GetTools())
		go func() {
			if err := satServer.Run(ctx); err != nil {
				slog.Error("satellite server error", "error", err)
			}
		}()
		slog.Info("satellite server enabled",
			"addr", satCfg.ListenAddr,
			"tokens", len(satCfg.AuthTokens),
		)
	}

	// Start admin panel if enabled
	if cfg.Admin.Enabled {
		adminStatus := &admin.StatusProvider{
			Version:          version,
			Uptime:           time.Now(),
			Provider:         cfg.Provider,
			Model:            cfg.Model,
			MemoryBackend:    cfg.Memory.Backend,
			Channel:          *channel,
			ToolCount:        registry.Count(),
			Integrations:     loader.ActiveIntegrations(),
			VoiceSTT:         cfg.Voice.STT.Provider,
			VoiceTTS:         cfg.Voice.TTS.Provider,
			SatelliteEnabled: cfg.Satellite.Enabled,
		}
		adminCfg := admin.Config{
			Enabled:    true,
			ListenAddr: cfg.Admin.ListenAddr,
			Username:   cfg.Admin.Username,
			Password:   cfg.Admin.Password,
			BridgeURL:  cfg.Admin.BridgeURL,
		}
		adminServer := admin.NewServer(adminCfg, *configPath, cfg.IntegrationsDir, adminStatus, nil)
		go func() {
			if err := adminServer.Run(ctx); err != nil && err != http.ErrServerClosed {
				slog.Error("admin panel error", "error", err)
			}
		}()
		slog.Info("admin panel enabled", "addr", cfg.Admin.ListenAddr)
	}

	// Run the selected channel
	switch *channel {
	case "telegram":
		ch, err := telegram.New(steward, cfg.Telegram)
		if err != nil {
			slog.Error("failed to create telegram channel", "error", err)
			os.Exit(1)
		}
		if err := ch.Run(ctx); err != nil {
			slog.Error("telegram channel error", "error", err)
			os.Exit(1)
		}

	case "whatsapp":
		ch, err := whatsapp.New(steward, cfg.WhatsApp, voiceEngine)
		if err != nil {
			slog.Error("failed to create whatsapp channel", "error", err)
			os.Exit(1)
		}

		// Start scheduler with WhatsApp as notification channel
		schedSavePath := filepath.Join(filepath.Dir(cfg.IntegrationsDir), "scheduler.json")
		sched := scheduler.New(scheduler.Config{
			SavePath: schedSavePath,
			ChatFn:   steward.Chat,
			NotifyFn: func(channel, message string) error {
				// Parse channel format: "whatsapp:905xxxxxxxxxx"
				parts := strings.SplitN(channel, ":", 2)
				if len(parts) != 2 || parts[0] != "whatsapp" {
					return fmt.Errorf("unsupported channel: %s", channel)
				}
				ch.SendReply(parts[1], message)
				return nil
			},
		})
		registry.RegisterAll(sched.GetTools())
		if err := sched.Start(); err != nil {
			slog.Warn("scheduler start error", "error", err)
		}
		defer sched.Stop()
		slog.Info("scheduler enabled", "save_path", schedSavePath)

		if err := ch.Run(ctx); err != nil {
			slog.Error("whatsapp channel error", "error", err)
			os.Exit(1)
		}

	default:
		slog.Error("unknown channel", "channel", *channel)
		os.Exit(1)
	}
}

func createMemoryStore(cfg *config.Config) (memory.Store, error) {
	switch cfg.Memory.Backend {
	case "badger":
		return memory.NewBadgerStore(cfg.Memory.DataDir, cfg.Memory.ShortTermLimit)
	case "postgres":
		return memory.NewPostgresStore(cfg.Memory.PostgresURL, cfg.Memory.ShortTermLimit)
	default:
		return nil, fmt.Errorf("unknown memory backend: %s", cfg.Memory.Backend)
	}
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
}
