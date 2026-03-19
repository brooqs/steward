package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Model files to download from HuggingFace
var modelFiles = []struct {
	Name string
	URL  string
	Size string
}{
	{"model.onnx", "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx", "~22MB"},
	{"tokenizer.json", "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json", "~700KB"},
}

var downloadMu sync.Mutex
var downloadProgress string

func (s *Server) handleEmbeddingStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	modelsDir := "/var/lib/steward/models"
	modelExists := fileExists(filepath.Join(modelsDir, "model.onnx"))
	tokenizerExists := fileExists(filepath.Join(modelsDir, "tokenizer.json"))

	// Check if embedding is enabled in config
	cfg, _ := s.readConfigFile()
	embeddingEnabled := false
	embeddingProvider := ""
	if cfgMap, ok := cfg.(map[string]any); ok {
		if mem, ok := cfgMap["memory"].(map[string]any); ok {
			if emb, ok := mem["embedding"].(map[string]any); ok {
				if enabled, ok := emb["enabled"].(bool); ok {
					embeddingEnabled = enabled
				}
				if prov, ok := emb["provider"].(string); ok {
					embeddingProvider = prov
				}
			}
		}
	}

	downloadMu.Lock()
	progress := downloadProgress
	downloadMu.Unlock()

	json.NewEncoder(w).Encode(map[string]any{
		"model_downloaded":   modelExists && tokenizerExists,
		"model_path":         modelsDir,
		"embedding_enabled":  embeddingEnabled,
		"embedding_provider": embeddingProvider,
		"downloading":        progress != "",
		"progress":           progress,
	})
}

func (s *Server) handleEmbeddingSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Action   string `json:"action"` // "download" | "enable" | "enable_hf"
		Provider string `json:"provider"`
	}
	json.NewDecoder(r.Body).Decode(&payload)

	switch payload.Action {
	case "download":
		go downloadModel()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "downloading", "message": "Model download started..."})

	case "enable_hf":
		// Enable HuggingFace Inference API (no download needed)
		if err := s.enableEmbedding("huggingface", "sentence-transformers/all-MiniLM-L6-v2"); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "HuggingFace embedding enabled! Restarting..."})
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()

	case "enable":
		provider := payload.Provider
		if provider == "" {
			provider = "huggingface"
		}
		model := "sentence-transformers/all-MiniLM-L6-v2"
		if provider == "ollama" {
			model = "nomic-embed-text"
		}
		if err := s.enableEmbedding(provider, model); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Embedding enabled! Restarting..."})
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()

	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (s *Server) enableEmbedding(provider, model string) error {
	// Read current config
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Update embedding section
	mem, ok := cfg["memory"].(map[string]any)
	if !ok {
		mem = map[string]any{}
		cfg["memory"] = mem
	}
	mem["embedding"] = map[string]any{
		"enabled":  true,
		"provider": provider,
		"model":    model,
	}

	// Write back
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}

	// Backup
	os.WriteFile(s.configPath+".bak", data, 0o600)

	if err := os.WriteFile(s.configPath, out, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	slog.Info("embedding enabled via admin", "provider", provider, "model", model)
	return nil
}

func downloadModel() {
	downloadMu.Lock()
	if downloadProgress != "" {
		downloadMu.Unlock()
		return // already downloading
	}
	downloadProgress = "starting"
	downloadMu.Unlock()

	defer func() {
		downloadMu.Lock()
		downloadProgress = ""
		downloadMu.Unlock()
	}()

	modelsDir := "/var/lib/steward/models"
	os.MkdirAll(modelsDir, 0o755)

	for i, mf := range modelFiles {
		downloadMu.Lock()
		downloadProgress = fmt.Sprintf("downloading %s (%d/%d)", mf.Name, i+1, len(modelFiles))
		downloadMu.Unlock()

		slog.Info("downloading model file", "name", mf.Name, "url", mf.URL)

		destPath := filepath.Join(modelsDir, mf.Name)
		if err := downloadFile(destPath, mf.URL); err != nil {
			slog.Error("model download failed", "file", mf.Name, "error", err)
			downloadMu.Lock()
			downloadProgress = "error: " + err.Error()
			downloadMu.Unlock()
			time.Sleep(5 * time.Second)
			return
		}
	}

	downloadMu.Lock()
	downloadProgress = "complete"
	downloadMu.Unlock()
	slog.Info("model download complete", "dir", modelsDir)
	time.Sleep(2 * time.Second)
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dest)
}

func (s *Server) readConfigFile() (any, error) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, err
	}
	var result any
	err = yaml.Unmarshal(data, &result)
	return result, err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
