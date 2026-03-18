package embedding

import (
	"context"
	"fmt"
)

// ONNXEmbedder is a placeholder for local ONNX embedding.
//
// Direct ONNX Runtime integration requires platform-specific C bindings
// that prevent cross-compilation. Instead, use one of these alternatives
// for local embeddings:
//
//   - Ollama:   provider: "ollama"  (runs nomic-embed-text locally)
//   - OpenAI:   provider: "openai"  (API-based)
//
// To run embeddings locally with Ollama:
//
//	ollama pull nomic-embed-text
//	# then in core.yml:
//	embedding:
//	  provider: "ollama"
//	  model: "nomic-embed-text"
type ONNXEmbedder struct{}

var errONNXNotSupported = fmt.Errorf(
	"direct ONNX Runtime embedding is not yet supported; " +
		"use provider: \"ollama\" with model: \"nomic-embed-text\" for local embeddings, " +
		"or provider: \"openai\" for API-based embeddings",
)

// NewONNXEmbedder returns an error directing users to use Ollama instead.
func NewONNXEmbedder(modelPath string) (*ONNXEmbedder, error) {
	return nil, errONNXNotSupported
}

func (o *ONNXEmbedder) Name() string       { return "onnx" }
func (o *ONNXEmbedder) Dimensions() int     { return 0 }

func (o *ONNXEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errONNXNotSupported
}

func (o *ONNXEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errONNXNotSupported
}
