package integration

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/brooqs/steward/internal/tools"
)

// knownIntegrations maps config filename (without extension) to a constructor.
var knownIntegrations = map[string]func() Integration{}

// Register makes an integration available for hot-reload by config filename.
// Call this from each integration package's init() function.
func Register(configName string, constructor func() Integration) {
	knownIntegrations[configName] = constructor
}

// Loader watches the integrations config directory and dynamically
// loads/unloads integrations as YAML files are added, changed, or removed.
type Loader struct {
	mu       sync.RWMutex
	dir      string
	registry *tools.Registry
	active   map[string]Integration // configName → active integration
	watcher  *fsnotify.Watcher
	done     chan struct{}
}

// NewLoader creates a new integration loader.
func NewLoader(dir string, registry *tools.Registry) *Loader {
	return &Loader{
		dir:      dir,
		registry: registry,
		active:   make(map[string]Integration),
		done:     make(chan struct{}),
	}
}

// LoadAll scans the integrations directory and loads all valid configs.
func (l *Loader) LoadAll() error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("integrations directory does not exist, skipping", "dir", l.dir)
			return nil
		}
		return fmt.Errorf("reading integrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		if strings.HasSuffix(name, ".example") {
			continue
		}

		l.loadIntegration(name)
	}

	return nil
}

// Watch starts watching the directory for file changes.
// This enables hot-reload of integrations.
func (l *Loader) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	l.watcher = watcher

	// Ensure directory exists
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return fmt.Errorf("creating integrations dir: %w", err)
	}

	if err := watcher.Add(l.dir); err != nil {
		return fmt.Errorf("watching directory: %w", err)
	}

	go l.watchLoop()
	slog.Info("watching integrations directory", "dir", l.dir)
	return nil
}

// Stop stops watching for changes.
func (l *Loader) Stop() {
	if l.watcher != nil {
		close(l.done)
		l.watcher.Close()
	}
}

func (l *Loader) watchLoop() {
	for {
		select {
		case event, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			name := filepath.Base(event.Name)
			if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
				continue
			}
			if strings.HasSuffix(name, ".example") {
				continue
			}

			switch {
			case event.Op&(fsnotify.Create|fsnotify.Write) != 0:
				slog.Info("integration config changed", "file", name)
				l.loadIntegration(name)
			case event.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
				slog.Info("integration config removed", "file", name)
				l.unloadIntegration(name)
			}

		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("watcher error", "error", err)

		case <-l.done:
			return
		}
	}
}

// loadIntegration loads or reloads an integration from its YAML config file.
func (l *Loader) loadIntegration(filename string) {
	configName := strings.TrimSuffix(filename, filepath.Ext(filename))

	constructor, ok := knownIntegrations[configName]
	if !ok {
		slog.Debug("no integration registered for config", "name", configName)
		return
	}

	// Read the config file
	path := filepath.Join(l.dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error("reading integration config", "file", filename, "error", err)
		return
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Error("parsing integration config", "file", filename, "error", err)
		return
	}

	// Create and load the integration
	integ := constructor()
	if err := integ.LoadConfig(cfg); err != nil {
		slog.Error("loading integration", "name", configName, "error", err)
		return
	}

	if !integ.Enabled() {
		slog.Info("integration disabled by config", "name", configName)
		return
	}

	// Unload previous version if any
	l.unloadIntegration(filename)

	// Register tools
	integTools := integ.GetTools()
	l.registry.RegisterAll(integTools)

	l.mu.Lock()
	l.active[configName] = integ
	l.mu.Unlock()

	slog.Info("integration loaded",
		"name", configName,
		"tools", len(integTools),
		"healthy", integ.HealthCheck(),
	)
}

// unloadIntegration removes an integration and its tools.
func (l *Loader) unloadIntegration(filename string) {
	configName := strings.TrimSuffix(filename, filepath.Ext(filename))

	l.mu.Lock()
	integ, ok := l.active[configName]
	if ok {
		delete(l.active, configName)
	}
	l.mu.Unlock()

	if ok {
		l.registry.UnregisterPrefix(integ.ToolPrefix())
		slog.Info("integration unloaded", "name", configName)
	}
}

// ActiveIntegrations returns the names of all active integrations.
func (l *Loader) ActiveIntegrations() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.active))
	for name := range l.active {
		names = append(names, name)
	}
	return names
}
