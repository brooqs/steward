// Package embedding provides the embedding interface and implementations
// for generating vector embeddings. Embeddings are used for semantic
// search in long-term memory.
package embedding

import "context"

// Embedder is the interface for generating vector embeddings from text.
type Embedder interface {
	// Embed generates a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
	// Name returns the embedder identifier.
	Name() string
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value between -1 and 1, where 1 = identical, 0 = orthogonal.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float32) float32 {
	// Fast inverse square root approximation would be overkill,
	// use standard approach via float64.
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
