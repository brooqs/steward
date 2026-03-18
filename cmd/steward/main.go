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
	"os"
	"os/signal"
	"syscall"

	"github.com/brooqs/steward/internal/channel/telegram"
	"github.com/brooqs/steward/internal/channel/whatsapp"
	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/core"
	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/memory"
	"github.com/brooqs/steward/internal/provider"
	"github.com/brooqs/steward/internal/satellite"
	"github.com/brooqs/steward/internal/tools"
	"github.com/brooqs/steward/internal/tools/shell"
	"github.com/brooqs/steward/internal/voice"

	// Import integrations so their init() functions register them.
	_ "github.com/brooqs/steward/internal/integration/homeassistant"
	_ "github.com/brooqs/steward/internal/integration/jellyfin"
	_ "github.com/brooqs/steward/internal/integration/qbittorrent"
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
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
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

	// Create the agent
	steward := core.New(core.Config{
		Provider:     llm,
		Registry:     registry,
		Memory:       store,
		Model:        cfg.Model,
		MaxTokens:    cfg.MaxTokens,
		SystemPrompt: cfg.SystemPrompt,
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
		ch, err := whatsapp.New(steward, cfg.WhatsApp)
		if err != nil {
			slog.Error("failed to create whatsapp channel", "error", err)
			os.Exit(1)
		}
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
