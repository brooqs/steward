package embedding

import (
	"fmt"
	"strings"

	"github.com/brooqs/steward/internal/config"
)

// New creates an Embedder from configuration.
func New(cfg config.EmbeddingConfig) (Embedder, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	switch strings.ToLower(cfg.Provider) {
	case "local", "onnx", "llamacpp":
		modelsDir := cfg.Model
		if modelsDir == "" {
			modelsDir = "/var/lib/steward/models"
		}
		return NewLlamaCppEmbedder(modelsDir)

	case "huggingface", "hf":
		return NewHuggingFaceEmbedder(cfg.APIKey, cfg.Model), nil

	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openai embedder requires api_key")
		}
		return NewOpenAIEmbedder(cfg.APIKey, "", cfg.Model), nil

	case "ollama":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		model := cfg.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		return NewOpenAIEmbedder("", baseURL, model), nil

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Provider)
	}
}
